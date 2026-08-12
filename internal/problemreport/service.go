package problemreport

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidInput = errors.New("invalid problem report")
	ErrNotFound     = errors.New("problem report not found")
)

type Service struct {
	db       *pgxpool.Pool
	adminIDs []int64
}

type TechnicalContext struct {
	Route      string     `json:"route,omitempty"`
	AppBuild   string     `json:"app_build,omitempty"`
	Platform   string     `json:"platform,omitempty"`
	Browser    string     `json:"browser,omitempty"`
	Telegram   *bool      `json:"telegram,omitempty"`
	Online     *bool      `json:"online,omitempty"`
	ErrorCode  string     `json:"error_code,omitempty"`
	RequestID  string     `json:"request_id,omitempty"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

type CreateInput struct {
	Category         string
	Description      string
	Contact          string
	TechnicalContext TechnicalContext
}

type Report struct {
	ID               uuid.UUID        `json:"id"`
	PublicID         string           `json:"public_id"`
	Category         string           `json:"category"`
	Description      string           `json:"description"`
	Contact          *string          `json:"contact,omitempty"`
	TechnicalContext TechnicalContext `json:"technical_context"`
	Status           string           `json:"status"`
	AdminNote        *string          `json:"admin_note,omitempty"`
	ReporterName     *string          `json:"reporter_name,omitempty"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	ResolvedAt       *time.Time       `json:"resolved_at,omitempty"`
}

type Stats struct {
	ActiveRooms         int   `json:"active_rooms"`
	ReadyMedia          int   `json:"ready_media"`
	StoredBytes         int64 `json:"stored_bytes"`
	ReservedBytes       int64 `json:"reserved_bytes"`
	TotalUsers          int   `json:"total_users"`
	NewReports          int   `json:"new_reports"`
	ReportsToday        int   `json:"reports_today"`
	UploadsToday        int   `json:"uploads_today"`
	UploadFailuresToday int   `json:"upload_failures_today"`
	UploadsInProgress   int   `json:"uploads_in_progress"`
	ThumbnailFailures   int   `json:"thumbnail_failures"`
	NewUsersToday       int   `json:"new_users_today"`
}

func New(db *pgxpool.Pool, adminIDs []int64) *Service {
	return &Service{db: db, adminIDs: append([]int64(nil), adminIDs...)}
}

func (s *Service) Create(ctx context.Context, reporterID *uuid.UUID, input CreateInput) (Report, error) {
	input.Category = strings.TrimSpace(strings.ToLower(input.Category))
	input.Description = strings.TrimSpace(input.Description)
	input.Contact = strings.TrimSpace(input.Contact)
	if !validCategory(input.Category) || utf8.RuneCountInString(input.Description) < 10 || utf8.RuneCountInString(input.Description) > 2000 || utf8.RuneCountInString(input.Contact) > 160 {
		return Report{}, ErrInvalidInput
	}
	input.TechnicalContext = sanitizeContext(input.TechnicalContext)
	contextJSON, err := json.Marshal(input.TechnicalContext)
	if err != nil {
		return Report{}, err
	}
	publicID, err := newPublicID()
	if err != nil {
		return Report{}, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result Report
	var rawContext []byte
	err = tx.QueryRow(ctx, `
		INSERT INTO problem_reports (public_id, reporter_identity_id, category, description, contact, technical_context)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		RETURNING id, public_id, category, description, contact, technical_context, status, admin_note, created_at, updated_at, resolved_at
	`, publicID, reporterID, input.Category, input.Description, input.Contact, contextJSON).Scan(
		&result.ID, &result.PublicID, &result.Category, &result.Description, &result.Contact, &rawContext,
		&result.Status, &result.AdminNote, &result.CreatedAt, &result.UpdatedAt, &result.ResolvedAt,
	)
	if err != nil {
		return Report{}, fmt.Errorf("create problem report: %w", err)
	}
	if err := json.Unmarshal(rawContext, &result.TechnicalContext); err != nil {
		return Report{}, err
	}
	for _, telegramID := range s.adminIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO admin_notification_outbox (problem_report_id, telegram_user_id, event_type, payload)
			VALUES ($1, $2, 'problem_report_created', jsonb_build_object(
				'report_id', $6::text, 'public_id', $3::text, 'category', $4::text,
				'description', left($5::text, 500)
			)) ON CONFLICT DO NOTHING
		`, result.ID, telegramID, result.PublicID, result.Category, result.Description, result.ID.String())
		if err != nil {
			return Report{}, fmt.Errorf("enqueue admin notification: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Report{}, err
	}
	return result, nil
}

func (s *Service) IsAdmin(ctx context.Context, identityID uuid.UUID) (bool, error) {
	if len(s.adminIDs) == 0 {
		return false, nil
	}
	var telegramID *int64
	if err := s.db.QueryRow(ctx, `SELECT telegram_user_id FROM identities WHERE id = $1`, identityID).Scan(&telegramID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if telegramID == nil {
		return false, nil
	}
	for _, allowed := range s.adminIDs {
		if *telegramID == allowed {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) List(ctx context.Context, status, category string) ([]Report, error) {
	if status != "" && !validStatus(status) || category != "" && !validCategory(category) {
		return nil, ErrInvalidInput
	}
	rows, err := s.db.Query(ctx, `
		SELECT report.id, report.public_id, report.category, report.description, report.contact,
		       report.technical_context, report.status, report.admin_note, identity.display_name,
		       report.created_at, report.updated_at, report.resolved_at
		FROM problem_reports report
		LEFT JOIN identities identity ON identity.id = report.reporter_identity_id
		WHERE ($1 = '' OR report.status = $1) AND ($2 = '' OR report.category = $2)
		ORDER BY CASE report.status WHEN 'new' THEN 0 WHEN 'in_progress' THEN 1 ELSE 2 END, report.created_at DESC
		LIMIT 100
	`, status, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Report, 0)
	for rows.Next() {
		report, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, report)
	}
	return result, rows.Err()
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Report, error) {
	row := s.db.QueryRow(ctx, `
		SELECT report.id, report.public_id, report.category, report.description, report.contact,
		       report.technical_context, report.status, report.admin_note, identity.display_name,
		       report.created_at, report.updated_at, report.resolved_at
		FROM problem_reports report
		LEFT JOIN identities identity ON identity.id = report.reporter_identity_id
		WHERE report.id = $1
	`, id)
	result, err := scanReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	return result, err
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, status, note string) (Report, error) {
	status = strings.TrimSpace(status)
	note = strings.TrimSpace(note)
	if !validStatus(status) || utf8.RuneCountInString(note) > 2000 {
		return Report{}, ErrInvalidInput
	}
	row := s.db.QueryRow(ctx, `
		UPDATE problem_reports SET status = $2::varchar, admin_note = NULLIF($3::text, ''), updated_at = now(),
		resolved_at = CASE WHEN $2::text IN ('resolved', 'closed') THEN COALESCE(resolved_at, now()) ELSE NULL END
		WHERE id = $1
		RETURNING id, public_id, category, description, contact, technical_context, status, admin_note,
		          NULL::text AS reporter_name, created_at, updated_at, resolved_at
	`, id, status, note)
	result, err := scanReport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	return result, err
}

func (s *Service) Stats(ctx context.Context) (Stats, error) {
	var result Stats
	err := s.db.QueryRow(ctx, `
		SELECT
			(SELECT count(*)::int FROM rooms WHERE status = 'active' AND expires_at > now()),
			(SELECT count(*)::int FROM media WHERE status = 'ready'),
			(SELECT COALESCE(sum(size_bytes), 0)::bigint FROM media WHERE status = 'ready'),
			(SELECT COALESCE(sum(reserved_storage_bytes), 0)::bigint FROM rooms WHERE status = 'active' AND expires_at > now()),
			(SELECT count(*)::int FROM identities),
			(SELECT count(*)::int FROM problem_reports WHERE status = 'new'),
			(SELECT count(*)::int FROM problem_reports WHERE created_at >= date_trunc('day', now())),
			(SELECT count(*)::int FROM media WHERE status = 'ready' AND created_at >= date_trunc('day', now())),
			(SELECT count(*)::int FROM media WHERE status = 'failed' AND created_at >= date_trunc('day', now())),
			(SELECT count(*)::int FROM upload_sessions WHERE status IN ('initiated', 'uploading') AND expires_at > now()),
			(SELECT count(*)::int FROM media WHERE status = 'ready' AND thumbnail_status = 'failed'),
			(SELECT count(*)::int FROM identities WHERE created_at >= date_trunc('day', now()))
	`).Scan(&result.ActiveRooms, &result.ReadyMedia, &result.StoredBytes, &result.ReservedBytes, &result.TotalUsers, &result.NewReports, &result.ReportsToday, &result.UploadsToday, &result.UploadFailuresToday, &result.UploadsInProgress, &result.ThumbnailFailures, &result.NewUsersToday)
	return result, err
}

type rowScanner interface{ Scan(...any) error }

func scanReport(row rowScanner) (Report, error) {
	var result Report
	var rawContext []byte
	err := row.Scan(&result.ID, &result.PublicID, &result.Category, &result.Description, &result.Contact,
		&rawContext, &result.Status, &result.AdminNote, &result.ReporterName,
		&result.CreatedAt, &result.UpdatedAt, &result.ResolvedAt)
	if err != nil {
		return Report{}, err
	}
	if err := json.Unmarshal(rawContext, &result.TechnicalContext); err != nil {
		return Report{}, err
	}
	return result, nil
}

func validCategory(value string) bool {
	return value == "upload" || value == "download" || value == "room" || value == "telegram" || value == "other"
}

func validStatus(value string) bool {
	return value == "new" || value == "in_progress" || value == "resolved" || value == "closed"
}

func sanitizeContext(value TechnicalContext) TechnicalContext {
	value.Route = sanitizeRoute(value.Route)
	value.AppBuild = truncate(value.AppBuild, 80)
	value.Platform = truncate(value.Platform, 120)
	value.Browser = truncate(value.Browser, 500)
	value.ErrorCode = truncate(value.ErrorCode, 80)
	value.RequestID = truncate(value.RequestID, 100)
	return value
}

func sanitizeRoute(value string) string {
	value = strings.TrimSpace(strings.SplitN(value, "?", 2)[0])
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) > 1 && parts[0] == "i" {
		return "/i/:invite"
	}
	if len(parts) > 1 && parts[0] == "r" {
		if len(parts) > 2 && parts[2] == "recap" {
			return "/r/:room/recap"
		}
		return "/r/:room"
	}
	if !strings.HasPrefix(value, "/") {
		return ""
	}
	return truncate(value, 160)
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

func newPublicID() (string, error) {
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "ZHY-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}
