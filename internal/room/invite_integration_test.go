package room_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/room"
)

func TestManagedInvitePermissionsRevocationAndRecap(t *testing.T) {
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

	ownerID, guestID, revokedGuestID := uuid.New(), uuid.New(), uuid.New()
	for id, name := range map[uuid.UUID]string{ownerID: "Invite owner", guestID: "Invite guest", revokedGuestID: "Revoked guest"} {
		if _, err := db.Exec(ctx, `INSERT INTO identities (id, kind, display_name) VALUES ($1, 'anonymous', $2)`, id, name); err != nil {
			t.Fatal(err)
		}
	}
	service := room.NewService(db)
	created, err := service.Create(ctx, ownerID, uuid.New(), room.CreateInput{Name: "Managed invite test", LifetimeDays: 1, AccessMode: "public"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM rooms WHERE id = $1`, created.ID)
		_, _ = db.Exec(ctx, `DELETE FROM identities WHERE id = ANY($1)`, []uuid.UUID{ownerID, guestID, revokedGuestID})
	})
	viewer, err := service.CreateInvite(ctx, ownerID, created.Slug, "viewer")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := service.InvitePreview(ctx, viewer.Token)
	if err != nil || preview.Permission != "viewer" {
		t.Fatalf("viewer preview: %#v, %v", preview, err)
	}
	joined, err := service.JoinInvite(ctx, guestID, viewer.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	if joined.CanUpload {
		t.Fatal("viewer invitation granted upload permission")
	}

	contributor, err := service.CreateInvite(ctx, ownerID, created.Slug, "contributor")
	if err != nil {
		t.Fatal(err)
	}
	joined, err = service.JoinInvite(ctx, guestID, contributor.Token, "")
	if err != nil {
		t.Fatal(err)
	}
	if !joined.CanUpload {
		t.Fatal("contributor invitation did not upgrade upload permission")
	}
	shared, err := service.ShareInvite(ctx, guestID, created.Slug)
	if err != nil || shared.Token != contributor.Token {
		t.Fatalf("member share invitation: %#v, %v", shared, err)
	}
	if err := service.RevokeInvite(ctx, ownerID, created.Slug, viewer.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.JoinInvite(ctx, revokedGuestID, viewer.Token, ""); !errors.Is(err, room.ErrInviteNotFound) {
		t.Fatalf("revoked invitation returned %v", err)
	}
	if err := service.DisableLegacyInvites(ctx, ownerID, created.Slug); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Preview(ctx, created.Slug); !errors.Is(err, room.ErrInviteNotFound) {
		t.Fatalf("disabled legacy invitation returned %v", err)
	}

	for index, mediaType := range []string{"image", "video"} {
		if _, err := db.Exec(ctx, `
			INSERT INTO media (room_id, uploader_identity_id, status, media_type, original_filename, mime_type, size_bytes, storage_key)
			VALUES ($1, $2, 'ready', $3, $4, $5, $6, $7)
		`, created.ID, guestID, mediaType, mediaType+".bin", mediaType+"/test", 100+index, "integration/"+uuid.NewString()); err != nil {
			t.Fatal(err)
		}
	}
	recap, err := service.Recap(ctx, ownerID, created.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if recap.MediaCount != 2 || recap.ImageCount != 1 || recap.VideoCount != 1 || recap.MemberCount != 2 || recap.ContributorCount != 1 || recap.TotalBytes != 201 {
		t.Fatalf("unexpected recap: %#v", recap)
	}
}
