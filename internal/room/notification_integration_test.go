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
	enabled := true
	if _, err := service.UpdateNotifications(ctx, ownerID, created.Slug, room.NotificationUpdate{TelegramEnabled: &enabled}); err != nil {
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

	disabled := false
	if _, err := service.UpdateNotifications(ctx, ownerID, created.Slug, room.NotificationUpdate{TelegramEnabled: &disabled}); err != nil {
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

func TestMemberNotificationPreferencesAndJoinFanout(t *testing.T) {
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

	ownerID, memberID, newcomerID := uuid.New(), uuid.New(), uuid.New()
	seed := time.Now().UnixNano() % 10_000_000
	ownerTelegramID, memberTelegramID := int64(8_100_000_000+seed), int64(8_200_000_000+seed)
	if _, err := db.Exec(ctx, `
		INSERT INTO identities (id, kind, display_name, telegram_user_id)
		VALUES ($1, 'telegram', 'Notification owner', $2),
		       ($3, 'telegram', 'Notification member', $4),
		       ($5, 'anonymous', 'New participant', NULL)
	`, ownerID, ownerTelegramID, memberID, memberTelegramID, newcomerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM identities WHERE id = ANY($1)`, []uuid.UUID{ownerID, memberID, newcomerID})
	})

	service := room.NewService(db)
	created, err := service.Create(ctx, ownerID, uuid.New(), room.CreateInput{Name: "Member notification test", LifetimeDays: 1, AccessMode: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Join(ctx, memberID, created.Slug, ""); err != nil {
		t.Fatal(err)
	}
	enabled, disabled := true, false
	if _, err := service.UpdateNotifications(ctx, ownerID, created.Slug, room.NotificationUpdate{TelegramEnabled: &enabled}); err != nil {
		t.Fatal(err)
	}
	memberSettings, err := service.UpdateNotifications(ctx, memberID, created.Slug, room.NotificationUpdate{
		TelegramEnabled: &enabled, NewMediaEnabled: &enabled, ExpiryEnabled: &enabled, MemberJoinedEnabled: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if memberSettings.IsOwner || !memberSettings.TelegramEnabled || memberSettings.MemberJoinedEnabled {
		t.Fatalf("unexpected member settings: %#v", memberSettings)
	}
	var expiryRecipients int
	if err := db.QueryRow(ctx, `
		SELECT count(DISTINCT telegram_user_id) FROM telegram_notification_outbox
		WHERE room_id = $1 AND event_type = 'room_expiry' AND sent_at IS NULL
	`, created.ID).Scan(&expiryRecipients); err != nil || expiryRecipients != 2 {
		t.Fatalf("expiry recipient count = %d, error = %v", expiryRecipients, err)
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO telegram_notification_outbox (room_id, telegram_user_id, event_type, payload, dedupe_key)
		VALUES ($1, $2, 'media_uploaded', '{}'::jsonb, $3)
	`, created.ID, memberTelegramID, "test-member-media:"+uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateNotifications(ctx, memberID, created.Slug, room.NotificationUpdate{NewMediaEnabled: &disabled}); err != nil {
		t.Fatal(err)
	}
	var pendingMemberMedia int
	if err := db.QueryRow(ctx, `
		SELECT count(*) FROM telegram_notification_outbox
		WHERE room_id = $1 AND telegram_user_id = $2 AND event_type = 'media_uploaded' AND sent_at IS NULL
	`, created.ID, memberTelegramID).Scan(&pendingMemberMedia); err != nil || pendingMemberMedia != 0 {
		t.Fatalf("pending disabled member media notifications = %d, error = %v", pendingMemberMedia, err)
	}
	if _, err := service.Join(ctx, newcomerID, created.Slug, ""); err != nil {
		t.Fatal(err)
	}
	var ownerJoins, memberJoins int
	if err := db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE telegram_user_id = $2), count(*) FILTER (WHERE telegram_user_id = $3)
		FROM telegram_notification_outbox
		WHERE room_id = $1 AND event_type = 'member_joined' AND payload->>'actor_name' = 'New participant'
	`, created.ID, ownerTelegramID, memberTelegramID).Scan(&ownerJoins, &memberJoins); err != nil {
		t.Fatal(err)
	}
	if ownerJoins != 1 || memberJoins != 0 {
		t.Fatalf("join notifications owner=%d member=%d, want 1 and 0", ownerJoins, memberJoins)
	}
}
