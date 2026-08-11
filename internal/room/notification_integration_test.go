package room_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/room"
)

func TestExpiryNotificationsAreScheduledAndDisabled(t *testing.T) {
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

	ownerID := uuid.New()
	telegramID := int64(8_000_000_000 + time.Now().UnixNano()%100_000_000)
	if _, err := db.Exec(ctx, `
		INSERT INTO identities (id, kind, display_name, telegram_user_id)
		VALUES ($1, 'telegram', 'Expiry test owner', $2)
	`, ownerID, telegramID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM identities WHERE id = $1`, ownerID) })

	service := room.NewService(db)
	created, err := service.Create(ctx, ownerID, uuid.New(), room.CreateInput{Name: "Expiry reminder test", LifetimeDays: 1, AccessMode: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateNotifications(ctx, ownerID, created.Slug, true); err != nil {
		t.Fatal(err)
	}

	rows, err := db.Query(ctx, `
		SELECT (payload->>'hours_remaining')::int, available_at
		FROM telegram_notification_outbox
		WHERE room_id = $1 AND event_type = 'room_expiry' AND sent_at IS NULL
		ORDER BY (payload->>'hours_remaining')::int DESC
	`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	wantHours := []int{6, 1}
	index := 0
	for rows.Next() {
		var hours int
		var availableAt time.Time
		if err := rows.Scan(&hours, &availableAt); err != nil {
			t.Fatal(err)
		}
		if index >= len(wantHours) || hours != wantHours[index] {
			t.Fatalf("unexpected reminder threshold %d at index %d", hours, index)
		}
		wantAvailable := created.ExpiresAt.Add(-time.Duration(hours) * time.Hour)
		if delta := availableAt.Sub(wantAvailable); delta < -time.Second || delta > time.Second {
			t.Fatalf("threshold %d scheduled at %s, want %s", hours, availableAt, wantAvailable)
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if index != len(wantHours) {
		t.Fatalf("got %d expiry reminders, want %d", index, len(wantHours))
	}

	if _, err := service.UpdateNotifications(ctx, ownerID, created.Slug, false); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM telegram_notification_outbox WHERE room_id = $1 AND event_type = 'room_expiry' AND sent_at IS NULL`, created.ID).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("got %d pending reminders after disabling notifications", pending)
	}
}
