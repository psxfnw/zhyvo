package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrLinkChallengeUnavailable = errors.New("browser link challenge unavailable")

type BrowserLinkChallenge struct {
	Token     string    `json:"token,omitempty"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Service) CreateBrowserLinkChallenge(ctx context.Context, sourceID uuid.UUID) (BrowserLinkChallenge, error) {
	var kind string
	if err := s.db.QueryRow(ctx, `SELECT kind FROM identities WHERE id = $1`, sourceID).Scan(&kind); err != nil || kind != "anonymous" {
		return BrowserLinkChallenge{}, ErrIdentityLinkDenied
	}
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return BrowserLinkChallenge{}, fmt.Errorf("generate link challenge: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	hash := sha256.Sum256([]byte(token))
	now := s.now().UTC()
	expiresAt := now.Add(5 * time.Minute)
	_, err := s.db.Exec(ctx, `
		UPDATE browser_link_challenges SET status = 'denied'
		WHERE source_identity_id = $1 AND status IN ('pending', 'approved')
	`, sourceID)
	if err == nil {
		_, err = s.db.Exec(ctx, `
			INSERT INTO browser_link_challenges (token_hash, source_identity_id, expires_at, created_at)
			VALUES ($1, $2, $3, $4)
		`, hash[:], sourceID, expiresAt, now)
	}
	if err != nil {
		return BrowserLinkChallenge{}, fmt.Errorf("create browser link challenge: %w", err)
	}
	return BrowserLinkChallenge{Token: token, Status: "pending", ExpiresAt: expiresAt}, nil
}

func (s *Service) BrowserLinkStatus(ctx context.Context, sourceID uuid.UUID, token string) (BrowserLinkChallenge, error) {
	hash := sha256.Sum256([]byte(token))
	var result BrowserLinkChallenge
	err := s.db.QueryRow(ctx, `
		SELECT status, expires_at FROM browser_link_challenges
		WHERE token_hash = $1 AND source_identity_id = $2
	`, hash[:], sourceID).Scan(&result.Status, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return BrowserLinkChallenge{}, ErrLinkChallengeUnavailable
	}
	if err != nil {
		return BrowserLinkChallenge{}, err
	}
	if !result.ExpiresAt.After(s.now().UTC()) {
		result.Status = "expired"
	}
	return result, nil
}

func (s *Service) ApproveBrowserLink(ctx context.Context, telegramIdentityID uuid.UUID, token string) error {
	hash := sha256.Sum256([]byte(token))
	result, err := s.db.Exec(ctx, `
		UPDATE browser_link_challenges challenge
		SET status = 'approved', approved_telegram_user_id = identity.telegram_user_id,
		    approved_display_name = identity.display_name, approved_at = now()
		FROM identities identity
		WHERE challenge.token_hash = $1 AND challenge.status = 'pending'
		  AND challenge.expires_at > now() AND identity.id = $2
		  AND identity.kind = 'telegram' AND identity.telegram_user_id IS NOT NULL
	`, hash[:], telegramIdentityID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var alreadyHandled bool
	err = s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM browser_link_challenges challenge
			JOIN identities identity ON identity.telegram_user_id = challenge.approved_telegram_user_id
			WHERE challenge.token_hash = $1 AND identity.id = $2
			  AND challenge.status IN ('approved', 'consumed')
		)
	`, hash[:], telegramIdentityID).Scan(&alreadyHandled)
	if err == nil && alreadyHandled {
		return nil
	}
	return ErrLinkChallengeUnavailable
}

func (s *Service) ExchangeBrowserLink(ctx context.Context, sourceID uuid.UUID, token string) (SessionTokens, error) {
	hash := sha256.Sum256([]byte(token))
	var user TelegramUser
	err := s.db.QueryRow(ctx, `
		UPDATE browser_link_challenges SET status = 'consumed', consumed_at = now()
		WHERE token_hash = $1 AND source_identity_id = $2 AND status = 'approved' AND expires_at > now()
		RETURNING approved_telegram_user_id, approved_display_name
	`, hash[:], sourceID).Scan(&user.ID, &user.FirstName)
	if errors.Is(err, pgx.ErrNoRows) {
		return SessionTokens{}, ErrLinkChallengeUnavailable
	}
	if err != nil {
		return SessionTokens{}, err
	}
	return s.LinkTelegram(ctx, sourceID, user)
}
