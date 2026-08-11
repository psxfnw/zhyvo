package realtime

import (
	"testing"

	"github.com/google/uuid"
)

func TestBrokerWakesOnlyMatchingRoom(t *testing.T) {
	broker := &Broker{rooms: make(map[uuid.UUID]map[chan struct{}]struct{})}
	firstRoom := uuid.New()
	secondRoom := uuid.New()
	first, unsubscribeFirst := broker.Subscribe(firstRoom)
	defer unsubscribeFirst()
	second, unsubscribeSecond := broker.Subscribe(secondRoom)
	defer unsubscribeSecond()

	broker.wake(firstRoom)
	select {
	case <-first:
	default:
		t.Fatal("matching room subscriber was not notified")
	}
	select {
	case <-second:
		t.Fatal("unrelated room subscriber was notified")
	default:
	}
}

func TestBrokerCoalescesPendingWakeups(t *testing.T) {
	broker := &Broker{rooms: make(map[uuid.UUID]map[chan struct{}]struct{})}
	roomID := uuid.New()
	subscriber, unsubscribe := broker.Subscribe(roomID)
	defer unsubscribe()

	broker.wake(roomID)
	broker.wake(roomID)
	<-subscriber
	select {
	case <-subscriber:
		t.Fatal("duplicate pending wakeup was not coalesced")
	default:
	}
}
