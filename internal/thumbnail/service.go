package thumbnail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxAttempts    = 3
	sourceURLTTL   = 10 * time.Minute
	maxErrorLength = 1000
)

type ObjectStore interface {
	PresignInternalGet(context.Context, string, time.Duration) (string, error)
	PutFile(context.Context, string, string, string) error
	Remove(context.Context, string) error
}

type Service struct {
	db       *pgxpool.Pool
	store    ObjectStore
	interval time.Duration
	logger   *slog.Logger
}

type job struct {
	mediaID    uuid.UUID
	roomID     uuid.UUID
	mediaType  string
	storageKey string
}

func New(db *pgxpool.Pool, store ObjectStore, interval time.Duration, logger *slog.Logger) *Service {
	return &Service{db: db, store: store, interval: interval, logger: logger}
}

func (s *Service) Run(ctx context.Context) error {
	s.runBatch(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.runBatch(ctx)
		}
	}
}

func (s *Service) runBatch(ctx context.Context) {
	for {
		claimed, err := s.claim(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			s.logger.Error("claim thumbnail job failed", "error", err)
			return
		}
		if err := s.process(ctx, claimed); err != nil {
			s.logger.Warn("thumbnail generation failed", "media_id", claimed.mediaID, "error", err)
			if markErr := s.markFailed(ctx, claimed.mediaID, err); markErr != nil {
				s.logger.Error("mark thumbnail failed", "media_id", claimed.mediaID, "error", markErr)
			}
			continue
		}
		s.logger.Info("thumbnail generated", "media_id", claimed.mediaID)
	}
}

func (s *Service) claim(ctx context.Context) (job, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return job{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var claimed job
	err = tx.QueryRow(ctx, `
		SELECT m.id, m.room_id, m.media_type, m.storage_key
		FROM media m
		JOIN rooms r ON r.id = m.room_id
		WHERE m.status = 'ready'
		  AND m.thumbnail_key IS NULL
		  AND r.status = 'active'
		  AND r.expires_at > now()
		  AND (
		      m.thumbnail_status = 'pending'
		      OR (m.thumbnail_status = 'failed' AND m.thumbnail_attempts < $1)
		      OR (m.thumbnail_status = 'processing' AND m.thumbnail_updated_at < now() - interval '10 minutes')
		  )
		ORDER BY m.created_at
		FOR UPDATE OF m SKIP LOCKED
		LIMIT 1
	`, maxAttempts).Scan(&claimed.mediaID, &claimed.roomID, &claimed.mediaType, &claimed.storageKey)
	if err != nil {
		return job{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE media
		SET thumbnail_status = 'processing',
		    thumbnail_attempts = thumbnail_attempts + 1,
		    thumbnail_error = NULL,
		    thumbnail_updated_at = now()
		WHERE id = $1
	`, claimed.mediaID); err != nil {
		return job{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return job{}, err
	}
	return claimed, nil
}

func (s *Service) process(ctx context.Context, claimed job) error {
	sourceURL, err := s.store.PresignInternalGet(ctx, claimed.storageKey, sourceURLTTL)
	if err != nil {
		return err
	}
	output, err := os.CreateTemp("", "photodrop-thumbnail-*.jpg")
	if err != nil {
		return fmt.Errorf("create thumbnail temp file: %w", err)
	}
	outputPath := output.Name()
	if err := output.Close(); err != nil {
		return err
	}
	defer os.Remove(outputPath)

	if err := runFFmpeg(ctx, claimed.mediaType, sourceURL, outputPath); err != nil {
		return err
	}
	info, err := os.Stat(outputPath)
	if err != nil || info.Size() == 0 {
		return errors.New("ffmpeg produced no thumbnail")
	}

	thumbnailKey := fmt.Sprintf("rooms/%s/thumbnails/%s.jpg", claimed.roomID, claimed.mediaID)
	if err := s.store.PutFile(ctx, thumbnailKey, outputPath, "image/jpeg"); err != nil {
		return err
	}
	result, err := s.db.Exec(ctx, `
		UPDATE media
		SET thumbnail_key = $1,
		    thumbnail_status = 'ready',
		    thumbnail_error = NULL,
		    thumbnail_updated_at = now()
		WHERE id = $2 AND status = 'ready'
	`, thumbnailKey, claimed.mediaID)
	if err != nil {
		_ = s.store.Remove(ctx, thumbnailKey)
		return err
	}
	if result.RowsAffected() != 1 {
		_ = s.store.Remove(ctx, thumbnailKey)
		return errors.New("media disappeared while generating thumbnail")
	}
	return nil
}

func (s *Service) markFailed(ctx context.Context, mediaID uuid.UUID, cause error) error {
	message := cause.Error()
	if len(message) > maxErrorLength {
		message = message[:maxErrorLength]
	}
	_, err := s.db.Exec(ctx, `
		UPDATE media
		SET thumbnail_status = 'failed', thumbnail_error = $1, thumbnail_updated_at = now()
		WHERE id = $2 AND thumbnail_status = 'processing'
	`, message, mediaID)
	return err
}

func runFFmpeg(ctx context.Context, mediaType, sourceURL, outputPath string) error {
	base := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if mediaType == "video" {
		base = append(base, "-ss", "1")
	}
	arguments := append(base,
		"-i", sourceURL,
		"-vf", "scale=480:-2:force_original_aspect_ratio=decrease",
		"-frames:v", "1",
		"-q:v", "4",
		outputPath,
	)
	output, err := exec.CommandContext(ctx, "ffmpeg", arguments...).CombinedOutput()
	if err != nil && mediaType == "video" {
		fallback := []string{
			"-hide_banner", "-loglevel", "error", "-y",
			"-i", sourceURL,
			"-vf", "scale=480:-2:force_original_aspect_ratio=decrease",
			"-frames:v", "1", "-q:v", "4", outputPath,
		}
		output, err = exec.CommandContext(ctx, "ffmpeg", fallback...).CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(output)))
	}
	return nil
}
