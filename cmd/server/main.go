package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorelov-m-v/gophprofile/internal/api"
	"github.com/gorelov-m-v/gophprofile/internal/config"
	"github.com/gorelov-m-v/gophprofile/internal/handlers"
	"github.com/gorelov-m-v/gophprofile/internal/migrate"
	"github.com/gorelov-m-v/gophprofile/internal/repository"
	"github.com/gorelov-m-v/gophprofile/internal/services"
	"github.com/gorelov-m-v/gophprofile/pkg/broker"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(log); err != nil {
		log.Error("server stopped", "err", err)
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

	repo := repository.NewAvatarRepository(pool)
	service := services.NewAvatarService(repo, s3, rabbit, cfg.MaxUploadSize)
	handler := handlers.New(service, cfg.MaxUploadSize, cfg.WebDir)

	server := &http.Server{
		Addr:         cfg.ServerAddr,
		Handler:      api.NewRouter(handler, cfg.WebDir),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.ServerAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
