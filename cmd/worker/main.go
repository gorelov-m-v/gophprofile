package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorelov-m-v/gophprofile/internal/config"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/internal/migrate"
	"github.com/gorelov-m-v/gophprofile/internal/repository"
	workerpkg "github.com/gorelov-m-v/gophprofile/internal/worker"
	"github.com/gorelov-m-v/gophprofile/pkg/broker"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

const maxAttempts = 3
const recoveryLimit = 50

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("worker stopped", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repository.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := migrate.Run(ctx, pool, cfg.MigrationsPath); err != nil {
		return err
	}

	s3, err := storage.NewS3(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3UseSSL, cfg.S3PublicURL)
	if err != nil {
		return err
	}
	if err := s3.EnsureBucket(ctx); err != nil {
		return err
	}

	rabbit, err := broker.NewRabbitMQ(cfg.RabbitMQURL, cfg.RabbitMQExchange)
	if err != nil {
		return err
	}
	defer rabbit.Close()

	uploads, err := rabbit.ConsumeUploads(ctx)
	if err != nil {
		return err
	}
	deletes, err := rabbit.ConsumeDeletes(ctx)
	if err != nil {
		return err
	}

	processor := workerpkg.NewProcessor(repository.NewAvatarRepository(pool), s3, log)
	runRecovery(ctx, processor, log)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.Info("worker started")
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			runRecovery(ctx, processor, log)
		case delivery, ok := <-uploads:
			if !ok {
				return fmt.Errorf("upload consumer closed")
			}
			err := withRetry(ctx, func() error {
				return processor.HandleUpload(delivery.Context, delivery.Event)
			})
			if err != nil {
				log.Error("upload event failed", "avatar_id", delivery.Event.AvatarID, "err", err)
				_ = delivery.Nack(shouldRequeue(err))
				continue
			}
			_ = delivery.Ack()
		case delivery, ok := <-deletes:
			if !ok {
				return fmt.Errorf("delete consumer closed")
			}
			err := withRetry(ctx, func() error {
				return processor.HandleDelete(delivery.Context, delivery.Event)
			})
			if err != nil {
				log.Error("delete event failed", "avatar_id", delivery.Event.AvatarID, "err", err)
				_ = delivery.Nack(shouldRequeue(err))
				continue
			}
			_ = delivery.Ack()
		}
	}
}

func runRecovery(ctx context.Context, processor *workerpkg.Processor, log *slog.Logger) {
	uploads, err := processor.RecoverPendingUploads(ctx, recoveryLimit)
	if err != nil {
		log.Warn("pending upload recovery failed", "err", err)
	} else if uploads > 0 {
		log.Info("pending upload recovery completed", "count", uploads)
	}

	deletes, err := processor.RecoverPendingDeletes(ctx, recoveryLimit)
	if err != nil {
		log.Warn("pending delete recovery failed", "err", err)
	} else if deletes > 0 {
		log.Info("pending delete recovery completed", "count", deletes)
	}
}

func shouldRequeue(err error) bool {
	return !errors.Is(err, domain.ErrInvalidInput)
}

func withRetry(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if attempt == maxAttempts-1 {
			return err
		}
		timer := time.NewTimer(broker.RetryBackoff(attempt))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
