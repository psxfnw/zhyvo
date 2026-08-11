package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"photodrop/internal/auth"
	"photodrop/internal/config"
	"photodrop/internal/database"
	"photodrop/internal/httpapi"
	"photodrop/internal/media"
	"photodrop/internal/objectstore"
	"photodrop/internal/realtime"
	"photodrop/internal/room"
	"photodrop/internal/roomarchive"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
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
	tokens := auth.NewTokenManager(cfg.Auth.AccessSecret, cfg.Auth.Issuer, cfg.Auth.AccessTTL)
	authService := auth.NewService(db, tokens, cfg.Auth.RefreshTTL)
	telegramOIDC := auth.NewTelegramOIDC(cfg.Telegram.LoginClientID, cfg.Telegram.LoginClientSecret)
	roomService := room.NewService(db)
	mediaService := media.NewService(db, store)
	archiveService := roomarchive.NewService(db, store)
	realtimeService := realtime.NewService(db)
	realtimeBroker := realtime.NewBroker(db, logger)
	go realtimeBroker.Run(ctx)

	server := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(httpapi.Dependencies{
			DB:                  db,
			Store:               store,
			AuthService:         authService,
			Tokens:              tokens,
			RoomService:         roomService,
			MediaService:        mediaService,
			ArchiveService:      archiveService,
			RealtimeService:     realtimeService,
			RealtimeBroker:      realtimeBroker,
			TelegramBotToken:    cfg.Telegram.BotToken,
			TelegramBotUsername: cfg.Telegram.BotUsername,
			TelegramInitDataTTL: cfg.Telegram.InitDataTTL,
			TelegramOIDC:        telegramOIDC,
			Logger:              logger,
		}),
		ReadHeaderTimeout: cfg.ShutdownTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", cfg.HTTPAddr)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
