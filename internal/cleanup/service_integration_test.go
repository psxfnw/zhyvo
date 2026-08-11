package cleanup

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/room"
)

func TestExpiredRoomMetadataCanBeDeletedWithRealtimeTriggers(t *testing.T) {
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
	if _, err := db.Exec(ctx, `INSERT INTO identities (id, kind, display_name) VALUES ($1, 'anonymous', 'Cleanup test owner')`, ownerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(ctx, `DELETE FROM identities WHERE id = $1`, ownerID) })

	created, err := room.NewService(db).Create(ctx, ownerID, uuid.New(), room.CreateInput{Name: "Cleanup trigger test", LifetimeDays: 1, AccessMode: "public"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE rooms SET status = 'deleting' WHERE id = $1`, created.ID); err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(ctx, `DELETE FROM rooms WHERE id = $1 AND status = 'deleting'`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatalf("deleted %d rooms, want 1", result.RowsAffected())
	}

	var exists bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM rooms WHERE id = $1)`, created.ID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("expired room metadata still exists")
	}
}
