package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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
	ErrInvalidInput        = errors.New("invalid input")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrIdentityLinkDenied  = errors.New("identity cannot be linked")
)

type Service struct {
	db         *pgxpool.Pool
	tokens     *TokenManager
	refreshTTL time.Duration
	now        func() time.Time
}

type Identity struct {
	ID          uuid.UUID `json:"id"`
	Kind        string    `json:"kind"`
	DisplayName string    `json:"display_name"`
}

type SessionTokens struct {
	Identity          Identity
	AccessToken       string
	AccessTokenExpiry time.Time
	RefreshToken      string
	RefreshExpiry     time.Time
}

func NewService(db *pgxpool.Pool, tokens *TokenManager, refreshTTL time.Duration) *Service {
	return &Service{db: db, tokens: tokens, refreshTTL: refreshTTL, now: time.Now}
}

func (s *Service) CreateAnonymous(ctx context.Context, displayName, clientType string) (SessionTokens, error) {
	displayName = strings.TrimSpace(displayName)
	if count := utf8.RuneCountInString(displayName); count < 1 || count > 80 {
		return SessionTokens{}, fmt.Errorf("%w: display_name must contain 1 to 80 characters", ErrInvalidInput)
	}
	if clientType != "web" && clientType != "ios" && clientType != "android" {
		return SessionTokens{}, fmt.Errorf("%w: unsupported client_type", ErrInvalidInput)
	}

	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return SessionTokens{}, err
	}
	now := s.now().UTC()
	refreshExpiry := now.Add(s.refreshTTL)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SessionTokens{}, fmt.Errorf("begin auth transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	identity := Identity{Kind: "anonymous", DisplayName: displayName}
	if err := tx.QueryRow(ctx, `
		INSERT INTO identities (kind, display_name, created_at, last_seen_at)
		VALUES ('anonymous', $1, $2, $2)
		RETURNING id
	`, displayName, now).Scan(&identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("create identity: %w", err)
	}

	var sessionID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO sessions (identity_id, token_hash, client_type, expires_at, created_at, last_used_at)
		VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING id
	`, identity.ID, refreshHash[:], clientType, refreshExpiry, now).Scan(&sessionID); err != nil {
		return SessionTokens{}, fmt.Errorf("create session: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return SessionTokens{}, fmt.Errorf("commit auth transaction: %w", err)
	}

	return s.issue(identity, sessionID, refreshToken, refreshExpiry)
}

func (s *Service) CreateTelegram(ctx context.Context, user TelegramUser) (SessionTokens, error) {
	if user.ID <= 0 {
		return SessionTokens{}, fmt.Errorf("%w: invalid Telegram user", ErrInvalidInput)
	}
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return SessionTokens{}, err
	}
	now := s.now().UTC()
	refreshExpiry := now.Add(s.refreshTTL)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SessionTokens{}, fmt.Errorf("begin Telegram auth transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	identity := Identity{Kind: "telegram", DisplayName: user.DisplayName()}
	if err := tx.QueryRow(ctx, `
		INSERT INTO identities (kind, display_name, telegram_user_id, created_at, last_seen_at)
		VALUES ('telegram', $1, $2, $3, $3)
		ON CONFLICT (telegram_user_id) WHERE telegram_user_id IS NOT NULL
		DO UPDATE SET display_name = EXCLUDED.display_name, last_seen_at = EXCLUDED.last_seen_at
		RETURNING id, kind, display_name
	`, identity.DisplayName, user.ID, now).Scan(&identity.ID, &identity.Kind, &identity.DisplayName); err != nil {
		return SessionTokens{}, fmt.Errorf("upsert Telegram identity: %w", err)
	}

	var sessionID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO sessions (identity_id, token_hash, client_type, expires_at, created_at, last_used_at)
		VALUES ($1, $2, 'telegram', $3, $4, $4)
		RETURNING id
	`, identity.ID, refreshHash[:], refreshExpiry, now).Scan(&sessionID); err != nil {
		return SessionTokens{}, fmt.Errorf("create Telegram session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionTokens{}, fmt.Errorf("commit Telegram auth transaction: %w", err)
	}
	return s.issue(identity, sessionID, refreshToken, refreshExpiry)
}

// LinkTelegram upgrades an anonymous browser identity to a stable Telegram
// identity without losing room ownership, memberships or uploaded media.
func (s *Service) LinkTelegram(ctx context.Context, sourceID uuid.UUID, user TelegramUser) (SessionTokens, error) {
	if user.ID <= 0 {
		return SessionTokens{}, fmt.Errorf("%w: invalid Telegram user", ErrInvalidInput)
	}
	refreshToken, refreshHash, err := newRefreshToken()
	if err != nil {
		return SessionTokens{}, err
	}
	now := s.now().UTC()
	refreshExpiry := now.Add(s.refreshTTL)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SessionTokens{}, fmt.Errorf("begin identity link transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sourceKind string
	if err := tx.QueryRow(ctx, `SELECT kind FROM identities WHERE id = $1 FOR UPDATE`, sourceID).Scan(&sourceKind); err != nil {
		return SessionTokens{}, fmt.Errorf("find source identity: %w", err)
	}
	if sourceKind != "anonymous" {
		return SessionTokens{}, ErrIdentityLinkDenied
	}

	identity := Identity{Kind: "telegram", DisplayName: user.DisplayName()}
	if err := tx.QueryRow(ctx, `
		INSERT INTO identities (kind, display_name, telegram_user_id, created_at, last_seen_at)
		VALUES ('telegram', $1, $2, $3, $3)
		ON CONFLICT (telegram_user_id) WHERE telegram_user_id IS NOT NULL
		DO UPDATE SET display_name = EXCLUDED.display_name, last_seen_at = EXCLUDED.last_seen_at
		RETURNING id, kind, display_name
	`, identity.DisplayName, user.ID, now).Scan(&identity.ID, &identity.Kind, &identity.DisplayName); err != nil {
		return SessionTokens{}, fmt.Errorf("upsert linked Telegram identity: %w", err)
	}

	// A Telegram identity may already be a member of a room created in this
	// browser. Remove the duplicate membership before transferring ownership.
	if _, err := tx.Exec(ctx, `
		DELETE FROM room_members target
		USING rooms owned
		WHERE owned.owner_identity_id = $1
		  AND target.room_id = owned.id
		  AND target.identity_id = $2
	`, sourceID, identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("remove duplicate owner memberships: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM room_bans target
		USING rooms owned
		WHERE owned.owner_identity_id = $1
		  AND target.room_id = owned.id
		  AND target.identity_id = $2
	`, sourceID, identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("remove obsolete owner bans: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE room_members SET identity_id = $2 WHERE identity_id = $1 AND role = 'owner'`, sourceID, identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("transfer owner memberships: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE rooms SET owner_identity_id = $2 WHERE owner_identity_id = $1`, sourceID, identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("transfer owned rooms: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO room_members (room_id, identity_id, role, joined_at, last_seen_at)
		SELECT room_id, $2, role, joined_at, last_seen_at
		FROM room_members source
		WHERE identity_id = $1
		  AND NOT EXISTS (SELECT 1 FROM room_bans ban WHERE ban.room_id = source.room_id AND ban.identity_id = $2)
		ON CONFLICT (room_id, identity_id) DO UPDATE
		SET last_seen_at = GREATEST(room_members.last_seen_at, EXCLUDED.last_seen_at)
	`, sourceID, identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("merge room memberships: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM room_members WHERE identity_id = $1`, sourceID); err != nil {
		return SessionTokens{}, fmt.Errorf("remove merged memberships: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO room_bans (room_id, identity_id, banned_by_identity_id, created_at)
		SELECT source.room_id, $2, CASE WHEN source.banned_by_identity_id = $1 THEN $2 ELSE source.banned_by_identity_id END, source.created_at
		FROM room_bans source
		WHERE source.identity_id = $1
		  AND NOT EXISTS (SELECT 1 FROM room_members member WHERE member.room_id = source.room_id AND member.identity_id = $2)
		ON CONFLICT (room_id, identity_id) DO NOTHING
	`, sourceID, identity.ID); err != nil {
		return SessionTokens{}, fmt.Errorf("merge room bans: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM room_bans WHERE identity_id = $1`, sourceID); err != nil {
		return SessionTokens{}, fmt.Errorf("remove merged room bans: %w", err)
	}

	updates := []struct {
		name string
		sql  string
	}{
		{"media uploaders", `UPDATE media SET uploader_identity_id = $2 WHERE uploader_identity_id = $1`},
		{"upload sessions", `UPDATE upload_sessions SET identity_id = $2 WHERE identity_id = $1`},
		{"room events actors", `UPDATE room_events SET actor_identity_id = $2 WHERE actor_identity_id = $1`},
		{"room events subjects", `UPDATE room_events SET subject_identity_id = $2 WHERE subject_identity_id = $1`},
		{"room ban moderators", `UPDATE room_bans SET banned_by_identity_id = $2 WHERE banned_by_identity_id = $1`},
		{"room archives", `UPDATE room_archives SET requested_by_identity_id = $2 WHERE requested_by_identity_id = $1`},
	}
	for _, update := range updates {
		if _, err := tx.Exec(ctx, update.sql, sourceID, identity.ID); err != nil {
			return SessionTokens{}, fmt.Errorf("merge %s: %w", update.name, err)
		}
	}
	// Idempotency records are short-lived implementation details; deleting them
	// avoids rare composite-key conflicts when two devices generated the same key.
	if _, err := tx.Exec(ctx, `DELETE FROM room_creation_requests WHERE identity_id = $1`, sourceID); err != nil {
		return SessionTokens{}, fmt.Errorf("remove anonymous creation requests: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = $2 WHERE identity_id = $1 AND revoked_at IS NULL`, sourceID, now); err != nil {
		return SessionTokens{}, fmt.Errorf("revoke anonymous sessions: %w", err)
	}

	var sessionID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO sessions (identity_id, token_hash, client_type, expires_at, created_at, last_used_at)
		VALUES ($1, $2, 'web', $3, $4, $4)
		RETURNING id
	`, identity.ID, refreshHash[:], refreshExpiry, now).Scan(&sessionID); err != nil {
		return SessionTokens{}, fmt.Errorf("create linked Telegram session: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM identities WHERE id = $1`, sourceID); err != nil {
		return SessionTokens{}, fmt.Errorf("remove anonymous identity: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionTokens{}, fmt.Errorf("commit identity link transaction: %w", err)
	}
	return s.issue(identity, sessionID, refreshToken, refreshExpiry)
}

func (s *Service) Refresh(ctx context.Context, rawRefreshToken string) (SessionTokens, error) {
	if len(rawRefreshToken) < 32 || len(rawRefreshToken) > 256 {
		return SessionTokens{}, ErrInvalidRefreshToken
	}
	oldHash := sha256.Sum256([]byte(rawRefreshToken))
	newToken, newHash, err := newRefreshToken()
	if err != nil {
		return SessionTokens{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return SessionTokens{}, fmt.Errorf("begin refresh transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sessionID uuid.UUID
	var refreshExpiry time.Time
	identity := Identity{}
	err = tx.QueryRow(ctx, `
		SELECT s.id, s.expires_at, i.id, i.kind, i.display_name
		FROM sessions s
		JOIN identities i ON i.id = s.identity_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()
		FOR UPDATE OF s
	`, oldHash[:]).Scan(&sessionID, &refreshExpiry, &identity.ID, &identity.Kind, &identity.DisplayName)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionTokens{}, ErrInvalidRefreshToken
	}
	if err != nil {
		return SessionTokens{}, fmt.Errorf("find refresh session: %w", err)
	}

	result, err := tx.Exec(ctx, `
		UPDATE sessions
		SET token_hash = $1, last_used_at = $2
		WHERE id = $3 AND token_hash = $4
	`, newHash[:], s.now().UTC(), sessionID, oldHash[:])
	if err != nil {
		return SessionTokens{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	if result.RowsAffected() != 1 {
		return SessionTokens{}, ErrInvalidRefreshToken
	}
	if err := tx.Commit(ctx); err != nil {
		return SessionTokens{}, fmt.Errorf("commit refresh transaction: %w", err)
	}

	return s.issue(identity, sessionID, newToken, refreshExpiry)
}

func (s *Service) GetIdentity(ctx context.Context, identityID uuid.UUID) (Identity, error) {
	var identity Identity
	err := s.db.QueryRow(ctx, `
		SELECT id, kind, display_name
		FROM identities
		WHERE id = $1
	`, identityID).Scan(&identity.ID, &identity.Kind, &identity.DisplayName)
	return identity, err
}

func (s *Service) Revoke(ctx context.Context, sessionID, identityID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = now()
		WHERE id = $1 AND identity_id = $2 AND revoked_at IS NULL
	`, sessionID, identityID)
	return err
}

func (s *Service) SessionIsActive(ctx context.Context, principal Principal) (bool, error) {
	var active bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM sessions
			WHERE id = $1
			  AND identity_id = $2
			  AND revoked_at IS NULL
			  AND expires_at > now()
		)
	`, principal.SessionID, principal.IdentityID).Scan(&active)
	return active, err
}

func (s *Service) issue(identity Identity, sessionID uuid.UUID, refreshToken string, refreshExpiry time.Time) (SessionTokens, error) {
	accessToken, accessExpiry, err := s.tokens.Issue(Principal{
		IdentityID: identity.ID,
		SessionID:  sessionID,
		Kind:       identity.Kind,
	})
	if err != nil {
		return SessionTokens{}, err
	}
	return SessionTokens{
		Identity:          identity,
		AccessToken:       accessToken,
		AccessTokenExpiry: accessExpiry,
		RefreshToken:      refreshToken,
		RefreshExpiry:     refreshExpiry,
	}, nil
}

func newRefreshToken() (string, [32]byte, error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", [32]byte{}, fmt.Errorf("generate refresh token: %w", err)
	}
	raw := base64.RawURLEncoding.EncodeToString(buffer)
	return raw, sha256.Sum256([]byte(raw)), nil
}
