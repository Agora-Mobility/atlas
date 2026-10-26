// Package queue provides generic RabbitMQ building blocks: a connection
// manager with lazy reconnection, a confirmed publisher, a consumer with
// retry/DLQ topology and a Redis deduplicator.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/utils"
)

// ErrClosed is returned after Close.
var ErrClosed = errors.New("queue: manager closed")

// Manager owns the AMQP connection. When the connection drops, the next call
// to Channel dials again, so publishers and consumers recover by simply
// asking for a new channel.
type Manager struct {
	url      string
	exchange string

	mu     sync.Mutex
	conn   *amqp.Connection
	closed bool
}

// NewManager connects (with retries) and declares the topic exchange.
func NewManager(ctx context.Context, url, exchange string) (*Manager, error) {
	m := &Manager{url: url, exchange: exchange}

	const attempts = 5
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if err = m.declareExchange(); err == nil {
			return m, nil
		}
		logger.Warn(ctx, "rabbitmq not ready, retrying", zap.Int("attempt", attempt+1), zap.Error(err))
		if !utils.Sleep(ctx, utils.Backoff(attempt, 500*time.Millisecond, 5*time.Second)) {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("connect rabbitmq: %w", err)
}

func (m *Manager) Exchange() string { return m.exchange }

func (m *Manager) declareExchange() error {
	ch, err := m.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	return ch.ExchangeDeclare(m.exchange, amqp.ExchangeTopic, true, false, false, false, nil)
}

// Channel opens a new channel, reconnecting first if needed.
func (m *Manager) Channel() (*amqp.Channel, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil, ErrClosed
	}
	if m.conn == nil || m.conn.IsClosed() {
		conn, err := amqp.DialConfig(m.url, amqp.Config{
			Heartbeat:  10 * time.Second,
			Properties: amqp.Table{"connection_name": "atlas"},
		})
		if err != nil {
			return nil, fmt.Errorf("dial rabbitmq: %w", err)
		}
		m.conn = conn
	}
	ch, err := m.conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("open channel: %w", err)
	}
	return ch, nil
}

// Healthy reports whether the connection is open.
func (m *Manager) Healthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.closed && m.conn != nil && !m.conn.IsClosed()
}

// Close closes the connection; further Channel calls fail with ErrClosed.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.conn != nil && !m.conn.IsClosed() {
		return m.conn.Close()
	}
	return nil
}
