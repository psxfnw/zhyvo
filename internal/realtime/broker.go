package realtime

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const notificationChannel = "room_realtime"

// Broker keeps one PostgreSQL LISTEN connection per API process and fans a
// lightweight wake-up signal out only to clients subscribed to that room.
// Events themselves remain durable in PostgreSQL and are fetched by ID.
type Broker struct {
	db     *pgxpool.Pool
	logger *slog.Logger
	mu     sync.RWMutex
	rooms  map[uuid.UUID]map[chan struct{}]struct{}
}

func NewBroker(db *pgxpool.Pool, logger *slog.Logger) *Broker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Broker{db: db, logger: logger, rooms: make(map[uuid.UUID]map[chan struct{}]struct{})}
}

func (broker *Broker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := broker.listen(ctx); err != nil && ctx.Err() == nil {
			broker.logger.Warn("realtime listener disconnected", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}
}

func (broker *Broker) listen(ctx context.Context) error {
	connection, err := broker.db.Acquire(ctx)
	if err != nil {
		return err
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, "LISTEN "+notificationChannel); err != nil {
		return err
	}
	for {
		notification, err := connection.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		parts := strings.SplitN(notification.Payload, ":", 2)
		if len(parts) != 2 {
			continue
		}
		roomID, err := uuid.Parse(parts[0])
		if err != nil {
			continue
		}
		broker.wake(roomID)
	}
}

func (broker *Broker) Subscribe(roomID uuid.UUID) (<-chan struct{}, func()) {
	channel := make(chan struct{}, 1)
	broker.mu.Lock()
	if broker.rooms[roomID] == nil {
		broker.rooms[roomID] = make(map[chan struct{}]struct{})
	}
	broker.rooms[roomID][channel] = struct{}{}
	broker.mu.Unlock()
	return channel, func() {
		broker.mu.Lock()
		delete(broker.rooms[roomID], channel)
		if len(broker.rooms[roomID]) == 0 {
			delete(broker.rooms, roomID)
		}
		broker.mu.Unlock()
	}
}

func (broker *Broker) wake(roomID uuid.UUID) {
	broker.mu.RLock()
	defer broker.mu.RUnlock()
	for channel := range broker.rooms[roomID] {
		select {
		case channel <- struct{}{}:
		default:
		}
	}
}
