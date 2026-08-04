package roomarchive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotMember = errors.New("room membership required")
	ErrExpired   = errors.New("room expired")
	ErrEmpty     = errors.New("room has no media")
	ErrNotFound  = errors.New("archive not found")
	ErrNotReady  = errors.New("archive is not ready")
)

type downloadStore interface {
	PresignGet(context.Context, string, string) (string, time.Time, error)
}

type Service struct {
	db    *pgxpool.Pool
	store downloadStore
}

type Job struct {
	ID             uuid.UUID  `json:"id"`
	RoomSlug       string     `json:"room_slug"`
	Status         string     `json:"status"`
	Filename       string     `json:"filename"`
	TotalFiles     int        `json:"total_files"`
	ProcessedFiles int        `json:"processed_files"`
	TotalBytes     int64      `json:"total_bytes"`
	ProcessedBytes int64      `json:"processed_bytes"`
	SizeBytes      *int64     `json:"size_bytes,omitempty"`
	Error          *string    `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`

	objectKey string
}

type Download struct {
	URL       string    `json:"url"`
	Filename  string    `json:"filename"`
	ExpiresAt time.Time `json:"expires_at"`
}

func NewService(db *pgxpool.Pool, store downloadStore) *Service {
	return &Service{db: db, store: store}
}

func (s *Service) Request(ctx context.Context, identityID uuid.UUID, slug string) (Job, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Job{}, fmt.Errorf("begin archive request: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var roomID uuid.UUID
	var roomName, roomStatus string
	var expiresAt time.Time
	var revision int64
	var totalFiles int
	var totalBytes int64
	err = tx.QueryRow(ctx, `
		SELECT r.id, r.name, r.status, r.expires_at, r.media_revision
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = upper(trim($2))
		FOR UPDATE OF r
	`, identityID, slug).Scan(&roomID, &roomName, &roomStatus, &expiresAt, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotMember
	}
	if err != nil {
		return Job{}, fmt.Errorf("authorize archive request: %w", err)
	}
	if roomStatus != "active" || !expiresAt.After(time.Now()) {
		return Job{}, ErrExpired
	}
	if err := tx.QueryRow(ctx, `
		SELECT count(*)::integer, coalesce(sum(size_bytes), 0)::bigint
		FROM media WHERE room_id = $1 AND status = 'ready'
	`, roomID).Scan(&totalFiles, &totalBytes); err != nil {
		return Job{}, fmt.Errorf("count archive media: %w", err)
	}
	if totalFiles == 0 {
		return Job{}, ErrEmpty
	}

	jobID := uuid.New()
	filename := archiveFilename(roomName)
	objectKey := fmt.Sprintf("rooms/%s/archives/%s.zip", roomID, jobID)
	_, err = tx.Exec(ctx, `
		INSERT INTO room_archives (
			id, room_id, requested_by_identity_id, source_revision, object_key,
			filename, total_files, total_bytes
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (room_id, source_revision) DO UPDATE
		SET status = CASE WHEN room_archives.status = 'failed' THEN 'pending' ELSE room_archives.status END,
		    requested_by_identity_id = CASE WHEN room_archives.status = 'failed' THEN EXCLUDED.requested_by_identity_id ELSE room_archives.requested_by_identity_id END,
		    attempts = CASE WHEN room_archives.status = 'failed' THEN 0 ELSE room_archives.attempts END,
		    processed_files = CASE WHEN room_archives.status = 'failed' THEN 0 ELSE room_archives.processed_files END,
		    processed_bytes = CASE WHEN room_archives.status = 'failed' THEN 0 ELSE room_archives.processed_bytes END,
		    error = CASE WHEN room_archives.status = 'failed' THEN NULL ELSE room_archives.error END,
		    updated_at = CASE WHEN room_archives.status = 'failed' THEN now() ELSE room_archives.updated_at END
	`, jobID, roomID, identityID, revision, objectKey, filename, totalFiles, totalBytes)
	if err != nil {
		return Job{}, fmt.Errorf("create archive job: %w", err)
	}

	job, err := scanJob(tx.QueryRow(ctx, jobSelect+` WHERE a.room_id = $1 AND a.source_revision = $2`, roomID, revision))
	if err != nil {
		return Job{}, fmt.Errorf("read archive job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Job{}, fmt.Errorf("commit archive request: %w", err)
	}
	return job, nil
}

func (s *Service) Get(ctx context.Context, identityID, archiveID uuid.UUID) (Job, error) {
	job, err := scanJob(s.db.QueryRow(ctx, jobSelect+`
		JOIN room_members rm ON rm.room_id = a.room_id AND rm.identity_id = $2
		WHERE a.id = $1
	`, archiveID, identityID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("get archive job: %w", err)
	}
	return job, nil
}

func (s *Service) Download(ctx context.Context, identityID, archiveID uuid.UUID) (Download, error) {
	job, err := s.Get(ctx, identityID, archiveID)
	if err != nil {
		return Download{}, err
	}
	if job.Status != "ready" {
		return Download{}, ErrNotReady
	}
	url, expiresAt, err := s.store.PresignGet(ctx, job.objectKey, job.Filename)
	if err != nil {
		return Download{}, err
	}
	return Download{URL: url, Filename: job.Filename, ExpiresAt: expiresAt}, nil
}

const jobSelect = `
	SELECT a.id, r.slug, a.status, a.filename, a.total_files, a.processed_files,
	       a.total_bytes, a.processed_bytes, a.size_bytes, a.error,
	       a.created_at, a.updated_at, a.completed_at, a.object_key
	FROM room_archives a
	JOIN rooms r ON r.id = a.room_id
`

type rowScanner interface {
	Scan(...any) error
}

func scanJob(row rowScanner) (Job, error) {
	var job Job
	err := row.Scan(
		&job.ID, &job.RoomSlug, &job.Status, &job.Filename, &job.TotalFiles, &job.ProcessedFiles,
		&job.TotalBytes, &job.ProcessedBytes, &job.SizeBytes, &job.Error,
		&job.CreatedAt, &job.UpdatedAt, &job.CompletedAt, &job.objectKey,
	)
	return job, err
}

func archiveFilename(roomName string) string {
	cleaned := strings.Map(func(character rune) rune {
		if character < 32 || strings.ContainsRune(`<>:"/\|?*`, character) {
			return '_'
		}
		return character
	}, strings.TrimSpace(roomName))
	cleaned = strings.Trim(cleaned, " .")
	if cleaned == "" {
		cleaned = "Zhyvo"
	}
	return cleaned + " — фото.zip"
}
