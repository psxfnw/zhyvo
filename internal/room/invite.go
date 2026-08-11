package room

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Invite struct {
	Token      string     `json:"token"`
	Permission string     `json:"permission"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	JoinCount  int        `json:"join_count"`
}

type InviteList struct {
	Invites              []Invite `json:"invites"`
	LegacyInvitesEnabled bool     `json:"legacy_invites_enabled"`
}

type InvitePreview struct {
	Preview
	Permission string `json:"permission"`
}

func (s *Service) CreateInvite(ctx context.Context, ownerID uuid.UUID, slug, permission string) (Invite, error) {
	if permission != "contributor" && permission != "viewer" {
		return Invite{}, fmt.Errorf("%w: permission must be contributor or viewer", ErrInvalidInput)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Invite{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	roomID, err := s.lockOwnedRoom(ctx, tx, ownerID, slug)
	if err != nil {
		return Invite{}, err
	}
	var activeCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM room_invites WHERE room_id = $1 AND revoked_at IS NULL`, roomID).Scan(&activeCount); err != nil {
		return Invite{}, fmt.Errorf("count room invitations: %w", err)
	}
	if activeCount >= 20 {
		return Invite{}, fmt.Errorf("%w: a room can have at most 20 active invitations", ErrInvalidInput)
	}
	token, err := newInviteToken()
	if err != nil {
		return Invite{}, err
	}
	var result Invite
	err = tx.QueryRow(ctx, `
		INSERT INTO room_invites (token, room_id, permission, created_by_identity_id)
		VALUES ($1, $2, $3, $4)
		RETURNING token, permission, created_at, revoked_at, last_used_at, join_count
	`, token, roomID, permission, ownerID).Scan(&result.Token, &result.Permission, &result.CreatedAt, &result.RevokedAt, &result.LastUsedAt, &result.JoinCount)
	if err != nil {
		return Invite{}, fmt.Errorf("create room invitation: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Invite{}, err
	}
	return result, nil
}

func (s *Service) Invites(ctx context.Context, ownerID uuid.UUID, slug string) (InviteList, error) {
	currentRoom, err := s.Get(ctx, ownerID, slug)
	if err != nil {
		return InviteList{}, err
	}
	if currentRoom.Role != "owner" {
		return InviteList{}, ErrOwnerRequired
	}
	var legacyEnabled bool
	if err := s.db.QueryRow(ctx, `SELECT legacy_invites_enabled FROM rooms WHERE id = $1`, currentRoom.ID).Scan(&legacyEnabled); err != nil {
		return InviteList{}, err
	}
	rows, err := s.db.Query(ctx, `
		SELECT token, permission, created_at, revoked_at, last_used_at, join_count
		FROM room_invites WHERE room_id = $1
		ORDER BY revoked_at NULLS FIRST, created_at DESC
		LIMIT 50
	`, currentRoom.ID)
	if err != nil {
		return InviteList{}, fmt.Errorf("list room invitations: %w", err)
	}
	defer rows.Close()
	invites := make([]Invite, 0)
	for rows.Next() {
		var invite Invite
		if err := rows.Scan(&invite.Token, &invite.Permission, &invite.CreatedAt, &invite.RevokedAt, &invite.LastUsedAt, &invite.JoinCount); err != nil {
			return InviteList{}, err
		}
		invites = append(invites, invite)
	}
	return InviteList{Invites: invites, LegacyInvitesEnabled: legacyEnabled}, rows.Err()
}

// ShareInvite returns the current contributor invitation to any active room
// member without exposing the owner's invitation-management controls.
func (s *Service) ShareInvite(ctx context.Context, identityID uuid.UUID, slug string) (Invite, error) {
	currentRoom, err := s.Get(ctx, identityID, slug)
	if err != nil {
		return Invite{}, err
	}
	var result Invite
	err = s.db.QueryRow(ctx, `
		SELECT token, permission, created_at, revoked_at, last_used_at, join_count
		FROM room_invites
		WHERE room_id = $1 AND permission = 'contributor' AND revoked_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, currentRoom.ID).Scan(&result.Token, &result.Permission, &result.CreatedAt, &result.RevokedAt, &result.LastUsedAt, &result.JoinCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invite{}, ErrInviteNotFound
	}
	if err != nil {
		return Invite{}, fmt.Errorf("get share invitation: %w", err)
	}
	return result, nil
}

func (s *Service) RevokeInvite(ctx context.Context, ownerID uuid.UUID, slug, token string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	roomID, err := s.lockOwnedRoom(ctx, tx, ownerID, slug)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE room_invites SET revoked_at = now() WHERE room_id = $1 AND token = $2 AND revoked_at IS NULL`, roomID, token)
	if err != nil {
		return fmt.Errorf("revoke room invitation: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrInviteNotFound
	}
	return tx.Commit(ctx)
}

func (s *Service) DisableLegacyInvites(ctx context.Context, ownerID uuid.UUID, slug string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	roomID, err := s.lockOwnedRoom(ctx, tx, ownerID, slug)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE rooms SET legacy_invites_enabled = false WHERE id = $1`, roomID); err != nil {
		return fmt.Errorf("disable legacy room invitations: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *Service) InvitePreview(ctx context.Context, token string) (InvitePreview, error) {
	var result InvitePreview
	err := s.db.QueryRow(ctx, `
		SELECT room.slug, room.name, room.access_mode, room.status, room.accepting_members, room.expires_at, invite.permission
		FROM room_invites invite
		JOIN rooms room ON room.id = invite.room_id
		WHERE invite.token = $1 AND invite.revoked_at IS NULL
	`, token).Scan(&result.Slug, &result.Name, &result.AccessMode, &result.Status, &result.AcceptingMembers, &result.ExpiresAt, &result.Permission)
	if errors.Is(err, pgx.ErrNoRows) {
		return InvitePreview{}, ErrInviteNotFound
	}
	if err != nil {
		return InvitePreview{}, fmt.Errorf("get invitation preview: %w", err)
	}
	if result.Status != "active" || !result.ExpiresAt.After(s.now()) {
		return InvitePreview{}, ErrExpired
	}
	return result, nil
}

func (s *Service) JoinInvite(ctx context.Context, identityID uuid.UUID, token, secret string) (Room, error) {
	preview, err := s.InvitePreview(ctx, token)
	if err != nil {
		return Room{}, err
	}
	return s.join(ctx, identityID, preview.Slug, secret, token)
}

func newInviteToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate room invitation: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
