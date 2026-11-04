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
	_, err := p.PublishBatch(ctx, []Message{msg})
	return err
}

// PublishBatch sends every message in order and then waits for the
// confirmations, so one batch costs one round trip to the broker instead of
// one per message. It returns how many messages, from the start of the batch,
// the broker confirmed; messages after them were not confirmed.
func (p *Publisher) PublishBatch(ctx context.Context, msgs []Message) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch, err := p.channel()
	if err != nil {
		return 0, err
	}

	type sent struct {
		confirmation *amqp.DeferredConfirmation
		message      *Message
	}
	pending := make([]sent, 0, len(msgs))
	var sendErr error
	for i := range msgs {
		confirmation, err := p.send(ctx, ch, &msgs[i])
		if err != nil {
			sendErr = err
			break
		}
		pending = append(pending, sent{confirmation: confirmation, message: &msgs[i]})
	}

	confirmed := 0
	for _, item := range pending {
		acked, err := item.confirmation.WaitContext(ctx)
		if err != nil {
			p.reset()
			return confirmed, fmt.Errorf("wait confirm %s: %w", item.message.RoutingKey, err)
		}
		if !acked {
			return confirmed, errors.New("broker nacked message " + item.message.ID)
		}
		confirmed++
	}
	if sendErr != nil {
		p.reset()
		return confirmed, sendErr
	}
	return confirmed, nil
}

func (p *Publisher) send(ctx context.Context, ch *amqp.Channel, msg *Message) (*amqp.DeferredConfirmation, error) {
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
		return nil, fmt.Errorf("publish %s: %w", msg.RoutingKey, err)
	}
	return dc, nil
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
