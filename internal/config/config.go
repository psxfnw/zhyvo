package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr             string
	DatabaseURL          string
	ShutdownTimeout      time.Duration
	CleanupInterval      time.Duration
	ThumbnailInterval    time.Duration
	ArchiveInterval      time.Duration
	NotificationInterval time.Duration
	S3                   S3Config
	Auth                 AuthConfig
	Telegram             TelegramConfig
	AdminTelegramIDs     []int64
}

type TelegramConfig struct {
	BotToken          string
	BotUsername       string
	LoginClientID     string
	LoginClientSecret string
	InitDataTTL       time.Duration
}

type S3Config struct {
	Endpoint       string
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	Bucket         string
	Region         string
	UseSSL         bool
	PublicUseSSL   bool
	PresignTTL     time.Duration
}

type AuthConfig struct {
	AccessSecret string
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	Issuer       string
}

func Load() (Config, error) {
	shutdownTimeout, err := duration("SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}

	cleanupInterval, err := duration("CLEANUP_INTERVAL", time.Minute)
	if err != nil {
		return Config{}, err
	}
	thumbnailInterval, err := duration("THUMBNAIL_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	archiveInterval, err := duration("ARCHIVE_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	notificationInterval, err := duration("NOTIFICATION_INTERVAL", 2*time.Second)
	if err != nil {
		return Config{}, err
	}

	useSSL, err := boolean("S3_USE_SSL", false)
	if err != nil {
		return Config{}, err
	}
	publicUseSSL, err := boolean("S3_PUBLIC_USE_SSL", useSSL)
	if err != nil {
		return Config{}, err
	}

	accessTTL, err := duration("AUTH_ACCESS_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTTL, err := duration("AUTH_REFRESH_TTL", 30*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	presignTTL, err := duration("S3_PRESIGN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	telegramInitDataTTL, err := duration("TELEGRAM_INIT_DATA_TTL", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:             value("HTTP_ADDR", ":8080"),
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		ShutdownTimeout:      shutdownTimeout,
		CleanupInterval:      cleanupInterval,
		ThumbnailInterval:    thumbnailInterval,
		ArchiveInterval:      archiveInterval,
		NotificationInterval: notificationInterval,
		S3: S3Config{
			Endpoint:       value("S3_ENDPOINT", "localhost:9000"),
			PublicEndpoint: value("S3_PUBLIC_ENDPOINT", value("S3_ENDPOINT", "localhost:9000")),
			AccessKey:      os.Getenv("S3_ACCESS_KEY"),
			SecretKey:      os.Getenv("S3_SECRET_KEY"),
			Bucket:         value("S3_BUCKET", "photodrop-media"),
			Region:         value("S3_REGION", "us-east-1"),
			UseSSL:         useSSL,
			PublicUseSSL:   publicUseSSL,
			PresignTTL:     presignTTL,
		},
		Auth: AuthConfig{
			AccessSecret: os.Getenv("AUTH_ACCESS_SECRET"),
			AccessTTL:    accessTTL,
			RefreshTTL:   refreshTTL,
			Issuer:       value("AUTH_ISSUER", "photodrop"),
		},
		Telegram: TelegramConfig{
			BotToken:          os.Getenv("TELEGRAM_BOT_TOKEN"),
			BotUsername:       value("TELEGRAM_BOT_USERNAME", "zhyvoappbot"),
			LoginClientID:     os.Getenv("TELEGRAM_LOGIN_CLIENT_ID"),
			LoginClientSecret: os.Getenv("TELEGRAM_LOGIN_CLIENT_SECRET"),
			InitDataTTL:       telegramInitDataTTL,
		},
	}
	cfg.AdminTelegramIDs, err = int64List("ADMIN_TELEGRAM_IDS")
	if err != nil {
		return Config{}, err
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.S3.AccessKey == "" || cfg.S3.SecretKey == "" {
		return Config{}, fmt.Errorf("S3_ACCESS_KEY and S3_SECRET_KEY are required")
	}
	if cfg.S3.PresignTTL <= 0 || cfg.S3.PresignTTL > 24*time.Hour {
		return Config{}, fmt.Errorf("S3_PRESIGN_TTL must be between 1 second and 24 hours")
	}
	if cfg.CleanupInterval <= 0 {
		return Config{}, fmt.Errorf("CLEANUP_INTERVAL must be positive")
	}
	if cfg.ThumbnailInterval <= 0 {
		return Config{}, fmt.Errorf("THUMBNAIL_INTERVAL must be positive")
	}
	if cfg.ArchiveInterval <= 0 {
		return Config{}, fmt.Errorf("ARCHIVE_INTERVAL must be positive")
	}
	if cfg.NotificationInterval <= 0 {
		return Config{}, fmt.Errorf("NOTIFICATION_INTERVAL must be positive")
	}
	if len(cfg.Auth.AccessSecret) < 32 {
		return Config{}, fmt.Errorf("AUTH_ACCESS_SECRET must contain at least 32 characters")
	}
	if cfg.Auth.AccessTTL <= 0 || cfg.Auth.RefreshTTL <= cfg.Auth.AccessTTL {
		return Config{}, fmt.Errorf("AUTH_REFRESH_TTL must be greater than positive AUTH_ACCESS_TTL")
	}
	if cfg.Telegram.InitDataTTL <= 0 || cfg.Telegram.InitDataTTL > 24*time.Hour {
		return Config{}, fmt.Errorf("TELEGRAM_INIT_DATA_TTL must be between 1 second and 24 hours")
	}

	return cfg, nil
}

func int64List(key string) ([]int64, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, nil
	}
	seen := make(map[int64]struct{})
	result := make([]int64, 0)
	for _, item := range strings.Split(raw, ",") {
		parsed, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%s must contain positive Telegram user IDs separated by commas", key)
		}
		if _, exists := seen[parsed]; exists {
			continue
		}
		seen[parsed] = struct{}{}
		result = append(result, parsed)
	}
	return result, nil
}

func value(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func boolean(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}
