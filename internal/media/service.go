package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/objectstore"
)

const (
	maxImageSize       int64 = 50 * 1024 * 1024
	maxVideoSize       int64 = 2 * 1024 * 1024 * 1024
	multipartThreshold int64 = 100 * 1024 * 1024
	multipartPartSize  int64 = 16 * 1024 * 1024
	maxPartsBatch            = 20
	uploadLifetime           = 24 * time.Hour
)

var (
	ErrInvalidInput          = errors.New("invalid media input")
	ErrRoomNotFound          = errors.New("room not found")
	ErrRoomExpired           = errors.New("room expired")
	ErrNotMember             = errors.New("identity is not a room member")
	ErrUploadsClosed         = errors.New("room uploads are closed")
	ErrUploadPermission      = errors.New("room member cannot upload")
	ErrRoomLimitReached      = errors.New("room storage limit reached")
	ErrUploadNotFound        = errors.New("upload not found")
	ErrUploadExpired         = errors.New("upload expired")
	ErrUploadCompleted       = errors.New("upload already completed")
	ErrUploadNotReady        = errors.New("uploaded object is not available yet")
	ErrMediaNotReady         = errors.New("media is not ready")
	ErrMediaNotFound         = errors.New("media not found")
	ErrMediaAccessDenied     = errors.New("media access denied")
	ErrMediaCaptionForbidden = errors.New("media caption edit denied")
	ErrRoomOwnerRequired     = errors.New("room owner required")
	ErrMediaDuplicate        = errors.New("media already exists in room")
	ErrInvalidUploadParts    = errors.New("invalid upload parts")
	ErrUploadedSizeMismatch  = errors.New("uploaded object size does not match declared size")
	ErrUploadedTypeMismatch  = errors.New("uploaded object content does not match declared media type")
	ErrIdempotencyConflict   = errors.New("idempotency key was already used with different input")
)

var supportedMIMETypes = map[string]string{
	"image/jpeg":      "image",
	"image/png":       "image",
	"image/webp":      "image",
	"image/heic":      "image",
	"image/heif":      "image",
	"image/avif":      "image",
	"image/gif":       "image",
	"video/mp4":       "video",
	"video/quicktime": "video",
	"video/webm":      "video",
	"video/x-m4v":     "video",
	"video/3gpp":      "video",
}

type ObjectStore interface {
	PresignPut(context.Context, string, string, time.Time) (string, time.Time, error)
	StartMultipart(context.Context, string, string) (string, error)
	PresignPart(context.Context, string, string, int, time.Time) (string, time.Time, error)
	CompleteMultipart(context.Context, string, string, string, []objectstore.CompletedPart) error
	AbortMultipart(context.Context, string, string) error
	Stat(context.Context, string) (objectstore.ObjectInfo, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Remove(context.Context, string) error
	PresignGet(context.Context, string, string) (string, time.Time, error)
}

type Service struct {
	db    *pgxpool.Pool
	store ObjectStore
	now   func() time.Time
}

type InitiateInput struct {
	Filename   string
	MIMEType   string
	SizeBytes  int64
	CapturedAt *time.Time
	Checksum   string
}

type Upload struct {
	ID            uuid.UUID         `json:"id"`
	MediaID       uuid.UUID         `json:"media_id"`
	Type          string            `json:"type"`
	Status        string            `json:"status"`
	PartSizeBytes *int64            `json:"part_size_bytes,omitempty"`
	PartsCount    *int              `json:"parts_count,omitempty"`
	ExpiresAt     time.Time         `json:"expires_at"`
	URL           string            `json:"url,omitempty"`
	URLExpiresAt  time.Time         `json:"url_expires_at,omitempty"`
	Method        string            `json:"method,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`

	objectKey       string
	storageUploadID *string
	contentType     string
	sizeBytes       int64
}

type PartURL struct {
	PartNumber int       `json:"part_number"`
	URL        string    `json:"url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type CompletedPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
}

type MediaResult struct {
	ID     uuid.UUID `json:"id"`
	Status string    `json:"status"`
}

func NewService(db *pgxpool.Pool, store ObjectStore) *Service {
	return &Service{db: db, store: store, now: time.Now}
}

func (s *Service) Initiate(ctx context.Context, identityID, idempotencyKey uuid.UUID, slug string, input InitiateInput) (Upload, error) {
	mediaType, normalized, err := validateInitiate(input)
	if err != nil {
		return Upload{}, err
	}
	input = normalized
	requestHash := hashInitiateInput(slug, input)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Upload{}, fmt.Errorf("begin upload transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lockKey := identityID.String() + ":" + idempotencyKey.String()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return Upload{}, fmt.Errorf("lock upload idempotency key: %w", err)
	}

	existing, found, err := findIdempotentUpload(ctx, tx, identityID, idempotencyKey, requestHash)
	if err != nil {
		return Upload{}, err
	}
	if found {
		if err := tx.Commit(ctx); err != nil {
			return Upload{}, fmt.Errorf("commit idempotent upload read: %w", err)
		}
		return s.attachSingleURL(ctx, existing)
	}

	roomID, roomExpiry, err := lockRoomForUpload(ctx, tx, identityID, slug, input.SizeBytes)
	if err != nil {
		return Upload{}, err
	}
	if input.Checksum != "" {
		var duplicate bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM media
				WHERE room_id = $1 AND checksum = $2
				  AND status IN ('pending', 'processing', 'ready')
			)
		`, roomID, input.Checksum).Scan(&duplicate); err != nil {
			return Upload{}, fmt.Errorf("check duplicate media: %w", err)
		}
		if duplicate {
			return Upload{}, ErrMediaDuplicate
		}
	}
	now := s.now().UTC()
	sessionExpiry := now.Add(uploadLifetime)
	if roomExpiry.Before(sessionExpiry) {
		sessionExpiry = roomExpiry
	}

	mediaID := uuid.New()
	objectKey := fmt.Sprintf("rooms/%s/originals/%s", roomID, mediaID)
	uploadType := "single"
	var storageUploadID *string
	var partSize *int64
	var partsCount *int
	if input.SizeBytes > multipartThreshold {
		uploadType = "multipart"
		started, err := s.store.StartMultipart(ctx, objectKey, input.MIMEType)
		if err != nil {
			return Upload{}, err
		}
		storageUploadID = &started
		value := multipartPartSize
		count := int((input.SizeBytes + multipartPartSize - 1) / multipartPartSize)
		partSize = &value
		partsCount = &count
	}
	committed := false
	if storageUploadID != nil {
		defer func() {
			if !committed {
				_ = s.store.AbortMultipart(context.Background(), objectKey, *storageUploadID)
			}
		}()
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO media (
			id, room_id, uploader_identity_id, status, media_type, original_filename,
			mime_type, size_bytes, storage_key, captured_at, checksum, created_at
		)
		VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11)
	`, mediaID, roomID, identityID, mediaType, input.Filename, input.MIMEType, input.SizeBytes, objectKey, input.CapturedAt, input.Checksum, now); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "media_room_checksum_active_uidx" {
			return Upload{}, ErrMediaDuplicate
		}
		return Upload{}, fmt.Errorf("create media metadata: %w", err)
	}

	uploadID := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO upload_sessions (
			id, media_id, identity_id, storage_upload_id, upload_type, status,
			part_size_bytes, parts_count, expires_at, created_at, idempotency_key, request_hash
		)
		VALUES ($1, $2, $3, $4, $5, 'initiated', $6, $7, $8, $9, $10, $11)
	`, uploadID, mediaID, identityID, storageUploadID, uploadType, partSize, partsCount, sessionExpiry, now, idempotencyKey, requestHash[:]); err != nil {
		return Upload{}, fmt.Errorf("create upload session: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE rooms
		SET reserved_files = reserved_files + 1,
		    reserved_storage_bytes = reserved_storage_bytes + $1
		WHERE id = $2
	`, input.SizeBytes, roomID); err != nil {
		return Upload{}, fmt.Errorf("reserve room capacity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Upload{}, fmt.Errorf("commit upload session: %w", err)
	}
	committed = true

	created := Upload{
		ID: uploadID, MediaID: mediaID, Type: uploadType, Status: "initiated",
		PartSizeBytes: partSize, PartsCount: partsCount, ExpiresAt: sessionExpiry,
		objectKey: objectKey, storageUploadID: storageUploadID,
		contentType: input.MIMEType, sizeBytes: input.SizeBytes,
	}
	return s.attachSingleURL(ctx, created)
}

func (s *Service) PartURLs(ctx context.Context, identityID, uploadID uuid.UUID, partNumbers []int) ([]PartURL, error) {
	if len(partNumbers) < 1 || len(partNumbers) > maxPartsBatch {
		return nil, fmt.Errorf("%w: request between 1 and %d parts", ErrInvalidUploadParts, maxPartsBatch)
	}
	upload, err := s.getUpload(ctx, identityID, uploadID)
	if err != nil {
		return nil, err
	}
	if upload.Type != "multipart" || upload.PartsCount == nil || upload.storageUploadID == nil {
		return nil, fmt.Errorf("%w: upload is not multipart", ErrInvalidUploadParts)
	}
	if upload.Status == "completed" {
		return nil, ErrUploadCompleted
	}
	if upload.Status == "aborted" || upload.Status == "expired" || !upload.ExpiresAt.After(s.now()) {
		return nil, ErrUploadExpired
	}

	seen := make(map[int]struct{}, len(partNumbers))
	urls := make([]PartURL, 0, len(partNumbers))
	for _, number := range partNumbers {
		if number < 1 || number > *upload.PartsCount {
			return nil, fmt.Errorf("%w: part number is out of range", ErrInvalidUploadParts)
		}
		if _, exists := seen[number]; exists {
			return nil, fmt.Errorf("%w: duplicate part number", ErrInvalidUploadParts)
		}
		seen[number] = struct{}{}
		presigned, expiry, err := s.store.PresignPart(ctx, upload.objectKey, *upload.storageUploadID, number, upload.ExpiresAt)
		if err != nil {
			return nil, err
		}
		urls = append(urls, PartURL{PartNumber: number, URL: presigned, ExpiresAt: expiry})
	}
	if _, err := s.db.Exec(ctx, `UPDATE upload_sessions SET status = 'uploading' WHERE id = $1 AND status = 'initiated'`, uploadID); err != nil {
		return nil, fmt.Errorf("mark upload in progress: %w", err)
	}
	return urls, nil
}

func (s *Service) Complete(ctx context.Context, identityID, uploadID uuid.UUID, parts []CompletedPart) (MediaResult, error) {
	upload, err := s.getUpload(ctx, identityID, uploadID)
	if err != nil {
		return MediaResult{}, err
	}
	if upload.Status == "completed" {
		return MediaResult{ID: upload.MediaID, Status: "ready"}, nil
	}
	if upload.Status == "aborted" || upload.Status == "expired" || !upload.ExpiresAt.After(s.now()) {
		return MediaResult{}, ErrUploadExpired
	}

	if upload.Type == "multipart" {
		storageParts, err := validateCompletedParts(parts, upload.PartsCount)
		if err != nil {
			return MediaResult{}, err
		}
		if upload.storageUploadID == nil {
			return MediaResult{}, errors.New("multipart upload has no storage upload ID")
		}
		if err := s.store.CompleteMultipart(ctx, upload.objectKey, *upload.storageUploadID, upload.contentType, storageParts); err != nil {
			if _, statErr := s.store.Stat(ctx, upload.objectKey); statErr != nil {
				return MediaResult{}, err
			}
		}
	} else if len(parts) != 0 {
		return MediaResult{}, fmt.Errorf("%w: single upload cannot contain parts", ErrInvalidUploadParts)
	}

	info, err := s.store.Stat(ctx, upload.objectKey)
	if err != nil {
		if errors.Is(err, objectstore.ErrObjectNotFound) {
			return MediaResult{}, ErrUploadNotReady
		}
		return MediaResult{}, err
	}
	if info.Size != upload.sizeBytes {
		_ = s.store.Remove(ctx, upload.objectKey)
		if releaseErr := s.releaseReservation(ctx, upload, "aborted", "failed"); releaseErr != nil {
			return MediaResult{}, releaseErr
		}
		return MediaResult{}, ErrUploadedSizeMismatch
	}
	valid, err := s.validateStoredMedia(ctx, upload.objectKey, upload.contentType)
	if err != nil {
		return MediaResult{}, err
	}
	if !valid {
		_ = s.store.Remove(ctx, upload.objectKey)
		if releaseErr := s.releaseReservation(ctx, upload, "aborted", "failed"); releaseErr != nil {
			return MediaResult{}, releaseErr
		}
		return MediaResult{}, ErrUploadedTypeMismatch
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return MediaResult{}, fmt.Errorf("begin complete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM upload_sessions WHERE id = $1 FOR UPDATE`, upload.ID).Scan(&status); err != nil {
		return MediaResult{}, fmt.Errorf("lock upload for completion: %w", err)
	}
	if status == "completed" {
		if err := tx.Commit(ctx); err != nil {
			return MediaResult{}, err
		}
		return MediaResult{ID: upload.MediaID, Status: "ready"}, nil
	}
	if status == "aborted" || status == "expired" {
		return MediaResult{}, ErrUploadExpired
	}
	if _, err := tx.Exec(ctx, `UPDATE upload_sessions SET status = 'completed', completed_at = now() WHERE id = $1`, upload.ID); err != nil {
		return MediaResult{}, fmt.Errorf("complete upload row: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media SET status = 'ready', ready_at = now() WHERE id = $1`, upload.MediaID); err != nil {
		return MediaResult{}, fmt.Errorf("mark media ready: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE rooms
		SET reserved_files = reserved_files - 1,
		    reserved_storage_bytes = reserved_storage_bytes - $1,
		    used_files = used_files + 1,
		    used_storage_bytes = used_storage_bytes + $1,
		    media_revision = media_revision + 1
		WHERE id = (SELECT room_id FROM media WHERE id = $2)
	`, upload.sizeBytes, upload.MediaID); err != nil {
		return MediaResult{}, fmt.Errorf("commit room capacity: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO telegram_notification_outbox (room_id, telegram_user_id, event_type, payload, dedupe_key, available_at)
		SELECT room.id, recipient.telegram_user_id, 'media_uploaded', jsonb_build_object(
			'room_name', room.name, 'room_slug', room.slug, 'actor_name', uploader.display_name,
			'filename', media.original_filename, 'count', 1
		), 'media:' || room.id || ':' || uploader.id || ':' || recipient.id || ':' || floor(extract(epoch from now()) / 300)::bigint, now() + interval '15 seconds'
		FROM media
		JOIN rooms room ON room.id = media.room_id
		JOIN identities uploader ON uploader.id = media.uploader_identity_id
		JOIN room_notification_preferences preference ON preference.room_id = room.id
		JOIN identities recipient ON recipient.id = preference.identity_id
		WHERE media.id = $1 AND preference.telegram_enabled AND preference.new_media_enabled
		  AND recipient.telegram_user_id IS NOT NULL AND recipient.id <> uploader.id
		ON CONFLICT (dedupe_key) DO UPDATE
		SET payload = jsonb_set(telegram_notification_outbox.payload, '{count}', to_jsonb(COALESCE((telegram_notification_outbox.payload->>'count')::int, 1) + 1)),
		    available_at = now() + interval '15 seconds'
		WHERE telegram_notification_outbox.sent_at IS NULL AND telegram_notification_outbox.failed_at IS NULL
	`, upload.MediaID); err != nil {
		return MediaResult{}, fmt.Errorf("enqueue media notification: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return MediaResult{}, fmt.Errorf("commit upload completion: %w", err)
	}
	return MediaResult{ID: upload.MediaID, Status: "ready"}, nil
}

func (s *Service) validateStoredMedia(ctx context.Context, objectKey, claimedMIME string) (bool, error) {
	reader, err := s.store.Open(ctx, objectKey)
	if err != nil {
		return false, err
	}
	defer reader.Close()
	header := make([]byte, 64*1024)
	count, err := io.ReadFull(reader, header)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return false, fmt.Errorf("read uploaded object header: %w", err)
	}
	return mediaHeaderMatchesMIME(header[:count], claimedMIME), nil
}

func (s *Service) Abort(ctx context.Context, identityID, uploadID uuid.UUID) error {
	upload, err := s.getUpload(ctx, identityID, uploadID)
	if err != nil {
		return err
	}
	if upload.Status == "completed" {
		return ErrUploadCompleted
	}
	if upload.Status == "aborted" || upload.Status == "expired" {
		return nil
	}
	if upload.Type == "multipart" && upload.storageUploadID != nil {
		if err := s.store.AbortMultipart(ctx, upload.objectKey, *upload.storageUploadID); err != nil {
			return err
		}
	} else {
		_ = s.store.Remove(ctx, upload.objectKey)
	}
	return s.releaseReservation(ctx, upload, "aborted", "deleted")
}

func (s *Service) attachSingleURL(ctx context.Context, upload Upload) (Upload, error) {
	if upload.Type != "single" || upload.Status == "completed" {
		return upload, nil
	}
	if upload.Status == "aborted" || upload.Status == "expired" {
		return Upload{}, ErrUploadExpired
	}
	if !upload.ExpiresAt.After(s.now()) {
		return Upload{}, ErrUploadExpired
	}
	presigned, expiry, err := s.store.PresignPut(ctx, upload.objectKey, upload.contentType, upload.ExpiresAt)
	if err != nil {
		return Upload{}, err
	}
	upload.URL = presigned
	upload.URLExpiresAt = expiry
	upload.Method = "PUT"
	upload.Headers = map[string]string{"Content-Type": upload.contentType}
	return upload, nil
}

func (s *Service) getUpload(ctx context.Context, identityID, uploadID uuid.UUID) (Upload, error) {
	var upload Upload
	err := s.db.QueryRow(ctx, `
		SELECT u.id, u.media_id, u.upload_type, u.status, u.part_size_bytes, u.parts_count,
		       u.expires_at, u.storage_upload_id, m.storage_key, m.mime_type, m.size_bytes
		FROM upload_sessions u
		JOIN media m ON m.id = u.media_id
		WHERE u.id = $1 AND u.identity_id = $2
	`, uploadID, identityID).Scan(
		&upload.ID, &upload.MediaID, &upload.Type, &upload.Status, &upload.PartSizeBytes,
		&upload.PartsCount, &upload.ExpiresAt, &upload.storageUploadID,
		&upload.objectKey, &upload.contentType, &upload.sizeBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Upload{}, ErrUploadNotFound
	}
	if err != nil {
		return Upload{}, fmt.Errorf("get upload session: %w", err)
	}
	return upload, nil
}

func (s *Service) releaseReservation(ctx context.Context, upload Upload, uploadStatus, mediaStatus string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current string
	if err := tx.QueryRow(ctx, `SELECT status FROM upload_sessions WHERE id = $1 FOR UPDATE`, upload.ID).Scan(&current); err != nil {
		return err
	}
	if current == "completed" {
		return ErrUploadCompleted
	}
	if current == "aborted" || current == "expired" {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE upload_sessions SET status = $1 WHERE id = $2`, uploadStatus, upload.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE media SET status = $1, deleted_at = now() WHERE id = $2`, mediaStatus, upload.MediaID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE rooms
		SET reserved_files = reserved_files - 1,
		    reserved_storage_bytes = reserved_storage_bytes - $1
		WHERE id = (SELECT room_id FROM media WHERE id = $2)
	`, upload.sizeBytes, upload.MediaID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func findIdempotentUpload(ctx context.Context, tx pgx.Tx, identityID, key uuid.UUID, expectedHash [32]byte) (Upload, bool, error) {
	var upload Upload
	var storedHash []byte
	err := tx.QueryRow(ctx, `
		SELECT u.id, u.media_id, u.upload_type, u.status, u.part_size_bytes, u.parts_count,
		       u.expires_at, u.storage_upload_id, u.request_hash,
		       m.storage_key, m.mime_type, m.size_bytes
		FROM upload_sessions u
		JOIN media m ON m.id = u.media_id
		WHERE u.identity_id = $1 AND u.idempotency_key = $2
	`, identityID, key).Scan(
		&upload.ID, &upload.MediaID, &upload.Type, &upload.Status, &upload.PartSizeBytes,
		&upload.PartsCount, &upload.ExpiresAt, &upload.storageUploadID, &storedHash,
		&upload.objectKey, &upload.contentType, &upload.sizeBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Upload{}, false, nil
	}
	if err != nil {
		return Upload{}, false, fmt.Errorf("find idempotent upload: %w", err)
	}
	if !bytes.Equal(storedHash, expectedHash[:]) {
		return Upload{}, false, ErrIdempotencyConflict
	}
	return upload, true, nil
}

func lockRoomForUpload(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, slug string, size int64) (uuid.UUID, time.Time, error) {
	var roomID uuid.UUID
	var status string
	var accepting bool
	var canUpload bool
	var expiresAt time.Time
	var maxFiles, usedFiles, reservedFiles int
	var maxBytes, usedBytes, reservedBytes int64
	err := tx.QueryRow(ctx, `
		SELECT r.id, r.status, r.accepting_uploads, rm.can_upload, r.expires_at,
		       r.max_files, r.used_files, r.reserved_files,
		       r.max_storage_bytes, r.used_storage_bytes, r.reserved_storage_bytes
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = upper(trim($2))
		FOR UPDATE OF r
	`, identityID, slug).Scan(
		&roomID, &status, &accepting, &canUpload, &expiresAt,
		&maxFiles, &usedFiles, &reservedFiles,
		&maxBytes, &usedBytes, &reservedBytes,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if checkErr := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rooms WHERE slug = upper(trim($1)))`, slug).Scan(&exists); checkErr != nil {
			return uuid.Nil, time.Time{}, checkErr
		}
		if exists {
			return uuid.Nil, time.Time{}, ErrNotMember
		}
		return uuid.Nil, time.Time{}, ErrRoomNotFound
	}
	if err != nil {
		return uuid.Nil, time.Time{}, fmt.Errorf("lock room for upload: %w", err)
	}
	if status != "active" || !expiresAt.After(time.Now()) {
		return uuid.Nil, time.Time{}, ErrRoomExpired
	}
	if !accepting {
		return uuid.Nil, time.Time{}, ErrUploadsClosed
	}
	if !canUpload {
		return uuid.Nil, time.Time{}, ErrUploadPermission
	}
	if usedFiles+reservedFiles+1 > maxFiles || usedBytes+reservedBytes+size > maxBytes {
		return uuid.Nil, time.Time{}, ErrRoomLimitReached
	}
	return roomID, expiresAt, nil
}

func validateInitiate(input InitiateInput) (string, InitiateInput, error) {
	input.Filename = strings.TrimSpace(input.Filename)
	if count := utf8.RuneCountInString(input.Filename); count < 1 || count > 255 {
		return "", InitiateInput{}, fmt.Errorf("%w: filename must contain 1 to 255 characters", ErrInvalidInput)
	}
	for _, character := range input.Filename {
		if character == 0 || unicode.IsControl(character) {
			return "", InitiateInput{}, fmt.Errorf("%w: filename contains control characters", ErrInvalidInput)
		}
	}
	input.MIMEType = strings.ToLower(strings.TrimSpace(strings.Split(input.MIMEType, ";")[0]))
	mediaType, supported := supportedMIMETypes[input.MIMEType]
	if !supported {
		return "", InitiateInput{}, fmt.Errorf("%w: unsupported MIME type", ErrInvalidInput)
	}
	if input.SizeBytes <= 0 {
		return "", InitiateInput{}, fmt.Errorf("%w: size_bytes must be positive", ErrInvalidInput)
	}
	if mediaType == "image" && input.SizeBytes > maxImageSize {
		return "", InitiateInput{}, fmt.Errorf("%w: image exceeds 50 MiB", ErrInvalidInput)
	}
	if mediaType == "video" && input.SizeBytes > maxVideoSize {
		return "", InitiateInput{}, fmt.Errorf("%w: video exceeds 2 GiB", ErrInvalidInput)
	}
	input.Checksum = strings.ToLower(strings.TrimSpace(input.Checksum))
	if input.Checksum != "" {
		decoded, err := hex.DecodeString(input.Checksum)
		if err != nil || len(decoded) != sha256.Size {
			return "", InitiateInput{}, fmt.Errorf("%w: checksum must be a SHA-256 hex digest", ErrInvalidInput)
		}
	}
	return mediaType, input, nil
}

func hashInitiateInput(slug string, input InitiateInput) [32]byte {
	captured := ""
	if input.CapturedAt != nil {
		captured = input.CapturedAt.UTC().Format(time.RFC3339Nano)
	}
	canonical := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", strings.ToUpper(strings.TrimSpace(slug)), input.Filename, input.MIMEType, input.SizeBytes, captured, input.Checksum)
	return sha256.Sum256([]byte(canonical))
}

func validateCompletedParts(parts []CompletedPart, expectedCount *int) ([]objectstore.CompletedPart, error) {
	if expectedCount == nil || len(parts) != *expectedCount {
		return nil, fmt.Errorf("%w: all multipart ETags are required", ErrInvalidUploadParts)
	}
	sort.Slice(parts, func(left, right int) bool { return parts[left].PartNumber < parts[right].PartNumber })
	result := make([]objectstore.CompletedPart, len(parts))
	for index, part := range parts {
		if part.PartNumber != index+1 || strings.TrimSpace(part.ETag) == "" || len(part.ETag) > 256 {
			return nil, fmt.Errorf("%w: parts must be unique, sequential, and contain ETags", ErrInvalidUploadParts)
		}
		result[index] = objectstore.CompletedPart{PartNumber: part.PartNumber, ETag: strings.TrimSpace(part.ETag)}
	}
	return result, nil
}
