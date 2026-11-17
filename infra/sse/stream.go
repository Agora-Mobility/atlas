package sse

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/observability/businessmetrics"
)

const (
	eventControl         = "control"
	eventResync          = "resync"
	eventPosition        = "request.position"
	readBatch            = 100
	retryHintMillis      = 1000
	writeDeadline        = 30 * time.Second
	reconnectMinSeconds  = 1
	reconnectMaxSeconds  = 60
	tokenRenewalLeadTime = 30 * time.Second
	secondsPerUnit       = 1000
)

type Settings struct {
	Heartbeat    time.Duration
	PollInterval time.Duration
	DeadAfter    time.Duration
}

type Session struct {
	UserID          string
	LastEventID     string
	TokenExpiresAt  time.Time
	ActiveRequestID func(ctx context.Context) (string, error)
	Allow           func(eventType string) bool
}

func (s Session) allows(eventType string) bool {
	return s.Allow == nil || s.Allow(eventType)
}

type Streamer struct {
	hub      *Hub
	mailbox  services.IMailbox
	feed     services.IPositionFeed
	settings Settings
	now      func() time.Time
}

func NewStreamer(hub *Hub, mailbox services.IMailbox, feed services.IPositionFeed, settings Settings) *Streamer {
	return &Streamer{hub: hub, mailbox: mailbox, feed: feed, settings: settings, now: time.Now}
}

type streamWriter struct {
	out        io.Writer
	controller *http.ResponseController
	buffer     bytes.Buffer
}

func (w *streamWriter) write(id, event string, data []byte) {
	if id != "" {
		fmt.Fprintf(&w.buffer, "id: %s\n", id)
	}
	if event != "" {
		fmt.Fprintf(&w.buffer, "event: %s\n", event)
	}
	fmt.Fprintf(&w.buffer, "data: %s\n\n", data)
}

func (w *streamWriter) comment(text string) {
	fmt.Fprintf(&w.buffer, ": %s\n\n", text)
}

func (w *streamWriter) flush() error {
	if w.buffer.Len() == 0 {
		return nil
	}
	businessmetrics.SSEBytes.Add(float64(w.buffer.Len()))
	if err := w.controller.SetWriteDeadline(time.Now().Add(writeDeadline)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return fmt.Errorf("set write deadline: %w", err)
	}
	if _, err := w.out.Write(w.buffer.Bytes()); err != nil {
		return fmt.Errorf("write stream: %w", err)
	}
	w.buffer.Reset()
	if err := w.controller.Flush(); err != nil {
		return fmt.Errorf("flush stream: %w", err)
	}
	return nil
}

func (s *Streamer) Serve(ctx context.Context, response http.ResponseWriter, session Session) error {
	headers := response.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache, no-transform")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")
	response.WriteHeader(http.StatusOK)

	connection, leave := s.hub.Subscribe(session.UserID)
	defer leave()
	businessmetrics.SSEConnections.Inc()
	defer businessmetrics.SSEConnections.Dec()

	writer := &streamWriter{out: response, controller: http.NewResponseController(response)}
	fmt.Fprintf(&writer.buffer, "retry: %d\n\n", retryHintMillis)
	writer.write("", eventControl, s.controlMessage("configure", nil))

	cursor, err := s.open(ctx, writer, session)
	if err != nil {
		return err
	}
	if err := writer.flush(); err != nil {
		return err
	}

	heartbeat := time.NewTicker(s.settings.Heartbeat)
	defer heartbeat.Stop()
	poll := time.NewTicker(s.settings.PollInterval)
	defer poll.Stop()
	expiry := s.tokenExpiry(session)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.hub.Draining():
			writer.write("", eventControl, s.reconnectMessage())
			return writer.flush()
		case <-expiry:
			writer.write("", eventControl, s.controlMessage("renew_token", nil))
			return writer.flush()
		case <-heartbeat.C:
			writer.comment("hb")
		case <-poll.C:
			cursor, err = s.deliver(ctx, writer, session, cursor)
			if err != nil {
				return err
			}
		case <-connection.Wake():
			if requestID := connection.TakePositionRequest(); requestID != "" {
				if err := s.writePosition(ctx, writer, requestID); err != nil {
					return err
				}
			}
			cursor, err = s.deliver(ctx, writer, session, cursor)
			if err != nil {
				return err
			}
		}
		if err := writer.flush(); err != nil {
			return err
		}
	}
}

func (s *Streamer) tokenExpiry(session Session) <-chan time.Time {
	if session.TokenExpiresAt.IsZero() {
		return nil
	}
	wait := session.TokenExpiresAt.Add(-tokenRenewalLeadTime).Sub(s.now())
	if wait < 0 {
		wait = 0
	}
	return time.After(wait)
}

func (s *Streamer) open(ctx context.Context, writer *streamWriter, session Session) (string, error) {
	cursor := session.LastEventID
	err := s.mailbox.CheckCursor(ctx, session.UserID, cursor)
	switch {
	case errors.Is(err, services.ErrMailboxCursorLost):
		businessmetrics.SSEReconnects.WithLabelValues("resync").Inc()
		writer.write("", eventResync, []byte(`{"class":"control","reason":"cursor_lost"}`))
		cursor, err = s.skipToEnd(ctx, session.UserID)
		if err != nil {
			return "", err
		}
	case err != nil:
		return "", err
	default:
		mode := "fresh"
		if cursor != "" {
			mode = "replay"
		}
		businessmetrics.SSEReconnects.WithLabelValues(mode).Inc()
		cursor, err = s.replay(ctx, writer, session, cursor)
		if err != nil {
			return "", err
		}
	}
	if session.ActiveRequestID != nil {
		requestID, err := session.ActiveRequestID(ctx)
		if err != nil {
			return "", err
		}
		if requestID != "" {
			if err := s.writePosition(ctx, writer, requestID); err != nil {
				return "", err
			}
		}
	}
	return cursor, nil
}

func (s *Streamer) skipToEnd(ctx context.Context, userID string) (string, error) {
	cursor := ""
	for {
		entries, err := s.mailbox.Read(ctx, userID, cursor, readBatch)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return cursor, nil
		}
		cursor = entries[len(entries)-1].ID
	}
}

func (s *Streamer) replay(ctx context.Context, writer *streamWriter, session Session, cursor string) (string, error) {
	var pending []services.MailboxEntry
	for {
		entries, err := s.mailbox.Read(ctx, session.UserID, cursor, readBatch)
		if err != nil {
			return cursor, err
		}
		if len(entries) == 0 {
			break
		}
		pending = append(pending, entries...)
		cursor = entries[len(entries)-1].ID
	}
	if len(pending) == 0 {
		return cursor, nil
	}

	now := s.now()
	for i := range pending {
		if pending[i].Class == services.ClassCritical && !isExpired(&pending[i], now) && session.allows(pending[i].Type) {
			writer.write("", pending[i].Type, withRemaining(&pending[i], now))
		}
	}
	lastDelivered := ""
	for i := range pending {
		if pending[i].Class == services.ClassCritical || isExpired(&pending[i], now) || !session.allows(pending[i].Type) {
			continue
		}
		writer.write(pending[i].ID, pending[i].Type, withRemaining(&pending[i], now))
		lastDelivered = pending[i].ID
	}
	if lastDelivered != cursor {
		writer.write(cursor, eventControl, s.controlMessage("cursor", nil))
	}
	return cursor, nil
}

func (s *Streamer) deliver(ctx context.Context, writer *streamWriter, session Session, cursor string) (string, error) {
	for {
		entries, err := s.mailbox.Read(ctx, session.UserID, cursor, readBatch)
		if err != nil {
			return cursor, err
		}
		if len(entries) == 0 {
			return cursor, nil
		}
		now := s.now()
		for i := range entries {
			cursor = entries[i].ID
			if isExpired(&entries[i], now) || !session.allows(entries[i].Type) {
				continue
			}
			writer.write(entries[i].ID, entries[i].Type, withRemaining(&entries[i], now))
		}
		if err := writer.flush(); err != nil {
			return cursor, err
		}
	}
}

func (s *Streamer) writePosition(ctx context.Context, writer *streamWriter, requestID string) error {
	payload, err := s.feed.LastPosition(ctx, requestID)
	if err != nil {
		return err
	}
	if payload != nil {
		writer.write("", eventPosition, payload)
	}
	return nil
}

func (s *Streamer) controlMessage(action string, extra map[string]any) []byte {
	body := map[string]any{
		"class":           services.ClassControl,
		"action":          action,
		"heartbeat_s":     int(s.settings.Heartbeat.Seconds()),
		"dead_after_s":    int(s.settings.DeadAfter.Seconds()),
		"reconnect_min_s": reconnectMinSeconds,
		"reconnect_max_s": reconnectMaxSeconds,
	}
	for key, value := range extra {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return []byte(`{"class":"control","action":"` + action + `"}`)
	}
	return encoded
}

func (s *Streamer) reconnectMessage() []byte {
	spread := s.hub.ReconnectSpread()
	delay := time.Duration(0)
	if spread > 0 {
		delay = rand.N(spread) // #nosec G404 -- jitter, not a secret
	}
	return s.controlMessage("reconnect", map[string]any{"retry_in_ms": delay.Milliseconds()})
}

func isExpired(entry *services.MailboxEntry, now time.Time) bool {
	return entry.ExpiresAt != nil && !now.Before(*entry.ExpiresAt)
}

func withRemaining(entry *services.MailboxEntry, now time.Time) []byte {
	if entry.ExpiresAt == nil || len(entry.Data) < 2 || entry.Data[0] != '{' {
		return entry.Data
	}
	remaining := int(entry.ExpiresAt.Sub(now).Milliseconds() / secondsPerUnit)
	prefix := `{"expires_in_s":` + strconv.Itoa(remaining)
	if entry.Data[1] == '}' {
		return []byte(prefix + "}")
	}
	return append([]byte(prefix+","), entry.Data[1:]...)
}
