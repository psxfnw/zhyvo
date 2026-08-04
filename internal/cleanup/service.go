package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photodrop/internal/objectstore"
)

type Service struct {
	db       *pgxpool.Pool
	store    *objectstore.Store
	interval time.Duration
	logger   *slog.Logger
}

func New(db *pgxpool.Pool, store *objectstore.Store, interval time.Duration, logger *slog.Logger) *Service {
	return &Service{db: db, store: store, interval: interval, logger: logger}
}

func (s *Service) Run(ctx context.Context) error {
	if err := s.runBatch(ctx); err != nil {
		s.logger.Error("initial cleanup failed", "error", err)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.runBatch(ctx); err != nil {
				s.logger.Error("cleanup batch failed", "error", err)
			}
		}
	}
}

func (s *Service) runBatch(ctx context.Context) error {
	if err := s.expireUploads(ctx); err != nil {
		return err
	}
	for {
		roomID, err := s.claimRoom(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		prefix := fmt.Sprintf("rooms/%s/", roomID)
		if err := s.store.RemovePrefix(ctx, prefix); err != nil {
			s.logger.Error("delete room objects failed", "room_id", roomID, "error", err)
			return err
		}

		result, err := s.db.Exec(ctx, `DELETE FROM rooms WHERE id = $1 AND status = 'deleting'`, roomID)
		if err != nil {
			return fmt.Errorf("delete room metadata: %w", err)
		}
		if result.RowsAffected() == 1 {
			s.logger.Info("expired room deleted", "room_id", roomID)
		}
	}
}

type expiredUpload struct {
	id              uuid.UUID
	typeName        string
	storageUploadID *string
	objectKey       string
}

func (s *Service) expireUploads(ctx context.Context) error {
	for {
		upload, err := s.claimExpiredUpload(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		if upload.typeName == "multipart" && upload.storageUploadID != nil {
			if err := s.store.AbortMultipart(ctx, upload.objectKey, *upload.storageUploadID); err != nil {
				s.logger.Warn("abort expired multipart upload failed", "upload_id", upload.id, "error", err)
			}
		} else if err := s.store.Remove(ctx, upload.objectKey); err != nil {
			s.logger.Warn("remove expired upload object failed", "upload_id", upload.id, "error", err)
		}
		s.logger.Info("expired upload reservation released", "upload_id", upload.id)
	}
}

func (s *Service) claimExpiredUpload(ctx context.Context) (expiredUpload, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return expiredUpload{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var upload expiredUpload
	var mediaID, roomID uuid.UUID
	var sizeBytes int64
	err = tx.QueryRow(ctx, `
		SELECT u.id, u.upload_type, u.storage_upload_id,
		       m.id, m.room_id, m.storage_key, m.size_bytes
		FROM upload_sessions u
		JOIN media m ON m.id = u.media_id
		JOIN rooms r ON r.id = m.room_id
		WHERE u.status IN ('initiated', 'uploading')
		  AND u.expires_at <= now()
		ORDER BY u.expires_at
		FOR UPDATE OF u, r SKIP LOCKED
		LIMIT 1
	`).Scan(
		&upload.id, &upload.typeName, &upload.storageUploadID,
		&mediaID, &roomID, &upload.objectKey, &sizeBytes,
	)
	if err != nil {
		return expiredUpload{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE upload_sessions SET status = 'expired' WHERE id = $1`, upload.id); err != nil {
		return expiredUpload{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE media SET status = 'failed', deleted_at = now() WHERE id = $1`, mediaID); err != nil {
		return expiredUpload{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE rooms
		SET reserved_files = reserved_files - 1,
		    reserved_storage_bytes = reserved_storage_bytes - $1
		WHERE id = $2
	`, sizeBytes, roomID); err != nil {
		return expiredUpload{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return expiredUpload{}, err
	}
	return upload, nil
}

func (s *Service) claimRoom(ctx context.Context) (uuid.UUID, error) {
	var roomID uuid.UUID
	err := s.db.QueryRow(ctx, `
		WITH candidate AS (
			SELECT id
			FROM rooms
			WHERE (status = 'active' AND expires_at <= now()) OR status = 'deleting'
			ORDER BY expires_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		UPDATE rooms
		SET status = 'deleting'
		FROM candidate
		WHERE rooms.id = candidate.id
		RETURNING rooms.id
	`).Scan(&roomID)
	if err != nil {
		return uuid.Nil, err
	}
	return roomID, nil
}
