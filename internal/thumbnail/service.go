package thumbnail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
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
	processTimeout = 2 * time.Minute
	maxHEIFBytes   = 55 * 1024 * 1024
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
	mimeType   string
	storageKey string
}

type mediaProbe struct {
	Width      int
	Height     int
	DurationMS int64
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
		SELECT m.id, m.room_id, m.media_type, m.mime_type, m.storage_key
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
	`, maxAttempts).Scan(&claimed.mediaID, &claimed.roomID, &claimed.mediaType, &claimed.mimeType, &claimed.storageKey)
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
	ctx, cancel := context.WithTimeout(ctx, processTimeout)
	defer cancel()
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

	probe, probeErr := runFFprobe(ctx, sourceURL)
	if probeErr != nil {
		s.logger.Warn("media metadata probe failed", "media_id", claimed.mediaID, "error", sanitizeCommandError(probeErr, sourceURL))
	}
	if err := runFFmpeg(ctx, claimed.mediaType, sourceURL, outputPath, probe.DurationMS); err != nil {
		if !isHEIF(claimed.mimeType) {
			return sanitizeCommandError(err, sourceURL)
		}
		fallbackProbe, fallbackErr := runHEIFConvert(ctx, sourceURL, outputPath)
		if fallbackErr != nil {
			return fmt.Errorf("HEIF thumbnail failed: ffmpeg: %v; libheif: %v", sanitizeCommandError(err, sourceURL), sanitizeCommandError(fallbackErr, sourceURL))
		}
		if probe.Width == 0 || probe.Height == 0 {
			probe.Width = fallbackProbe.Width
			probe.Height = fallbackProbe.Height
		}
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
		    thumbnail_updated_at = now(),
		    width = COALESCE(NULLIF($3, 0), width),
		    height = COALESCE(NULLIF($4, 0), height),
		    duration_ms = CASE WHEN $5 > 0 THEN $5 ELSE duration_ms END
		WHERE id = $2 AND status = 'ready'
	`, thumbnailKey, claimed.mediaID, probe.Width, probe.Height, probe.DurationMS)
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

func runFFmpeg(ctx context.Context, mediaType, sourceURL, outputPath string, durationMS int64) error {
	base := []string{"-hide_banner", "-loglevel", "error", "-y"}
	if mediaType == "video" {
		base = append(base, "-ss", thumbnailSeek(durationMS))
	}
	arguments := appendFFmpegInput(base, sourceURL)
	arguments = append(arguments,
		"-vf", "scale=480:-2:force_original_aspect_ratio=decrease:flags=lanczos,format=yuvj420p",
		"-frames:v", "1",
		"-q:v", "4", "-map_metadata", "-1",
		outputPath,
	)
	output, err := exec.CommandContext(ctx, "ffmpeg", arguments...).CombinedOutput()
	if err != nil && mediaType == "video" {
		fallback := []string{
			"-hide_banner", "-loglevel", "error", "-y",
		}
		fallback = appendFFmpegInput(fallback, sourceURL)
		fallback = append(fallback,
			"-vf", "scale=480:-2:force_original_aspect_ratio=decrease:flags=lanczos,format=yuvj420p",
			"-frames:v", "1", "-q:v", "4", "-map_metadata", "-1", outputPath,
		)
		output, err = exec.CommandContext(ctx, "ffmpeg", fallback...).CombinedOutput()
	}
	if err != nil {
		return fmt.Errorf("ffmpeg: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func appendFFmpegInput(arguments []string, sourceURL string) []string {
	// FFmpeg 6 treats autorotate as a boolean option with an explicit value.
	return append(arguments, "-autorotate", "1", "-i", sourceURL)
}

func thumbnailSeek(durationMS int64) string {
	if durationMS <= 0 {
		return "1"
	}
	seconds := math.Min(3, math.Max(0.1, float64(durationMS)/1000*0.1))
	return strconv.FormatFloat(seconds, 'f', 2, 64)
}

func isHEIF(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return mimeType == "image/heic" || mimeType == "image/heif"
}

func runHEIFConvert(ctx context.Context, sourceURL, outputPath string) (mediaProbe, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return mediaProbe{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return mediaProbe{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return mediaProbe{}, fmt.Errorf("download HEIF source returned status %d", response.StatusCode)
	}
	input, err := os.CreateTemp("", "photodrop-source-*.heic")
	if err != nil {
		return mediaProbe{}, err
	}
	inputPath := input.Name()
	defer os.Remove(inputPath)
	written, copyErr := io.Copy(input, io.LimitReader(response.Body, maxHEIFBytes+1))
	closeErr := input.Close()
	if copyErr != nil {
		return mediaProbe{}, copyErr
	}
	if closeErr != nil {
		return mediaProbe{}, closeErr
	}
	if written > maxHEIFBytes {
		return mediaProbe{}, errors.New("HEIF source exceeds processing limit")
	}
	output, err := exec.CommandContext(ctx, "heif-convert", "-q", "85", inputPath, outputPath).CombinedOutput()
	if err != nil {
		return mediaProbe{}, fmt.Errorf("heif-convert: %s", strings.TrimSpace(string(output)))
	}
	converted, err := os.Open(outputPath)
	if err != nil {
		return mediaProbe{}, err
	}
	defer converted.Close()
	configuration, err := jpeg.DecodeConfig(converted)
	if err != nil {
		return mediaProbe{}, fmt.Errorf("read converted HEIF dimensions: %w", err)
	}
	return mediaProbe{Width: configuration.Width, Height: configuration.Height}, nil
}

func runFFprobe(ctx context.Context, sourceURL string) (mediaProbe, error) {
	output, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,duration:stream_tags=rotate:stream_side_data=rotation:format=duration",
		"-of", "json", sourceURL,
	).CombinedOutput()
	if err != nil {
		return mediaProbe{}, fmt.Errorf("ffprobe: %s", strings.TrimSpace(string(output)))
	}
	return parseFFprobe(output)
}

func parseFFprobe(raw []byte) (mediaProbe, error) {
	var result struct {
		Streams []struct {
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration string `json:"duration"`
			Tags     struct {
				Rotate string `json:"rotate"`
			} `json:"tags"`
			SideData []struct {
				Rotation float64 `json:"rotation"`
			} `json:"side_data_list"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return mediaProbe{}, err
	}
	if len(result.Streams) == 0 {
		return mediaProbe{}, errors.New("no video stream found")
	}
	stream := result.Streams[0]
	rotation, _ := strconv.ParseFloat(stream.Tags.Rotate, 64)
	if len(stream.SideData) > 0 {
		rotation = stream.SideData[0].Rotation
	}
	width, height := stream.Width, stream.Height
	normalizedRotation := int(math.Abs(rotation)) % 180
	if normalizedRotation == 90 {
		width, height = height, width
	}
	duration := stream.Duration
	if duration == "" || duration == "N/A" {
		duration = result.Format.Duration
	}
	durationSeconds, _ := strconv.ParseFloat(duration, 64)
	return mediaProbe{Width: width, Height: height, DurationMS: int64(math.Round(durationSeconds * 1000))}, nil
}

func sanitizeCommandError(err error, sourceURL string) error {
	if err == nil {
		return nil
	}
	return errors.New(strings.ReplaceAll(err.Error(), sourceURL, "[media source]"))
}
