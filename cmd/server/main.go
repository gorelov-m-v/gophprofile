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
	"github.com/gorelov-m-v/gophprofile/internal/metrics"
	"github.com/gorelov-m-v/gophprofile/internal/migrate"
	"github.com/gorelov-m-v/gophprofile/internal/observability"
	"github.com/gorelov-m-v/gophprofile/internal/repository"
	"github.com/gorelov-m-v/gophprofile/internal/services"
	"github.com/gorelov-m-v/gophprofile/pkg/broker"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

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
		log.ErrorContext(context.Background(), "server stopped", "err", err)
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
	registerRabbitMQMetrics(cfg.ServiceName, rabbit)

	repo := repository.NewAvatarRepository(pool)
	service := services.NewAvatarService(repo, s3, rabbit, cfg.MaxUploadSize).WithLogger(log)
	handler := handlers.New(service, cfg.MaxUploadSize, cfg.WebDir)

	server := &http.Server{
		Addr:         cfg.ServerAddr,
		Handler:      api.NewRouter(handler, cfg.WebDir),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.InfoContext(ctx, "http server listening", "addr", cfg.ServerAddr)
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

type dbStatProvider interface {
	AcquiredConns() int32
	IdleConns() int32
	TotalConns() int32
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
