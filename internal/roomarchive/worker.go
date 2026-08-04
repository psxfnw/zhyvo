package roomarchive

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxArchiveAttempts = 3

type workerStore interface {
	Open(context.Context, string) (io.ReadCloser, error)
	PutReader(context.Context, string, io.Reader, string) (int64, error)
	Remove(context.Context, string) error
}

type Worker struct {
	db       *pgxpool.Pool
	store    workerStore
	interval time.Duration
	logger   *slog.Logger
}

type claimedJob struct {
	id             uuid.UUID
	roomID         uuid.UUID
	sourceRevision int64
	objectKey      string
}

type archiveItem struct {
	storageKey string
	filename   string
	sizeBytes  int64
}

func NewWorker(db *pgxpool.Pool, store workerStore, interval time.Duration, logger *slog.Logger) *Worker {
	return &Worker{db: db, store: store, interval: interval, logger: logger}
}

func (w *Worker) Run(ctx context.Context) error {
	if err := w.runOne(ctx); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		w.logger.Error("initial archive job failed", "error", err)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.runOne(ctx); err != nil && !errors.Is(err, pgx.ErrNoRows) {
				w.logger.Error("archive job failed", "error", err)
			}
		}
	}
}

func (w *Worker) runOne(ctx context.Context) error {
	job, err := w.claim(ctx)
	if err != nil {
		return err
	}
	if err := w.build(ctx, job); err != nil {
		_ = w.store.Remove(ctx, job.objectKey)
		if markErr := w.markAttemptFailed(ctx, job.id, err); markErr != nil {
			return fmt.Errorf("archive build failed: %v; mark failed: %w", err, markErr)
		}
		return fmt.Errorf("build archive %s: %w", job.id, err)
	}
	w.logger.Info("room archive ready", "archive_id", job.id)
	return nil
}

func (w *Worker) claim(ctx context.Context) (claimedJob, error) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return claimedJob{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var job claimedJob
	err = tx.QueryRow(ctx, `
		SELECT a.id, a.room_id, a.source_revision, a.object_key
		FROM room_archives a
		JOIN rooms r ON r.id = a.room_id
		WHERE r.status = 'active' AND r.expires_at > now()
		  AND a.attempts < $1
		  AND (a.status = 'pending' OR (a.status = 'processing' AND a.updated_at < now() - interval '15 minutes'))
		ORDER BY a.created_at
		FOR UPDATE OF a SKIP LOCKED
		LIMIT 1
	`, maxArchiveAttempts).Scan(&job.id, &job.roomID, &job.sourceRevision, &job.objectKey)
	if err != nil {
		return claimedJob{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE room_archives
		SET status = 'processing', attempts = attempts + 1, processed_files = 0,
		    processed_bytes = 0, size_bytes = NULL, error = NULL, updated_at = now()
		WHERE id = $1
	`, job.id)
	if err != nil {
		return claimedJob{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return claimedJob{}, err
	}
	return job, nil
}

func (w *Worker) build(ctx context.Context, job claimedJob) error {
	var currentRevision int64
	if err := w.db.QueryRow(ctx, `SELECT media_revision FROM rooms WHERE id = $1`, job.roomID).Scan(&currentRevision); err != nil {
		return err
	}
	if currentRevision != job.sourceRevision {
		return errors.New("gallery changed while archive was queued")
	}

	rows, err := w.db.Query(ctx, `
		SELECT storage_key, original_filename, size_bytes
		FROM media
		WHERE room_id = $1 AND status = 'ready'
		ORDER BY created_at, id
	`, job.roomID)
	if err != nil {
		return err
	}
	items := make([]archiveItem, 0)
	for rows.Next() {
		var item archiveItem
		if err := rows.Scan(&item.storageKey, &item.filename, &item.sizeBytes); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(items) == 0 {
		return ErrEmpty
	}

	reader, writer := io.Pipe()
	zipResult := make(chan error, 1)
	go func() {
		err := w.writeZIP(ctx, job.id, writer, items)
		if err != nil {
			_ = writer.CloseWithError(err)
		} else {
			_ = writer.Close()
		}
		zipResult <- err
	}()

	sizeBytes, uploadErr := w.store.PutReader(ctx, job.objectKey, reader, "application/zip")
	if uploadErr != nil {
		_ = reader.CloseWithError(uploadErr)
	} else {
		_ = reader.Close()
	}
	zipErr := <-zipResult
	if zipErr != nil {
		return zipErr
	}
	if uploadErr != nil {
		return uploadErr
	}

	result, err := w.db.Exec(ctx, `
		UPDATE room_archives
		SET status = 'ready', processed_files = total_files, processed_bytes = total_bytes,
		    size_bytes = $2, error = NULL, updated_at = now(), completed_at = now()
		WHERE id = $1 AND status = 'processing'
	`, job.id, sizeBytes)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("archive job disappeared while completing")
	}
	return nil
}

func (w *Worker) writeZIP(ctx context.Context, jobID uuid.UUID, output io.Writer, items []archiveItem) error {
	archive := zip.NewWriter(output)
	names := make(map[string]int)
	var processedBytes int64
	for _, item := range items {
		name := uniqueFilename(item.filename, names)
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetModTime(time.Now())
		entry, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := w.store.Open(ctx, item.storageKey)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(entry, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		processedBytes += item.sizeBytes
		if _, err := w.db.Exec(ctx, `
			UPDATE room_archives
			SET processed_files = processed_files + 1, processed_bytes = $2, updated_at = now()
			WHERE id = $1 AND status = 'processing'
		`, jobID, processedBytes); err != nil {
			return err
		}
	}
	return archive.Close()
}

func (w *Worker) markAttemptFailed(ctx context.Context, jobID uuid.UUID, cause error) error {
	message := cause.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := w.db.Exec(ctx, `
		UPDATE room_archives
		SET status = CASE WHEN attempts >= $2 THEN 'failed' ELSE 'pending' END,
		    error = $3, updated_at = now()
		WHERE id = $1
	`, jobID, maxArchiveAttempts, message)
	return err
}

func uniqueFilename(original string, used map[string]int) string {
	name := strings.TrimSpace(filepath.Base(strings.ReplaceAll(original, "\\", "/")))
	name = strings.Map(func(character rune) rune {
		if character < 32 || character == '/' || character == '\\' {
			return '_'
		}
		return character
	}, name)
	if name == "" || name == "." {
		name = "media"
	}
	key := strings.ToLower(name)
	used[key]++
	if used[key] == 1 {
		return name
	}
	extension := filepath.Ext(name)
	base := strings.TrimSuffix(name, extension)
	return fmt.Sprintf("%s (%d)%s", base, used[key], extension)
}
