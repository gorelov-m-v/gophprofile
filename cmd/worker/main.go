package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorelov-m-v/gophprofile/internal/config"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/internal/metrics"
	"github.com/gorelov-m-v/gophprofile/internal/migrate"
	"github.com/gorelov-m-v/gophprofile/internal/observability"
	"github.com/gorelov-m-v/gophprofile/internal/repository"
	workerpkg "github.com/gorelov-m-v/gophprofile/internal/worker"
	"github.com/gorelov-m-v/gophprofile/pkg/broker"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const maxAttempts = 3
const recoveryLimit = 50

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("load config", "err", err)
		os.Exit(1)
	}

	log, closeLog, err := observability.NewLogger(cfg.ServiceName, cfg.LogLevel, cfg.LogFile)
	if err != nil {
		slog.New(slog.NewJSONHandler(os.Stdout, nil)).Error("create logger", "err", err)
		os.Exit(1)
	}
	defer func() {
		_ = closeLog()
	}()

	if err := run(cfg, log); err != nil {
		log.ErrorContext(context.Background(), "worker stopped", "err", err)
		os.Exit(1)
	}
}

func run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := observability.SetupTracing(ctx, cfg.ServiceName, cfg.OTLPEndpoint)
	if err != nil {
		return err
	}
	defer func() {
		_ = shutdownTracing(context.Background())
	}()

	pool, err := repository.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	registerDatabaseMetrics(cfg.ServiceName, func() dbStatProvider { return pool.Stat() })

	if cfg.RunMigrations {
		if err := migrate.Run(ctx, pool, cfg.MigrationsPath); err != nil {
			return err
		}
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
	registerRabbitMQMetrics(cfg.ServiceName, rabbit)

	uploads, err := rabbit.ConsumeUploads(ctx)
	if err != nil {
		return err
	}
	deletes, err := rabbit.ConsumeDeletes(ctx)
	if err != nil {
		return err
	}

	processor := workerpkg.NewProcessor(repository.NewAvatarRepository(pool), s3, log)
	metricsErrCh := startMetricsServer(ctx, cfg, log)
	runRecovery(ctx, processor, log)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	log.InfoContext(ctx, "worker started")
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-metricsErrCh:
			if errors.Is(err, http.ErrServerClosed) {
				continue
			}
			return err
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
				log.ErrorContext(delivery.Context, "upload event failed", "avatar_id", delivery.Event.AvatarID, "err", err)
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
				log.ErrorContext(delivery.Context, "delete event failed", "avatar_id", delivery.Event.AvatarID, "err", err)
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
		log.WarnContext(ctx, "pending upload recovery failed", "err", err)
	} else if uploads > 0 {
		log.InfoContext(ctx, "pending upload recovery completed", "count", uploads)
	}

	deletes, err := processor.RecoverPendingDeletes(ctx, recoveryLimit)
	if err != nil {
		log.WarnContext(ctx, "pending delete recovery failed", "err", err)
	} else if deletes > 0 {
		log.InfoContext(ctx, "pending delete recovery completed", "count", deletes)
	}
}

func shouldRequeue(err error) bool {
	return !errors.Is(err, domain.ErrInvalidInput)
}

type dbStatProvider interface {
	AcquiredConns() int32
	IdleConns() int32
	TotalConns() int32
}

func startMetricsServer(ctx context.Context, cfg config.Config, log *slog.Logger) <-chan error {
	errCh := make(chan error, 1)
	server := &http.Server{
		Addr:         cfg.MetricsAddr,
		Handler:      promhttp.Handler(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	go func() {
		log.InfoContext(ctx, "metrics server listening", "addr", cfg.MetricsAddr)
		errCh <- server.ListenAndServe()
	}()

	return errCh
}

func registerDatabaseMetrics(service string, stat func() dbStatProvider) {
	metrics.RegisterGauge("gophprofile_db_pool_acquired_conns", "Acquired PostgreSQL pool connections.", service, func() float64 {
		return float64(stat().AcquiredConns())
	})
	metrics.RegisterGauge("gophprofile_db_pool_idle_conns", "Idle PostgreSQL pool connections.", service, func() float64 {
		return float64(stat().IdleConns())
	})
	metrics.RegisterGauge("gophprofile_db_pool_total_conns", "Total PostgreSQL pool connections.", service, func() float64 {
		return float64(stat().TotalConns())
	})
}

func registerRabbitMQMetrics(service string, rabbit *broker.RabbitMQ) {
	metrics.RegisterGauge("gophprofile_rabbitmq_upload_queue_messages", "Ready messages in upload queue.", service, func() float64 {
		depth, err := rabbit.QueueDepth(broker.UploadQueue)
		if err != nil {
			return -1
		}
		return float64(depth)
	})
	metrics.RegisterGauge("gophprofile_rabbitmq_delete_queue_messages", "Ready messages in delete queue.", service, func() float64 {
		depth, err := rabbit.QueueDepth(broker.DeleteQueue)
		if err != nil {
			return -1
		}
		return float64(depth)
	})
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
