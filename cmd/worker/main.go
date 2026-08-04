package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"photodrop/internal/cleanup"
	"photodrop/internal/config"
	"photodrop/internal/database"
	"photodrop/internal/objectstore"
	"photodrop/internal/roomarchive"
	"photodrop/internal/thumbnail"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	store, err := objectstore.New(cfg.S3)
	if err != nil {
		return err
	}
	if err := store.Ready(ctx); err != nil {
		return err
	}

	cleanupService := cleanup.New(db, store, cfg.CleanupInterval, logger)
	thumbnailService := thumbnail.New(db, store, cfg.ThumbnailInterval, logger)
	archiveWorker := roomarchive.NewWorker(db, store, cfg.ArchiveInterval, logger)
	errCh := make(chan error, 3)
	go func() { errCh <- cleanupService.Run(ctx) }()
	go func() { errCh <- thumbnailService.Run(ctx) }()
	go func() { errCh <- archiveWorker.Run(ctx) }()
	logger.Info("worker started", "cleanup_interval", cfg.CleanupInterval, "thumbnail_interval", cfg.ThumbnailInterval, "archive_interval", cfg.ArchiveInterval)
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}
