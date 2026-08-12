package room

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrInvalidInput        = errors.New("invalid room input")
	ErrNotFound            = errors.New("room not found")
	ErrExpired             = errors.New("room expired")
	ErrAccessDenied        = errors.New("room access denied")
	ErrNotMember           = errors.New("identity is not a room member")
	ErrOwnerRequired       = errors.New("room owner permission required")
	ErrMemberNotFound      = errors.New("room member not found")
	ErrMemberBlocked       = errors.New("identity is blocked from this room")
	ErrJoiningClosed       = errors.New("room is closed to new members")
	ErrInviteNotFound      = errors.New("room invitation not found")
	ErrCannotRemoveOwner   = errors.New("room owner cannot be removed")
	ErrMemberNotBlocked    = errors.New("room member is not blocked")
	ErrIdempotencyConflict = errors.New("idempotency key was already used with different input")
)

var pinPattern = regexp.MustCompile(`^[0-9]{4,8}$`)

type Service struct {
	db  *pgxpool.Pool
	now func() time.Time
}

type CreateInput struct {
	Name         string
	LifetimeDays int
	AccessMode   string
	Secret       string
}

type AccessUpdate struct {
	Mode   string
	Secret string
}

type UpdateInput struct {
	Name             *string
	AcceptingUploads *bool
	AcceptingMembers *bool
	Access           *AccessUpdate
	LifetimeDays     *int
}

type Room struct {
	ID               uuid.UUID `json:"id"`
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	AccessMode       string    `json:"access_mode"`
	Role             string    `json:"role"`
	CanUpload        bool      `json:"can_upload"`
	Status           string    `json:"status"`
	AcceptingUploads bool      `json:"accepting_uploads"`
	AcceptingMembers bool      `json:"accepting_members"`
	MaxFiles         int       `json:"max_files"`
	MaxStorageBytes  int64     `json:"max_storage_bytes"`
	UsedFiles        int       `json:"used_files"`
	UsedStorageBytes int64     `json:"used_storage_bytes"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type Preview struct {
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	AccessMode       string    `json:"access_mode"`
	Status           string    `json:"status"`
	AcceptingMembers bool      `json:"accepting_members"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type Member struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	Role        string    `json:"role"`
	CanUpload   bool      `json:"can_upload"`
	JoinedAt    time.Time `json:"joined_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
}

type BlockedMember struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"display_name"`
	BlockedAt   time.Time `json:"blocked_at"`
}

type NotificationSettings struct {
	TelegramAvailable   bool `json:"telegram_available"`
	TelegramEnabled     bool `json:"telegram_enabled"`
	NewMediaEnabled     bool `json:"new_media_enabled"`
	ExpiryEnabled       bool `json:"expiry_enabled"`
	MemberJoinedEnabled bool `json:"member_joined_enabled"`
	IsOwner             bool `json:"is_owner"`
}

type NotificationUpdate struct {
	TelegramEnabled     *bool
	NewMediaEnabled     *bool
	ExpiryEnabled       *bool
	MemberJoinedEnabled *bool
}

type MembersResult struct {
	Members []Member        `json:"members"`
	Blocked []BlockedMember `json:"blocked_members"`
}

type ActivityEvent struct {
	ID                 uuid.UUID  `json:"id"`
	Type               string     `json:"type"`
	ActorID            uuid.UUID  `json:"actor_id"`
	ActorDisplayName   string     `json:"actor_display_name"`
	SubjectID          *uuid.UUID `json:"subject_id,omitempty"`
	SubjectDisplayName *string    `json:"subject_display_name,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db, now: time.Now}
}

func (s *Service) Create(ctx context.Context, ownerID, idempotencyKey uuid.UUID, input CreateInput) (Room, error) {
	input.Name = strings.TrimSpace(input.Name)
	if err := validateCreate(input); err != nil {
		return Room{}, err
	}
	requestHash := hashCreateInput(input)

	var secretHash *string
	if input.AccessMode != "public" {
		hashed, err := hashSecret(input.Secret)
		if err != nil {
			return Room{}, err
		}
		secretHash = &hashed
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Room{}, fmt.Errorf("begin create room transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reservation, err := tx.Exec(ctx, `
		INSERT INTO room_creation_requests (identity_id, idempotency_key, request_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, ownerID, idempotencyKey, requestHash[:])
	if err != nil {
		return Room{}, fmt.Errorf("reserve idempotency key: %w", err)
	}
	if reservation.RowsAffected() == 0 {
		room, err := existingIdempotentRoom(ctx, tx, ownerID, idempotencyKey, requestHash)
		if err != nil {
			return Room{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Room{}, fmt.Errorf("commit idempotent room read: %w", err)
		}
		return room, nil
	}

	now := s.now().UTC()
	expiresAt := now.AddDate(0, 0, input.LifetimeDays)
	var created Room
	for attempt := 0; attempt < 8; attempt++ {
		slug, err := newSlug(6)
		if err != nil {
			return Room{}, err
		}
		created, err = insertRoom(ctx, tx, slug, ownerID, input, secretHash, now, expiresAt)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Room{}, fmt.Errorf("insert room: %w", err)
		}
		if attempt == 7 {
			return Room{}, errors.New("generate unique room slug")
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO room_members (room_id, identity_id, role, joined_at, last_seen_at)
		VALUES ($1, $2, 'owner', $3, $3)
	`, created.ID, ownerID, now); err != nil {
		return Room{}, fmt.Errorf("add room owner: %w", err)
	}
	if err := recordEvent(ctx, tx, created.ID, "room_created", ownerID, nil); err != nil {
		return Room{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE room_creation_requests SET room_id = $1
		WHERE identity_id = $2 AND idempotency_key = $3
	`, created.ID, ownerID, idempotencyKey); err != nil {
		return Room{}, fmt.Errorf("complete idempotent room request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, fmt.Errorf("commit room creation: %w", err)
	}
	return created, nil
}

func (s *Service) Preview(ctx context.Context, slug string) (Preview, error) {
	var preview Preview
	var legacyInvitesEnabled bool
	err := s.db.QueryRow(ctx, `
		SELECT slug, name, access_mode, status, accepting_members, expires_at, legacy_invites_enabled
		FROM rooms
		WHERE slug = $1
	`, normalizeSlug(slug)).Scan(&preview.Slug, &preview.Name, &preview.AccessMode, &preview.Status, &preview.AcceptingMembers, &preview.ExpiresAt, &legacyInvitesEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Preview{}, ErrNotFound
	}
	if err != nil {
		return Preview{}, fmt.Errorf("get room preview: %w", err)
	}
	if preview.Status != "active" || !preview.ExpiresAt.After(s.now()) {
		return Preview{}, ErrExpired
	}
	if !legacyInvitesEnabled {
		return Preview{}, ErrInviteNotFound
	}
	return preview, nil
}

func (s *Service) List(ctx context.Context, identityID uuid.UUID) ([]Room, error) {
	rows, err := s.db.Query(ctx, `
		SELECT r.id, r.slug, r.name, r.access_mode, rm.role, rm.can_upload, r.status,
		       r.accepting_uploads, r.accepting_members, r.max_files, r.max_storage_bytes,
		       r.used_files, r.used_storage_bytes, r.created_at, r.expires_at
		FROM room_members rm
		JOIN rooms r ON r.id = rm.room_id
		WHERE rm.identity_id = $1
		  AND r.status = 'active'
		  AND r.expires_at > $2
		ORDER BY rm.last_seen_at DESC, r.created_at DESC
		LIMIT 50
	`, identityID, s.now().UTC())
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()

	result := make([]Room, 0)
	for rows.Next() {
		var item Room
		if err := rows.Scan(
			&item.ID, &item.Slug, &item.Name, &item.AccessMode, &item.Role, &item.CanUpload, &item.Status,
			&item.AcceptingUploads, &item.AcceptingMembers, &item.MaxFiles, &item.MaxStorageBytes,
			&item.UsedFiles, &item.UsedStorageBytes, &item.CreatedAt, &item.ExpiresAt,
		); err != nil {
			return nil, fmt.Errorf("scan listed room: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate listed rooms: %w", err)
	}
	return result, nil
}

func (s *Service) Members(ctx context.Context, identityID uuid.UUID, slug string) (MembersResult, error) {
	currentRoom, err := s.Get(ctx, identityID, slug)
	if err != nil {
		return MembersResult{}, err
	}
	if currentRoom.Role != "owner" {
		return MembersResult{}, ErrOwnerRequired
	}

	rows, err := s.db.Query(ctx, `
		SELECT i.id, i.display_name, rm.role, rm.can_upload, rm.joined_at, rm.last_seen_at
		FROM room_members rm
		JOIN identities i ON i.id = rm.identity_id
		WHERE rm.room_id = $1
		ORDER BY CASE WHEN rm.role = 'owner' THEN 0 ELSE 1 END, rm.joined_at ASC
	`, currentRoom.ID)
	if err != nil {
		return MembersResult{}, fmt.Errorf("list room members: %w", err)
	}

	members := make([]Member, 0)
	for rows.Next() {
		var member Member
		if err := rows.Scan(&member.ID, &member.DisplayName, &member.Role, &member.CanUpload, &member.JoinedAt, &member.LastSeenAt); err != nil {
			rows.Close()
			return MembersResult{}, fmt.Errorf("scan room member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return MembersResult{}, fmt.Errorf("iterate room members: %w", err)
	}
	rows.Close()

	blockedRows, err := s.db.Query(ctx, `
		SELECT i.id, i.display_name, rb.created_at
		FROM room_bans rb
		JOIN identities i ON i.id = rb.identity_id
		WHERE rb.room_id = $1
		ORDER BY rb.created_at DESC
	`, currentRoom.ID)
	if err != nil {
		return MembersResult{}, fmt.Errorf("list blocked room members: %w", err)
	}
	defer blockedRows.Close()

	blocked := make([]BlockedMember, 0)
	for blockedRows.Next() {
		var member BlockedMember
		if err := blockedRows.Scan(&member.ID, &member.DisplayName, &member.BlockedAt); err != nil {
			return MembersResult{}, fmt.Errorf("scan blocked room member: %w", err)
		}
		blocked = append(blocked, member)
	}
	if err := blockedRows.Err(); err != nil {
		return MembersResult{}, fmt.Errorf("iterate blocked room members: %w", err)
	}
	return MembersResult{Members: members, Blocked: blocked}, nil
}

func (s *Service) Join(ctx context.Context, identityID uuid.UUID, slug, secret string) (Room, error) {
	return s.join(ctx, identityID, slug, secret, "")
}

func (s *Service) join(ctx context.Context, identityID uuid.UUID, slug, secret, inviteToken string) (Room, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Room{}, fmt.Errorf("begin join room transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var roomID uuid.UUID
	var accessMode, status string
	var secretHash *string
	var expiresAt time.Time
	var acceptingMembers bool
	var legacyInvitesEnabled bool
	err = tx.QueryRow(ctx, `
		SELECT id, access_mode, access_secret_hash, status, accepting_members, expires_at, legacy_invites_enabled
		FROM rooms
		WHERE slug = $1
		FOR UPDATE
	`, normalizeSlug(slug)).Scan(&roomID, &accessMode, &secretHash, &status, &acceptingMembers, &expiresAt, &legacyInvitesEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("find room to join: %w", err)
	}
	if status != "active" || !expiresAt.After(s.now()) {
		return Room{}, ErrExpired
	}
	canUpload := true
	if inviteToken == "" {
		if !legacyInvitesEnabled {
			return Room{}, ErrInviteNotFound
		}
	} else {
		var permission string
		err := tx.QueryRow(ctx, `
			SELECT permission FROM room_invites
			WHERE token = $1 AND room_id = $2 AND revoked_at IS NULL
			FOR SHARE
		`, inviteToken, roomID).Scan(&permission)
		if errors.Is(err, pgx.ErrNoRows) {
			return Room{}, ErrInviteNotFound
		}
		if err != nil {
			return Room{}, fmt.Errorf("validate room invitation: %w", err)
		}
		canUpload = permission == "contributor"
	}
	var blocked bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM room_bans WHERE room_id = $1 AND identity_id = $2
		)
	`, roomID, identityID).Scan(&blocked); err != nil {
		return Room{}, fmt.Errorf("check room block: %w", err)
	}
	if blocked {
		return Room{}, ErrMemberBlocked
	}
	var alreadyMember bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM room_members WHERE room_id = $1 AND identity_id = $2
		)
	`, roomID, identityID).Scan(&alreadyMember); err != nil {
		return Room{}, fmt.Errorf("check room membership: %w", err)
	}
	if !acceptingMembers && !alreadyMember {
		return Room{}, ErrJoiningClosed
	}
	if accessMode != "public" {
		if secretHash == nil {
			return Room{}, errors.New("protected room has no secret hash")
		}
		matches, err := verifySecret(*secretHash, secret)
		if err != nil {
			return Room{}, fmt.Errorf("verify room secret: %w", err)
		}
		if !matches {
			return Room{}, ErrAccessDenied
		}
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO room_members (room_id, identity_id, role, can_upload)
		VALUES ($1, $2, 'member', $3)
		ON CONFLICT (room_id, identity_id)
		DO UPDATE SET last_seen_at = now(), can_upload = room_members.can_upload OR EXCLUDED.can_upload
	`, roomID, identityID, canUpload); err != nil {
		return Room{}, fmt.Errorf("join room: %w", err)
	}
	if !alreadyMember {
		if err := recordEvent(ctx, tx, roomID, "member_joined", identityID, nil); err != nil {
			return Room{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO telegram_notification_outbox (room_id, telegram_user_id, event_type, payload)
			SELECT room.id, recipient.telegram_user_id, 'member_joined', jsonb_build_object(
				'room_name', room.name, 'room_slug', room.slug, 'actor_name', actor.display_name
			)
			FROM rooms room
			JOIN identities actor ON actor.id = $2
			JOIN room_notification_preferences preference ON preference.room_id = room.id
			JOIN identities recipient ON recipient.id = preference.identity_id
			WHERE room.id = $1 AND preference.telegram_enabled AND preference.member_joined_enabled
			  AND recipient.telegram_user_id IS NOT NULL AND recipient.id <> actor.id
		`, roomID, identityID); err != nil {
			return Room{}, fmt.Errorf("enqueue member notification: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, fmt.Errorf("commit room join: %w", err)
	}
	if inviteToken != "" {
		_, _ = s.db.Exec(ctx, `UPDATE room_invites SET last_used_at = now(), join_count = join_count + 1 WHERE token = $1 AND revoked_at IS NULL`, inviteToken)
	}
	return s.Get(ctx, identityID, slug)
}

func (s *Service) Get(ctx context.Context, identityID uuid.UUID, slug string) (Room, error) {
	var result Room
	err := s.db.QueryRow(ctx, `
		SELECT r.id, r.slug, r.name, r.access_mode, rm.role, rm.can_upload, r.status,
		       r.accepting_uploads, r.accepting_members, r.max_files, r.max_storage_bytes,
		       r.used_files, r.used_storage_bytes, r.created_at, r.expires_at
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = $2
	`, identityID, normalizeSlug(slug)).Scan(
		&result.ID, &result.Slug, &result.Name, &result.AccessMode, &result.Role, &result.CanUpload, &result.Status,
		&result.AcceptingUploads, &result.AcceptingMembers, &result.MaxFiles, &result.MaxStorageBytes,
		&result.UsedFiles, &result.UsedStorageBytes, &result.CreatedAt, &result.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var exists bool
		if checkErr := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM rooms WHERE slug = $1)`, normalizeSlug(slug)).Scan(&exists); checkErr != nil {
			return Room{}, fmt.Errorf("check room existence: %w", checkErr)
		}
		if exists {
			return Room{}, ErrNotMember
		}
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("get room: %w", err)
	}
	if result.Status != "active" || !result.ExpiresAt.After(s.now()) {
		return Room{}, ErrExpired
	}
	return result, nil
}

func (s *Service) Notifications(ctx context.Context, identityID uuid.UUID, slug string) (NotificationSettings, error) {
	var result NotificationSettings
	err := s.db.QueryRow(ctx, `
		SELECT identity.telegram_user_id IS NOT NULL,
		       COALESCE(preference.telegram_enabled, false),
		       COALESCE(preference.new_media_enabled, true),
		       COALESCE(preference.expiry_enabled, true),
		       COALESCE(preference.member_joined_enabled, rm.role = 'owner'),
		       rm.role = 'owner'
		FROM rooms room
		JOIN room_members rm ON rm.room_id = room.id AND rm.identity_id = $1
		JOIN identities identity ON identity.id = $1
		LEFT JOIN room_notification_preferences preference ON preference.room_id = room.id AND preference.identity_id = $1
		WHERE room.slug = $2 AND room.status = 'active' AND room.expires_at > $3
	`, identityID, normalizeSlug(slug), s.now()).Scan(&result.TelegramAvailable, &result.TelegramEnabled, &result.NewMediaEnabled, &result.ExpiryEnabled, &result.MemberJoinedEnabled, &result.IsOwner)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationSettings{}, ErrNotFound
	}
	if err != nil {
		return NotificationSettings{}, fmt.Errorf("get notification settings: %w", err)
	}
	return result, nil
}

func (s *Service) UpdateNotifications(ctx context.Context, identityID uuid.UUID, slug string, input NotificationUpdate) (NotificationSettings, error) {
	if input.TelegramEnabled == nil && input.NewMediaEnabled == nil && input.ExpiryEnabled == nil && input.MemberJoinedEnabled == nil {
		return NotificationSettings{}, fmt.Errorf("%w: at least one notification setting is required", ErrInvalidInput)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return NotificationSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roomID uuid.UUID
	var status, role string
	var expiresAt time.Time
	var telegramAvailable, enabled, newMedia, expiry, memberJoined bool
	err = tx.QueryRow(ctx, `
		SELECT room.id, room.status, room.expires_at, member.role,
		       identity.telegram_user_id IS NOT NULL,
		       COALESCE(preference.telegram_enabled, false),
		       COALESCE(preference.new_media_enabled, true),
		       COALESCE(preference.expiry_enabled, true),
		       COALESCE(preference.member_joined_enabled, member.role = 'owner')
		FROM rooms room
		JOIN room_members member ON member.room_id = room.id AND member.identity_id = $1
		JOIN identities identity ON identity.id = $1
		LEFT JOIN room_notification_preferences preference ON preference.room_id = room.id AND preference.identity_id = $1
		WHERE room.slug = $2
		FOR UPDATE OF room
	`, identityID, normalizeSlug(slug)).Scan(&roomID, &status, &expiresAt, &role, &telegramAvailable, &enabled, &newMedia, &expiry, &memberJoined)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationSettings{}, ErrNotFound
	}
	if err != nil {
		return NotificationSettings{}, err
	}
	if status != "active" || !expiresAt.After(s.now()) {
		return NotificationSettings{}, ErrExpired
	}
	if input.TelegramEnabled != nil {
		enabled = *input.TelegramEnabled
	}
	if input.NewMediaEnabled != nil {
		newMedia = *input.NewMediaEnabled
	}
	if input.ExpiryEnabled != nil {
		expiry = *input.ExpiryEnabled
	}
	if input.MemberJoinedEnabled != nil {
		memberJoined = *input.MemberJoinedEnabled
	}
	if enabled && !telegramAvailable {
		return NotificationSettings{}, fmt.Errorf("%w: link a Telegram account before enabling notifications", ErrInvalidInput)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO room_notification_preferences (room_id, identity_id, telegram_enabled, new_media_enabled, expiry_enabled, member_joined_enabled)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (room_id, identity_id) DO UPDATE
		SET telegram_enabled = EXCLUDED.telegram_enabled,
		    new_media_enabled = EXCLUDED.new_media_enabled,
		    expiry_enabled = EXCLUDED.expiry_enabled,
		    member_joined_enabled = EXCLUDED.member_joined_enabled,
		    updated_at = now()
	`, roomID, identityID, enabled, newMedia, expiry, memberJoined); err != nil {
		return NotificationSettings{}, fmt.Errorf("update notification settings: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM telegram_notification_outbox
		WHERE room_id = $1 AND sent_at IS NULL
		  AND telegram_user_id = (SELECT telegram_user_id FROM identities WHERE id = $2)
		  AND (
		    NOT $3::boolean
		    OR (event_type = 'media_uploaded' AND NOT $4::boolean)
		    OR (event_type = 'room_expiry' AND NOT $5::boolean)
		    OR (event_type = 'member_joined' AND NOT $6::boolean)
		  )
	`, roomID, identityID, enabled, newMedia, expiry, memberJoined); err != nil {
		return NotificationSettings{}, fmt.Errorf("clear disabled notifications: %w", err)
	}
	if err := scheduleExpiryNotifications(ctx, tx, roomID); err != nil {
		return NotificationSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return NotificationSettings{}, err
	}
	return NotificationSettings{TelegramAvailable: telegramAvailable, TelegramEnabled: enabled, NewMediaEnabled: newMedia, ExpiryEnabled: expiry, MemberJoinedEnabled: memberJoined, IsOwner: role == "owner"}, nil
}

func (s *Service) Update(ctx context.Context, identityID uuid.UUID, slug string, input UpdateInput) (Room, error) {
	if input.Name == nil && input.AcceptingUploads == nil && input.AcceptingMembers == nil && input.Access == nil && input.LifetimeDays == nil {
		return Room{}, fmt.Errorf("%w: at least one field is required", ErrInvalidInput)
	}
	if input.LifetimeDays != nil && (*input.LifetimeDays < 1 || *input.LifetimeDays > 3) {
		return Room{}, fmt.Errorf("%w: lifetime_days must be between 1 and 3", ErrInvalidInput)
	}
	var name string
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
		if count := utf8.RuneCountInString(name); count < 1 || count > 120 {
			return Room{}, fmt.Errorf("%w: name must contain 1 to 120 characters", ErrInvalidInput)
		}
	}
	var accessMode string
	var accessHash *string
	if input.Access != nil {
		accessMode = input.Access.Mode
		if err := validateAccess(accessMode, input.Access.Secret); err != nil {
			return Room{}, err
		}
		if accessMode != "public" {
			hashed, err := hashSecret(input.Access.Secret)
			if err != nil {
				return Room{}, err
			}
			accessHash = &hashed
		}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Room{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roomID uuid.UUID
	var role, status string
	var expiresAt, createdAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT r.id, rm.role, r.status, r.created_at, r.expires_at
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = $2
		FOR UPDATE OF r
	`, identityID, normalizeSlug(slug)).Scan(&roomID, &role, &status, &createdAt, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, ErrNotFound
	}
	if err != nil {
		return Room{}, err
	}
	if role != "owner" {
		return Room{}, ErrOwnerRequired
	}
	if status != "active" || !expiresAt.After(s.now()) {
		return Room{}, ErrExpired
	}
	var updatedExpiry time.Time
	if input.LifetimeDays != nil {
		updatedExpiry = createdAt.Add(time.Duration(*input.LifetimeDays) * 24 * time.Hour)
		if updatedExpiry.Before(expiresAt) {
			return Room{}, fmt.Errorf("%w: room lifetime can only be extended", ErrInvalidInput)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE rooms
		SET name = CASE WHEN $2 THEN $3 ELSE name END,
		    accepting_uploads = CASE WHEN $4 THEN $5 ELSE accepting_uploads END,
		    accepting_members = CASE WHEN $6 THEN $7 ELSE accepting_members END,
		    access_mode = CASE WHEN $8 THEN $9 ELSE access_mode END,
		    access_secret_hash = CASE WHEN $8 THEN $10 ELSE access_secret_hash END,
		    expires_at = CASE WHEN $11 THEN $12 ELSE expires_at END
		WHERE id = $1
	`, roomID,
		input.Name != nil, name,
		input.AcceptingUploads != nil, input.AcceptingUploads,
		input.AcceptingMembers != nil, input.AcceptingMembers,
		input.Access != nil, accessMode, accessHash,
		input.LifetimeDays != nil, updatedExpiry,
	); err != nil {
		return Room{}, fmt.Errorf("update room: %w", err)
	}
	if err := recordEvent(ctx, tx, roomID, "room_updated", identityID, nil); err != nil {
		return Room{}, err
	}
	if input.Name != nil || input.LifetimeDays != nil {
		if err := scheduleExpiryNotifications(ctx, tx, roomID); err != nil {
			return Room{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, err
	}
	return s.Get(ctx, identityID, slug)
}

func (s *Service) RemoveMember(ctx context.Context, ownerID uuid.UUID, slug string, memberID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin remove member transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	roomID, err := s.lockOwnedRoom(ctx, tx, ownerID, slug)
	if err != nil {
		return err
	}
	var role string
	err = tx.QueryRow(ctx, `
		SELECT role FROM room_members WHERE room_id = $1 AND identity_id = $2
	`, roomID, memberID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberNotFound
	}
	if err != nil {
		return fmt.Errorf("find member to remove: %w", err)
	}
	if role == "owner" {
		return ErrCannotRemoveOwner
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM telegram_notification_outbox
		WHERE room_id = $1 AND sent_at IS NULL
		  AND telegram_user_id = (SELECT telegram_user_id FROM identities WHERE id = $2)
	`, roomID, memberID); err != nil {
		return fmt.Errorf("clear removed member notifications: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM room_notification_preferences WHERE room_id = $1 AND identity_id = $2`, roomID, memberID); err != nil {
		return fmt.Errorf("clear removed member notification preference: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM room_members WHERE room_id = $1 AND identity_id = $2
	`, roomID, memberID); err != nil {
		return fmt.Errorf("remove room member: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO room_bans (room_id, identity_id, banned_by_identity_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (room_id, identity_id)
		DO UPDATE SET banned_by_identity_id = EXCLUDED.banned_by_identity_id, created_at = now()
	`, roomID, memberID, ownerID); err != nil {
		return fmt.Errorf("block removed room member: %w", err)
	}
	if err := recordEvent(ctx, tx, roomID, "member_removed", ownerID, &memberID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit remove member: %w", err)
	}
	return nil
}

func (s *Service) UnblockMember(ctx context.Context, ownerID uuid.UUID, slug string, memberID uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin unblock member transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	roomID, err := s.lockOwnedRoom(ctx, tx, ownerID, slug)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `
		DELETE FROM room_bans WHERE room_id = $1 AND identity_id = $2
	`, roomID, memberID)
	if err != nil {
		return fmt.Errorf("unblock room member: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrMemberNotBlocked
	}
	if err := recordEvent(ctx, tx, roomID, "member_unblocked", ownerID, &memberID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit unblock member: %w", err)
	}
	return nil
}

func (s *Service) TransferOwnership(ctx context.Context, ownerID uuid.UUID, slug string, memberID uuid.UUID) (Room, error) {
	if ownerID == memberID {
		return Room{}, fmt.Errorf("%w: select another room member", ErrInvalidInput)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Room{}, fmt.Errorf("begin ownership transfer transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	roomID, err := s.lockOwnedRoom(ctx, tx, ownerID, slug)
	if err != nil {
		return Room{}, err
	}
	var targetRole string
	err = tx.QueryRow(ctx, `
		SELECT role FROM room_members WHERE room_id = $1 AND identity_id = $2 FOR UPDATE
	`, roomID, memberID).Scan(&targetRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, ErrMemberNotFound
	}
	if err != nil {
		return Room{}, fmt.Errorf("find ownership recipient: %w", err)
	}
	if targetRole != "member" {
		return Room{}, fmt.Errorf("%w: select a regular room member", ErrInvalidInput)
	}
	if _, err := tx.Exec(ctx, `UPDATE room_members SET role = 'member' WHERE room_id = $1 AND identity_id = $2`, roomID, ownerID); err != nil {
		return Room{}, fmt.Errorf("demote previous room owner: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE room_members SET role = 'owner' WHERE room_id = $1 AND identity_id = $2`, roomID, memberID); err != nil {
		return Room{}, fmt.Errorf("promote new room owner: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE rooms SET owner_identity_id = $2 WHERE id = $1`, roomID, memberID); err != nil {
		return Room{}, fmt.Errorf("update room owner: %w", err)
	}
	if err := recordEvent(ctx, tx, roomID, "ownership_transferred", ownerID, &memberID); err != nil {
		return Room{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Room{}, fmt.Errorf("commit ownership transfer: %w", err)
	}
	return s.Get(ctx, ownerID, slug)
}

func (s *Service) Activity(ctx context.Context, identityID uuid.UUID, slug string) ([]ActivityEvent, error) {
	currentRoom, err := s.Get(ctx, identityID, slug)
	if err != nil {
		return nil, err
	}
	if currentRoom.Role != "owner" {
		return nil, ErrOwnerRequired
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, event_type, actor_identity_id, actor_display_name,
		       subject_identity_id, subject_display_name, created_at
		FROM room_events
		WHERE room_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT 100
	`, currentRoom.ID)
	if err != nil {
		return nil, fmt.Errorf("list room activity: %w", err)
	}
	defer rows.Close()
	result := make([]ActivityEvent, 0)
	for rows.Next() {
		var event ActivityEvent
		if err := rows.Scan(
			&event.ID, &event.Type, &event.ActorID, &event.ActorDisplayName,
			&event.SubjectID, &event.SubjectDisplayName, &event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan room activity: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate room activity: %w", err)
	}
	return result, nil
}

func (s *Service) Delete(ctx context.Context, identityID uuid.UUID, slug string) error {
	var role, status string
	var expiresAt time.Time
	err := s.db.QueryRow(ctx, `
		SELECT rm.role, r.status, r.expires_at
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = $2
	`, identityID, normalizeSlug(slug)).Scan(&role, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role != "owner" {
		return ErrOwnerRequired
	}
	if status == "deleting" {
		return nil
	}
	if !expiresAt.After(s.now()) {
		return ErrExpired
	}
	result, err := s.db.Exec(ctx, `
		UPDATE rooms
		SET status = 'deleting', accepting_uploads = false, accepting_members = false
		WHERE slug = $1 AND owner_identity_id = $2 AND status = 'active'
	`, normalizeSlug(slug), identityID)
	if err != nil {
		return fmt.Errorf("schedule room deletion: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) lockOwnedRoom(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, slug string) (uuid.UUID, error) {
	var roomID uuid.UUID
	var role, status string
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `
		SELECT r.id, rm.role, r.status, r.expires_at
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.slug = $2
		FOR UPDATE OF r
	`, identityID, normalizeSlug(slug)).Scan(&roomID, &role, &status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("lock owned room: %w", err)
	}
	if role != "owner" {
		return uuid.Nil, ErrOwnerRequired
	}
	if status != "active" || !expiresAt.After(s.now()) {
		return uuid.Nil, ErrExpired
	}
	return roomID, nil
}

func recordEvent(ctx context.Context, tx pgx.Tx, roomID uuid.UUID, eventType string, actorID uuid.UUID, subjectID *uuid.UUID) error {
	result, err := tx.Exec(ctx, `
		INSERT INTO room_events (
			room_id, event_type, actor_identity_id, actor_display_name,
			subject_identity_id, subject_display_name
		)
		SELECT $1, $2, actor.id, actor.display_name, subject.id, subject.display_name
		FROM identities actor
		LEFT JOIN identities subject ON subject.id = $4
		WHERE actor.id = $3
	`, roomID, eventType, actorID, subjectID)
	if err != nil {
		return fmt.Errorf("record room event: %w", err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("record room event actor not found")
	}
	return nil
}

func scheduleExpiryNotifications(ctx context.Context, tx pgx.Tx, roomID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM telegram_notification_outbox
		WHERE room_id = $1 AND event_type = 'room_expiry' AND sent_at IS NULL
	`, roomID); err != nil {
		return fmt.Errorf("clear expiry notifications: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO telegram_notification_outbox (
			room_id, telegram_user_id, event_type, payload, dedupe_key, available_at
		)
		SELECT room.id, identity.telegram_user_id, 'room_expiry',
		       jsonb_build_object(
			       'room_name', room.name,
			       'room_slug', room.slug,
			       'hours_remaining', reminder.hours,
			       'expires_at', room.expires_at
		       ),
		       format('room-expiry:%s:%s:%s:%s', room.id, identity.id, extract(epoch FROM room.expires_at)::bigint, reminder.hours),
		       GREATEST(now(), room.expires_at - reminder.hours * interval '1 hour')
		FROM rooms room
		JOIN room_notification_preferences preference ON preference.room_id = room.id
		JOIN identities identity ON identity.id = preference.identity_id AND identity.telegram_user_id IS NOT NULL
		CROSS JOIN (VALUES (6), (1)) AS reminder(hours)
		WHERE room.id = $1 AND room.status = 'active' AND room.expires_at > now()
		  AND preference.telegram_enabled AND preference.expiry_enabled
		  AND (reminder.hours = 1 OR room.expires_at > now() + interval '1 hour')
		ON CONFLICT (dedupe_key) DO NOTHING
	`, roomID); err != nil {
		return fmt.Errorf("schedule expiry notifications: %w", err)
	}
	return nil
}

func validateCreate(input CreateInput) error {
	if count := utf8.RuneCountInString(input.Name); count < 1 || count > 120 {
		return fmt.Errorf("%w: name must contain 1 to 120 characters", ErrInvalidInput)
	}
	if input.LifetimeDays < 1 || input.LifetimeDays > 3 {
		return fmt.Errorf("%w: lifetime_days must be 1, 2, or 3", ErrInvalidInput)
	}
	return validateAccess(input.AccessMode, input.Secret)
}

func validateAccess(mode, secret string) error {
	switch mode {
	case "public":
		if secret != "" {
			return fmt.Errorf("%w: public room cannot have a secret", ErrInvalidInput)
		}
	case "pin":
		if !pinPattern.MatchString(secret) {
			return fmt.Errorf("%w: PIN must contain 4 to 8 digits", ErrInvalidInput)
		}
	case "password":
		if count := utf8.RuneCountInString(secret); count < 6 || count > 72 {
			return fmt.Errorf("%w: password must contain 6 to 72 characters", ErrInvalidInput)
		}
	default:
		return fmt.Errorf("%w: access mode must be public, pin, or password", ErrInvalidInput)
	}
	return nil
}

func hashCreateInput(input CreateInput) [32]byte {
	canonical := fmt.Sprintf("%s\x00%d\x00%s\x00%s", input.Name, input.LifetimeDays, input.AccessMode, input.Secret)
	return sha256.Sum256([]byte(canonical))
}

func existingIdempotentRoom(ctx context.Context, tx pgx.Tx, identityID, key uuid.UUID, expectedHash [32]byte) (Room, error) {
	var storedHash []byte
	var roomID uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT request_hash, room_id
		FROM room_creation_requests
		WHERE identity_id = $1 AND idempotency_key = $2
	`, identityID, key).Scan(&storedHash, &roomID); err != nil {
		return Room{}, fmt.Errorf("read idempotent room request: %w", err)
	}
	if !bytes.Equal(storedHash, expectedHash[:]) {
		return Room{}, ErrIdempotencyConflict
	}
	var result Room
	err := tx.QueryRow(ctx, `
		SELECT r.id, r.slug, r.name, r.access_mode, rm.role, rm.can_upload, r.status,
		       r.accepting_uploads, r.accepting_members, r.max_files, r.max_storage_bytes,
		       r.used_files, r.used_storage_bytes, r.created_at, r.expires_at
		FROM rooms r
		JOIN room_members rm ON rm.room_id = r.id AND rm.identity_id = $1
		WHERE r.id = $2
	`, identityID, roomID).Scan(
		&result.ID, &result.Slug, &result.Name, &result.AccessMode, &result.Role, &result.CanUpload, &result.Status,
		&result.AcceptingUploads, &result.AcceptingMembers, &result.MaxFiles, &result.MaxStorageBytes,
		&result.UsedFiles, &result.UsedStorageBytes, &result.CreatedAt, &result.ExpiresAt,
	)
	if err != nil {
		return Room{}, fmt.Errorf("read idempotently created room: %w", err)
	}
	return result, nil
}

func insertRoom(ctx context.Context, tx pgx.Tx, slug string, ownerID uuid.UUID, input CreateInput, secretHash *string, createdAt, expiresAt time.Time) (Room, error) {
	var result Room
	result.Role = "owner"
	result.CanUpload = true
	err := tx.QueryRow(ctx, `
		INSERT INTO rooms (
			slug, name, owner_identity_id, access_mode, access_secret_hash, created_at, expires_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (slug) DO NOTHING
		RETURNING id, slug, name, access_mode, status, accepting_uploads, accepting_members,
		          max_files, max_storage_bytes, used_files, used_storage_bytes, created_at, expires_at
	`, slug, input.Name, ownerID, input.AccessMode, secretHash, createdAt, expiresAt).Scan(
		&result.ID, &result.Slug, &result.Name, &result.AccessMode, &result.Status,
		&result.AcceptingUploads, &result.AcceptingMembers, &result.MaxFiles, &result.MaxStorageBytes,
		&result.UsedFiles, &result.UsedStorageBytes, &result.CreatedAt, &result.ExpiresAt,
	)
	return result, err
}

func newSlug(length int) (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate room slug: %w", err)
	}
	result := make([]byte, length)
	for index, value := range random {
		result[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(result), nil
}

func normalizeSlug(slug string) string {
	return strings.ToUpper(strings.TrimSpace(slug))
}
