package auth_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/auth"
	"photodrop/internal/room"
)

func TestLinkTelegramPreservesAnonymousRoom(t *testing.T) {
	databaseURL := os.Getenv("PHOTODROP_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("PHOTODROP_INTEGRATION_DATABASE_URL is not set")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tokens := auth.NewTokenManager("integration_test_secret_with_at_least_32_chars", "photodrop-test", 15*time.Minute)
	authService := auth.NewService(db, tokens, 24*time.Hour)
	roomService := room.NewService(db)
	anonymous, err := authService.CreateAnonymous(ctx, "Browser guest", "web")
	if err != nil {
		t.Fatal(err)
	}
	created, err := roomService.Create(ctx, anonymous.Identity.ID, uuid.New(), room.CreateInput{
		Name: "Identity link test", LifetimeDays: 1, AccessMode: "public",
	})
	if err != nil {
		t.Fatal(err)
	}
	telegramID := int64(9_000_000_000 + time.Now().UnixNano()%100_000_000)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM rooms WHERE id = $1`, created.ID)
		_, _ = db.Exec(ctx, `DELETE FROM identities WHERE telegram_user_id = $1 OR id = $2`, telegramID, anonymous.Identity.ID)
	})

	linked, err := authService.LinkTelegram(ctx, anonymous.Identity.ID, auth.TelegramUser{ID: telegramID, FirstName: "Telegram owner"})
	if err != nil {
		t.Fatal(err)
	}
	if linked.Identity.Kind != "telegram" {
		t.Fatalf("expected telegram identity, got %q", linked.Identity.Kind)
	}
	transferred, err := roomService.Get(ctx, linked.Identity.ID, created.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if transferred.Role != "owner" {
		t.Fatalf("expected owner role, got %q", transferred.Role)
	}
	var sourceExists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM identities WHERE id = $1)`, anonymous.Identity.ID).Scan(&sourceExists); err != nil {
		t.Fatal(err)
	}
	if sourceExists {
		t.Fatal("anonymous identity was not removed after linking")
	}
}
