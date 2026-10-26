package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const headerCorrelationID = "correlation_id"

// Message is a ready-to-publish event. Body is the JSON envelope.
type Message struct {
	ID            string
	Type          string
	RoutingKey    string
	CorrelationID string
	Body          []byte
}

// Publisher publishes persistent messages to the exchange and waits for the
// broker confirmation of each one. It is safe for concurrent use.
type Publisher struct {
	mgr *Manager

	mu sync.Mutex
	ch *amqp.Channel
}

func NewPublisher(mgr *Manager) *Publisher {
	return &Publisher{mgr: mgr}
}

// Publish returns nil only after the broker confirmed the message.
func (p *Publisher) Publish(ctx context.Context, msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch, err := p.channel()
	if err != nil {
		return err
	}

	headers := amqp.Table{}
	if msg.CorrelationID != "" {
		headers[headerCorrelationID] = msg.CorrelationID
	}

	dc, err := ch.PublishWithDeferredConfirmWithContext(ctx, p.mgr.Exchange(), msg.RoutingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		MessageId:    msg.ID,
		Type:         msg.Type,
		Timestamp:    time.Now().UTC(),
		Headers:      headers,
		Body:         msg.Body,
	})
	if err != nil {
		p.reset()
		return fmt.Errorf("publish %s: %w", msg.RoutingKey, err)
	}

	acked, err := dc.WaitContext(ctx)
	if err != nil {
		p.reset()
		return fmt.Errorf("wait confirm %s: %w", msg.RoutingKey, err)
	}
	if !acked {
		return errors.New("broker nacked message " + msg.ID)
	}
	return nil
}

// channel returns the confirm-mode channel, opening a new one if needed.
func (p *Publisher) channel() (*amqp.Channel, error) {
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	ch, err := p.mgr.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	p.ch = ch
	return ch, nil
}

func (p *Publisher) reset() {
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
}

// Close closes the publishing channel.
func (p *Publisher) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reset()
}
