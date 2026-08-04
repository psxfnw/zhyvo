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
