package media

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Uploader struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
}

type MediaPermissions struct {
	CanDelete bool `json:"can_delete"`
}

type GalleryItem struct {
	ID               uuid.UUID        `json:"id"`
	MediaType        string           `json:"media_type"`
	MIMEType         string           `json:"mime_type"`
	OriginalFilename string           `json:"original_filename"`
	SizeBytes        int64            `json:"size_bytes"`
	Width            *int             `json:"width,omitempty"`
	Height           *int             `json:"height,omitempty"`
	DurationMS       *int64           `json:"duration_ms,omitempty"`
	CapturedAt       *time.Time       `json:"captured_at,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	ThumbnailURL     *string          `json:"thumbnail_url"`
	ThumbnailStatus  string           `json:"thumbnail_status"`
	UploadedBy       Uploader         `json:"uploaded_by"`
	Permissions      MediaPermissions `json:"permissions"`

	thumbnailKey *string
}

type GalleryPage struct {
	Items      []GalleryItem `json:"items"`
	NextCursor *string       `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
}

type Download struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Filename  string    `json:"filename"`
}

type galleryCursor struct {
	SortAt time.Time
	ID     uuid.UUID
}

func (s *Service) Gallery(ctx context.Context, identityID uuid.UUID, slug string, limit int, rawCursor string) (GalleryPage, error) {
	if limit < 1 || limit > 100 {
		return GalleryPage{}, fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalidInput)
	}
	roomID, role, err := s.roomMembership(ctx, identityID, slug)
	if err != nil {
		return GalleryPage{}, err
	}

	var cursor *galleryCursor
	if rawCursor != "" {
		parsed, err := decodeGalleryCursor(rawCursor)
		if err != nil {
			return GalleryPage{}, fmt.Errorf("%w: invalid cursor", ErrInvalidInput)
		}
		cursor = &parsed
	}

	var cursorTime *time.Time
	cursorID := uuid.Nil
	if cursor != nil {
		cursorTime = &cursor.SortAt
		cursorID = cursor.ID
	}
	rows, err := s.db.Query(ctx, `
		SELECT m.id, m.media_type, m.mime_type, m.original_filename, m.size_bytes,
		       m.width, m.height, m.duration_ms, m.captured_at, m.created_at,
		       m.thumbnail_key, m.thumbnail_status, i.id, i.display_name, m.uploader_identity_id
		FROM media m
		JOIN identities i ON i.id = m.uploader_identity_id
		WHERE m.room_id = $1
		  AND m.status = 'ready'
		  AND ($2::timestamptz IS NULL OR (COALESCE(m.captured_at, m.created_at), m.id) < ($2, $3))
		ORDER BY COALESCE(m.captured_at, m.created_at) DESC, m.id DESC
		LIMIT $4
	`, roomID, cursorTime, cursorID, limit+1)
	if err != nil {
		return GalleryPage{}, fmt.Errorf("query gallery: %w", err)
	}
	defer rows.Close()

	items := make([]GalleryItem, 0, limit+1)
	for rows.Next() {
		var item GalleryItem
		var uploaderID uuid.UUID
		if err := rows.Scan(
			&item.ID, &item.MediaType, &item.MIMEType, &item.OriginalFilename, &item.SizeBytes,
			&item.Width, &item.Height, &item.DurationMS, &item.CapturedAt, &item.CreatedAt,
			&item.thumbnailKey, &item.ThumbnailStatus, &item.UploadedBy.ID, &item.UploadedBy.DisplayName, &uploaderID,
		); err != nil {
			return GalleryPage{}, fmt.Errorf("scan gallery item: %w", err)
		}
		item.Permissions.CanDelete = role == "owner" || uploaderID == identityID
		if item.thumbnailKey != nil {
			url, _, err := s.store.PresignGet(ctx, *item.thumbnailKey, "")
			if err != nil {
				return GalleryPage{}, err
			}
			item.ThumbnailURL = &url
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return GalleryPage{}, fmt.Errorf("iterate gallery: %w", err)
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var nextCursor *string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		sortAt := last.CreatedAt
		if last.CapturedAt != nil {
			sortAt = *last.CapturedAt
		}
		encoded := encodeGalleryCursor(galleryCursor{SortAt: sortAt, ID: last.ID})
		nextCursor = &encoded
	}
	return GalleryPage{Items: items, NextCursor: nextCursor, HasMore: hasMore}, nil
}

func (s *Service) Download(ctx context.Context, identityID, mediaID uuid.UUID) (Download, error) {
	var storageKey, filename, status, roomStatus string
	var roomExpiry time.Time
	err := s.db.QueryRow(ctx, `
		SELECT m.storage_key, m.original_filename, m.status, r.status, r.expires_at
		FROM media m
		JOIN rooms r ON r.id = m.room_id
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE m.id = $2
	`, identityID, mediaID).Scan(&storageKey, &filename, &status, &roomStatus, &roomExpiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return Download{}, ErrMediaNotFound
	}
	if err != nil {
		return Download{}, fmt.Errorf("get media download: %w", err)
	}
	if roomStatus != "active" || !roomExpiry.After(s.now()) {
		return Download{}, ErrRoomExpired
	}
	if status != "ready" {
		return Download{}, ErrMediaNotReady
	}
	url, expiresAt, err := s.store.PresignGet(ctx, storageKey, filename)
	if err != nil {
		return Download{}, err
	}
	return Download{URL: url, ExpiresAt: expiresAt, Filename: filename}, nil
}

func (s *Service) Delete(ctx context.Context, identityID, mediaID uuid.UUID) error {
	var uploaderID, roomID uuid.UUID
	var role, status, storageKey string
	var thumbnailKey *string
	err := s.db.QueryRow(ctx, `
		SELECT m.uploader_identity_id, m.room_id, rm.role, m.status, m.storage_key, m.thumbnail_key
		FROM media m
		JOIN room_members rm ON rm.room_id = m.room_id AND rm.identity_id = $1
		WHERE m.id = $2
	`, identityID, mediaID).Scan(&uploaderID, &roomID, &role, &status, &storageKey, &thumbnailKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMediaNotFound
	}
	if err != nil {
		return fmt.Errorf("authorize media deletion: %w", err)
	}
	if role != "owner" && uploaderID != identityID {
		return ErrMediaAccessDenied
	}
	if status == "deleted" {
		return nil
	}
	if status != "ready" && status != "failed" {
		return ErrMediaNotReady
	}
	if err := s.store.Remove(ctx, storageKey); err != nil {
		return err
	}
	if thumbnailKey != nil {
		if err := s.store.Remove(ctx, *thumbnailKey); err != nil {
			return err
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var currentStatus string
	var sizeBytes int64
	err = tx.QueryRow(ctx, `
		SELECT m.status, m.size_bytes
		FROM media m
		JOIN rooms r ON r.id = m.room_id
		WHERE m.id = $1 AND r.id = $2
		FOR UPDATE OF m, r
	`, mediaID, roomID).Scan(&currentStatus, &sizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock media deletion: %w", err)
	}
	if currentStatus == "deleted" {
		return tx.Commit(ctx)
	}
	if currentStatus != "ready" && currentStatus != "failed" {
		return ErrMediaNotReady
	}
	if _, err := tx.Exec(ctx, `UPDATE media SET status = 'deleted', deleted_at = now() WHERE id = $1`, mediaID); err != nil {
		return err
	}
	if currentStatus == "ready" {
		if _, err := tx.Exec(ctx, `
			UPDATE rooms
			SET used_files = used_files - 1,
			    used_storage_bytes = used_storage_bytes - $1,
			    media_revision = media_revision + 1
			WHERE id = $2
		`, sizeBytes, roomID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Service) roomMembership(ctx context.Context, identityID uuid.UUID, slug string) (uuid.UUID, string, error) {
	var roomID uuid.UUID
	var role, status string
	var expiresAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT r.id, rm.role, r.status, r.expires_at
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = upper(trim($2))
	`, identityID, slug).Scan(&roomID, &role, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if checkErr := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rooms WHERE slug = upper(trim($1)))`, slug).Scan(&exists); checkErr != nil {
			return uuid.Nil, "", checkErr
		}
		if exists {
			return uuid.Nil, "", ErrNotMember
		}
		return uuid.Nil, "", ErrRoomNotFound
	}
	if err != nil {
		return uuid.Nil, "", err
	}
	if status != "active" || !expiresAt.After(s.now()) {
		return uuid.Nil, "", ErrRoomExpired
	}
	return roomID, role, nil
}

func encodeGalleryCursor(cursor galleryCursor) string {
	raw := strconv.FormatInt(cursor.SortAt.UTC().UnixMicro(), 10) + "." + cursor.ID.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeGalleryCursor(encoded string) (galleryCursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return galleryCursor{}, err
	}
	parts := strings.Split(string(raw), ".")
	if len(parts) != 2 {
		return galleryCursor{}, errors.New("invalid cursor structure")
	}
	microseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return galleryCursor{}, err
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return galleryCursor{}, err
	}
	return galleryCursor{SortAt: time.UnixMicro(microseconds).UTC(), ID: id}, nil
}
