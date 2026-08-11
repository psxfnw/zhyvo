package realtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotMember = errors.New("identity is not an active room member")

type Event struct {
	ID        int64      `json:"id"`
	Type      string     `json:"type"`
	EntityID  *uuid.UUID `json:"entity_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type Service struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db, now: time.Now}
}

func (service *Service) ResolveRoom(ctx context.Context, identityID uuid.UUID, slug string) (uuid.UUID, int64, error) {
	var roomID uuid.UUID
	var cursor int64
	err := service.db.QueryRow(ctx, `
		SELECT room.id, COALESCE(max(event.id), 0)
		FROM rooms room
		JOIN room_members member ON member.room_id = room.id AND member.identity_id = $1
		LEFT JOIN room_realtime_events event ON event.room_id = room.id
		WHERE room.slug = upper(trim($2)) AND room.status = 'active' AND room.expires_at > $3
		GROUP BY room.id
	`, identityID, slug, service.now().UTC()).Scan(&roomID, &cursor)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, 0, ErrNotMember
	}
	if err != nil {
		return uuid.Nil, 0, fmt.Errorf("resolve realtime room: %w", err)
	}
	return roomID, cursor, nil
}

func (service *Service) EventsAfter(ctx context.Context, identityID, roomID uuid.UUID, cursor int64, limit int) ([]Event, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	var active bool
	err := service.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM rooms room
			JOIN room_members member ON member.room_id = room.id AND member.identity_id = $1
			WHERE room.id = $2 AND room.status = 'active' AND room.expires_at > $3
		)
	`, identityID, roomID, service.now().UTC()).Scan(&active)
	if err != nil {
		return nil, fmt.Errorf("authorize realtime events: %w", err)
	}
	if !active {
		return nil, ErrNotMember
	}
	rows, err := service.db.Query(ctx, `
		SELECT id, event_type, entity_id, created_at
		FROM room_realtime_events
		WHERE room_id = $1 AND id > $2
		ORDER BY id
		LIMIT $3
	`, roomID, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("query realtime events: %w", err)
	}
	defer rows.Close()
	result := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Type, &event.EntityID, &event.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan realtime event: %w", err)
		}
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate realtime events: %w", err)
	}
	return result, nil
}
