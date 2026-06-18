package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorelov-m-v/gophprofile/internal/config"
	"github.com/gorelov-m-v/gophprofile/internal/migrate"
	"github.com/gorelov-m-v/gophprofile/internal/observability"
	"github.com/gorelov-m-v/gophprofile/internal/repository"
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
		log.ErrorContext(context.Background(), "migration failed", "err", err)
		os.Exit(1)
	}
	log.InfoContext(context.Background(), "migration completed")
}

func run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := repository.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	log.InfoContext(ctx, "running migrations", "path", cfg.MigrationsPath)
	return migrate.Run(ctx, pool, cfg.MigrationsPath)
}
