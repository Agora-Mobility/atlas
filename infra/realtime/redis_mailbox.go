package realtime

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	DoorChannel     = "rt:door"
	doorMailbox     = "m"
	doorPosition    = "p"
	doorSeparator   = "|"
	positionTTL     = 60 * time.Second
	fieldEventID    = "event_id"
	fieldType       = "type"
	fieldClass      = "class"
	fieldData       = "data"
	fieldExpiresAt  = "expires_at"
	millisecondsTag = "-0"
)

var appendEntry = goredis.NewScript(`
if redis.call('SET', KEYS[2], '1', 'NX', 'PX', ARGV[1]) == false then
  return 0
end
redis.call('XADD', KEYS[1], 'MAXLEN', '~', ARGV[2], '*',
  'event_id', ARGV[4], 'type', ARGV[5], 'class', ARGV[6], 'data', ARGV[7], 'expires_at', ARGV[8])
redis.call('XTRIM', KEYS[1], 'MINID', '~', ARGV[3])
redis.call('PEXPIRE', KEYS[1], ARGV[1])
redis.call('PUBLISH', ARGV[9], ARGV[10])
return 1`)

type Mailbox struct {
	rdb        *goredis.Client
	maxEntries int64
	retention  time.Duration
	now        func() time.Time
}

var (
	_ services.IMailbox      = (*Mailbox)(nil)
	_ services.IPositionFeed = (*Mailbox)(nil)
)

func NewMailbox(rdb *goredis.Client, maxEntries int64, retention time.Duration) *Mailbox {
	return &Mailbox{rdb: rdb, maxEntries: maxEntries, retention: retention, now: time.Now}
}

func boxKey(userID string) string {
	return "rt:box:" + userID
}

func seenKey(userID, eventID string) string {
	return "rt:seen:" + userID + ":" + eventID
}

func positionKey(requestID string) string {
	return "rt:pos:" + requestID
}

func (m *Mailbox) Deliver(ctx context.Context, userID string, message services.MailboxMessage) error {
	var expiresAt int64
	if message.ExpiresAt != nil {
		expiresAt = message.ExpiresAt.UnixMilli()
	}
	minID := strconv.FormatInt(m.now().Add(-m.retention).UnixMilli(), 10) + millisecondsTag
	err := appendEntry.Run(ctx, m.rdb, []string{boxKey(userID), seenKey(userID, message.EventID)},
		m.retention.Milliseconds(), m.maxEntries, minID,
		message.EventID, message.Type, string(message.Class), message.Data, expiresAt, DoorChannel, doorMailbox+doorSeparator+userID).Err()
	if err != nil {
		return fmt.Errorf("append to mailbox: %w", err)
	}
	return nil
}

func (m *Mailbox) Read(ctx context.Context, userID, afterID string, count int64) ([]services.MailboxEntry, error) {
	start := "(" + afterID
	if afterID == "" {
		start = "-"
	}
	messages, err := m.rdb.XRangeN(ctx, boxKey(userID), start, "+", count).Result()
	if err != nil {
		return nil, fmt.Errorf("read mailbox: %w", err)
	}
	entries := make([]services.MailboxEntry, 0, len(messages))
	for _, message := range messages {
		entries = append(entries, toEntry(message))
	}
	return entries, nil
}

func (m *Mailbox) CheckCursor(ctx context.Context, userID, cursor string) error {
	if cursor == "" {
		return nil
	}
	if !isValidStreamID(cursor) {
		return services.ErrMailboxCursorLost
	}
	first, err := m.rdb.XRangeN(ctx, boxKey(userID), "-", "+", 1).Result()
	if err != nil {
		return fmt.Errorf("check cursor: %w", err)
	}
	if len(first) == 0 {
		return services.ErrMailboxCursorLost
	}
	if compareStreamIDs(cursor, first[0].ID) < 0 {
		return services.ErrMailboxCursorLost
	}
	return nil
}

func (m *Mailbox) PublishPosition(ctx context.Context, userID, requestID string, payload []byte) error {
	pipe := m.rdb.Pipeline()
	pipe.Set(ctx, positionKey(requestID), payload, positionTTL)
	pipe.Publish(ctx, DoorChannel, doorPosition+doorSeparator+userID+doorSeparator+requestID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("publish position: %w", err)
	}
	return nil
}

func (m *Mailbox) LastPosition(ctx context.Context, requestID string) ([]byte, error) {
	payload, err := m.rdb.Get(ctx, positionKey(requestID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("last position: %w", err)
	}
	return payload, nil
}

func toEntry(message goredis.XMessage) services.MailboxEntry {
	entry := services.MailboxEntry{ID: message.ID}
	entry.EventID = stringValue(message.Values[fieldEventID])
	entry.Type = stringValue(message.Values[fieldType])
	entry.Class = services.MessageClass(stringValue(message.Values[fieldClass]))
	entry.Data = []byte(stringValue(message.Values[fieldData]))
	if millis, err := strconv.ParseInt(stringValue(message.Values[fieldExpiresAt]), 10, 64); err == nil && millis > 0 {
		expiresAt := time.UnixMilli(millis)
		entry.ExpiresAt = &expiresAt
	}
	return entry
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func isValidStreamID(id string) bool {
	millis, sequence, found := strings.Cut(id, "-")
	if !found {
		return false
	}
	_, millisErr := strconv.ParseUint(millis, 10, 64)
	_, sequenceErr := strconv.ParseUint(sequence, 10, 64)
	return millisErr == nil && sequenceErr == nil
}

func compareStreamIDs(a, b string) int {
	aMillis, aSequence, _ := strings.Cut(a, "-")
	bMillis, bSequence, _ := strings.Cut(b, "-")
	am, _ := strconv.ParseUint(aMillis, 10, 64)
	bm, _ := strconv.ParseUint(bMillis, 10, 64)
	if am != bm {
		if am < bm {
			return -1
		}
		return 1
	}
	as, _ := strconv.ParseUint(aSequence, 10, 64)
	bs, _ := strconv.ParseUint(bSequence, 10, 64)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

type Signal struct {
	UserID    string
	RequestID string
}

func ParseSignal(payload string) (Signal, bool) {
	kind, rest, found := strings.Cut(payload, doorSeparator)
	if !found || rest == "" {
		return Signal{}, false
	}
	switch kind {
	case doorMailbox:
		return Signal{UserID: rest}, true
	case doorPosition:
		userID, requestID, ok := strings.Cut(rest, doorSeparator)
		return Signal{UserID: userID, RequestID: requestID}, ok
	default:
		return Signal{}, false
	}
}
