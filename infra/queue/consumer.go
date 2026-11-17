package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/infra/observability/prometheus"
	"github.com/Agora-Mobility/atlas/infra/observability/tracing"
	"github.com/Agora-Mobility/atlas/utils"
)

const (
	headerRetryCount = "x-retry-count"
	headerLastError  = "x-last-error"
)

// Handler processes one event. Returning an error schedules a retry.
type Handler func(ctx context.Context, env events.Envelope) error

// ConsumerConfig describes one consumer. Queue names follow
// "<module>.<purpose>", e.g. "notifications.example-created".
type ConsumerConfig struct {
	Queue       string
	BindingKeys []string // routing keys, wildcards allowed: "example.*.v1"
	Handler     Handler
	Prefetch    int
	MaxRetries  int
	RetryDelay  time.Duration
	Workers     int // events of one aggregate always go to the same worker, in order
}

// Consumer reads from a quorum queue with manual acks. Failed messages go to
// "<queue>.retry" with a per-message TTL and dead-letter back to the main
// queue when it expires; after MaxRetries they go to "<queue>.dlq".
type Consumer struct {
	mgr   *Manager
	dedup *Deduplicator
	cfg   ConsumerConfig
}

func NewConsumer(mgr *Manager, dedup *Deduplicator, cfg ConsumerConfig) *Consumer {
	return &Consumer{mgr: mgr, dedup: dedup, cfg: cfg}
}

func (c *Consumer) retryQueue() string { return c.cfg.Queue + ".retry" }
func (c *Consumer) dlq() string        { return c.cfg.Queue + ".dlq" }

// Run consumes until ctx is canceled, reconnecting with backoff when the
// channel or connection drops.
func (c *Consumer) Run(ctx context.Context) {
	log := logger.FromContext(ctx).With(zap.String("queue", c.cfg.Queue))
	log.Info("consumer started")

	for attempt := 0; ctx.Err() == nil; {
		err := c.consume(ctx)
		if ctx.Err() != nil {
			break
		}
		log.Warn("consumer interrupted, reconnecting", zap.Error(err), zap.Int("attempt", attempt+1))
		if !utils.Sleep(ctx, utils.Backoff(attempt, time.Second, 30*time.Second)) {
			break
		}
		attempt++
	}
	log.Info("consumer stopped")
}

func (c *Consumer) declareTopology(ch *amqp.Channel) error {
	quorum := amqp.Table{"x-queue-type": "quorum"}

	if _, err := ch.QueueDeclare(c.cfg.Queue, true, false, false, false, quorum); err != nil {
		return fmt.Errorf("declare %s: %w", c.cfg.Queue, err)
	}
	// The delay is set per message (Expiration), not as x-message-ttl, so
	// changing RABBITMQ_RETRY_DELAY never conflicts with the declared queue.
	if _, err := ch.QueueDeclare(c.retryQueue(), true, false, false, false, amqp.Table{
		"x-queue-type":              "quorum",
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": c.cfg.Queue,
	}); err != nil {
		return fmt.Errorf("declare %s: %w", c.retryQueue(), err)
	}
	if _, err := ch.QueueDeclare(c.dlq(), true, false, false, false, quorum); err != nil {
		return fmt.Errorf("declare %s: %w", c.dlq(), err)
	}
	for _, key := range c.cfg.BindingKeys {
		if err := ch.QueueBind(c.cfg.Queue, key, c.mgr.Exchange(), false, nil); err != nil {
			return fmt.Errorf("bind %s to %s: %w", c.cfg.Queue, key, err)
		}
	}
	return nil
}

// EnsureTopology declares the queues and bindings. The relay must only publish
// after this: a message sent to the exchange before its queue is bound is
// dropped by the broker and never comes back.
func (c *Consumer) EnsureTopology() error {
	ch, err := c.mgr.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()
	return c.declareTopology(ch)
}

func (c *Consumer) consume(ctx context.Context) error {
	ch, err := c.mgr.Channel()
	if err != nil {
		return err
	}
	defer func() { _ = ch.Close() }()

	if err := c.declareTopology(ch); err != nil {
		return err
	}
	if err := ch.Qos(c.cfg.Prefetch, 0, false); err != nil {
		return fmt.Errorf("qos: %w", err)
	}

	deliveries, err := ch.ConsumeWithContext(ctx, c.cfg.Queue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume: %w", err)
	}

	workers := c.startWorkers(ctx, ch)
	defer workers.stop()

	for {
		select {
		case <-ctx.Done():
			// Closing the channel blocks while prefetched deliveries are pending.
			for d := range deliveries {
				_ = d.Nack(false, true)
			}
			return nil
		case d, ok := <-deliveries:
			if !ok {
				return errors.New("delivery channel closed")
			}
			workers.dispatch(d)
		}
	}
}

type workerPool struct {
	queues []chan amqp.Delivery
	done   sync.WaitGroup
}

func (c *Consumer) startWorkers(ctx context.Context, ch *amqp.Channel) *workerPool {
	count := max(c.cfg.Workers, 1)
	pool := &workerPool{queues: make([]chan amqp.Delivery, count)}
	for i := range pool.queues {
		queue := make(chan amqp.Delivery, 1)
		pool.queues[i] = queue
		pool.done.Go(func() {
			for d := range queue {
				c.handle(ctx, ch, d)
			}
		})
	}
	return pool
}

func (p *workerPool) dispatch(d amqp.Delivery) {
	p.queues[workerFor(d.Body, len(p.queues))] <- d
}

func (p *workerPool) stop() {
	for _, queue := range p.queues {
		close(queue)
	}
	p.done.Wait()
}

func workerFor(body []byte, workers int) int {
	if workers == 1 {
		return 0
	}
	var key struct {
		AggregateID string `json:"aggregate_id"`
	}
	_ = json.Unmarshal(body, &key)
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key.AggregateID))
	return int(hash.Sum32() % uint32(workers)) // #nosec G115 -- a small configured count
}

func (c *Consumer) handle(ctx context.Context, ch *amqp.Channel, d amqp.Delivery) {
	var env events.Envelope
	if err := json.Unmarshal(d.Body, &env); err != nil {
		logger.FromContext(ctx).Error("malformed event, sending to DLQ", zap.String("queue", c.cfg.Queue), zap.Error(err))
		c.deadLetter(ctx, ch, d, err)
		return
	}

	// Continue the trace of the code that raised the event.
	ctx, span := tracing.Tracer().Start(tracing.Extract(ctx, env.Trace), "process "+c.cfg.Queue,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.destination.name", c.cfg.Queue),
			attribute.String("messaging.message.id", env.EventID.String()),
			attribute.String("messaging.rabbitmq.destination.routing_key", d.RoutingKey),
			attribute.String("event.type", env.EventType),
			attribute.Int("messaging.retry_count", retryCount(d.Headers)),
			attribute.String(logger.CorrelationIDField, env.CorrelationID),
		),
	)
	defer span.End()

	ctx = logger.WithCorrelationID(ctx, env.CorrelationID)
	log := logger.FromContext(ctx).With(
		zap.String("queue", c.cfg.Queue),
		zap.String("event_id", env.EventID.String()),
		zap.String("event_type", env.EventType),
	)
	eventID := env.EventID.String()

	processed, err := c.dedup.IsProcessed(ctx, c.cfg.Queue, eventID)
	if err != nil {
		// Without Redis we cannot guarantee idempotency: requeue and retry.
		log.Error("dedup unavailable, requeueing", zap.Error(err))
		span.SetStatus(codes.Error, "dedup unavailable")
		_ = d.Nack(false, true)
		return
	}
	if processed {
		log.Debug("duplicate event skipped")
		prometheus.EventsConsumed.WithLabelValues(c.cfg.Queue, "duplicate").Inc()
		_ = d.Ack(false)
		return
	}

	if err := c.cfg.Handler(ctx, env); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		c.retryOrDeadLetter(ctx, ch, d, err)
		return
	}
	if err := c.dedup.MarkProcessed(ctx, c.cfg.Queue, eventID); err != nil {
		// The handler succeeded; a redelivery may run it again, which
		// handlers must tolerate anyway (at-least-once delivery).
		log.Warn("could not record processed event", zap.Error(err))
	}

	prometheus.EventsConsumed.WithLabelValues(c.cfg.Queue, "success").Inc()
	_ = d.Ack(false)
}

func (c *Consumer) retryOrDeadLetter(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, cause error) {
	retries := retryCount(d.Headers)
	if retries >= c.cfg.MaxRetries {
		logger.FromContext(ctx).Error("event exhausted retries, sending to DLQ",
			zap.String("queue", c.cfg.Queue), zap.Int("retries", retries), zap.Error(cause))
		c.deadLetter(ctx, ch, d, cause)
		return
	}

	logger.FromContext(ctx).Warn("event handler failed, scheduling retry",
		zap.String("queue", c.cfg.Queue), zap.Int("retry", retries+1), zap.Error(cause))
	if err := c.republish(ctx, ch, d, c.retryQueue(), retries+1, cause, c.cfg.RetryDelay); err != nil {
		_ = d.Nack(false, true)
		return
	}
	prometheus.EventsConsumed.WithLabelValues(c.cfg.Queue, "retry").Inc()
	_ = d.Ack(false)
}

func (c *Consumer) deadLetter(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, cause error) {
	if err := c.republish(ctx, ch, d, c.dlq(), retryCount(d.Headers), cause, 0); err != nil {
		_ = d.Nack(false, true)
		return
	}
	prometheus.EventsConsumed.WithLabelValues(c.cfg.Queue, "dlq").Inc()
	_ = d.Ack(false)
}

// republish copies the delivery to queue through the default exchange. A
// positive ttl makes the message expire (and dead-letter back) after ttl.
func (c *Consumer) republish(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, queue string, retries int, cause error, ttl time.Duration) error {
	headers := amqp.Table{}
	for k, v := range d.Headers {
		headers[k] = v
	}
	headers[headerRetryCount] = int32(retries) // #nosec G115 -- bounded by MaxRetries
	headers[headerLastError] = cause.Error()

	var expiration string
	if ttl > 0 {
		expiration = strconv.FormatInt(ttl.Milliseconds(), 10)
	}

	return ch.PublishWithContext(context.WithoutCancel(ctx), "", queue, false, false, amqp.Publishing{
		Expiration:   expiration,
		ContentType:  d.ContentType,
		DeliveryMode: amqp.Persistent,
		MessageId:    d.MessageId,
		Type:         d.Type,
		Timestamp:    d.Timestamp,
		Headers:      headers,
		Body:         d.Body,
	})
}

func retryCount(h amqp.Table) int {
	switch v := h[headerRetryCount].(type) {
	case int32:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}
