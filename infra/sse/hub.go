package sse

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/realtime"
)

type Connection struct {
	wake     chan struct{}
	position atomic.Pointer[string]
}

func (c *Connection) Wake() <-chan struct{} {
	return c.wake
}

func (c *Connection) TakePositionRequest() string {
	requestID := c.position.Swap(nil)
	if requestID == nil {
		return ""
	}
	return *requestID
}

func (c *Connection) signal() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[*Connection]struct{}
	draining    chan struct{}
	drainOnce   sync.Once
	spread      time.Duration
}

func NewHub(reconnectSpread time.Duration) *Hub {
	return &Hub{
		subscribers: make(map[string]map[*Connection]struct{}),
		draining:    make(chan struct{}),
		spread:      reconnectSpread,
	}
}

func (h *Hub) Subscribe(userID string) (*Connection, func()) {
	connection := &Connection{wake: make(chan struct{}, 1)}

	h.mu.Lock()
	if h.subscribers[userID] == nil {
		h.subscribers[userID] = make(map[*Connection]struct{})
	}
	h.subscribers[userID][connection] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return connection, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers[userID], connection)
			if len(h.subscribers[userID]) == 0 {
				delete(h.subscribers, userID)
			}
			h.mu.Unlock()
		})
	}
}

func (h *Hub) Signal(userID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for connection := range h.subscribers[userID] {
		connection.signal()
	}
}

func (h *Hub) SignalPosition(userID, requestID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for connection := range h.subscribers[userID] {
		connection.position.Store(&requestID)
		connection.signal()
	}
}

func (h *Hub) Subscribers() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, connections := range h.subscribers {
		total += len(connections)
	}
	return total
}

func (h *Hub) Drain() {
	h.drainOnce.Do(func() { close(h.draining) })
}

func (h *Hub) Draining() <-chan struct{} {
	return h.draining
}

func (h *Hub) ReconnectSpread() time.Duration {
	return h.spread
}

func (h *Hub) RunDoor(ctx context.Context, rdb *goredis.Client) {
	subscription := rdb.Subscribe(ctx, realtime.DoorChannel)
	defer func() { _ = subscription.Close() }()
	log := logger.FromContext(ctx)
	log.Info("sse door opened")
	messages := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case message, open := <-messages:
			if !open {
				log.Warn("sse door closed")
				return
			}
			h.deliver(message.Payload)
		}
	}
}

func (h *Hub) deliver(payload string) {
	signal, ok := realtime.ParseSignal(payload)
	if !ok {
		return
	}
	if signal.RequestID != "" {
		h.SignalPosition(signal.UserID, signal.RequestID)
		return
	}
	h.Signal(signal.UserID)
}
