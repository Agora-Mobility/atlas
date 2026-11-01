package sse_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/sse"
)

const user = "user-1"

type fakeMailbox struct {
	mu      sync.Mutex
	entries []services.MailboxEntry
	lost    bool
}

func (m *fakeMailbox) Deliver(context.Context, string, services.MailboxMessage) error { return nil }

func (m *fakeMailbox) add(id, event string, class services.MessageClass, data string, expires *time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, services.MailboxEntry{ID: id, MailboxMessage: services.MailboxMessage{Type: event, Class: class, Data: []byte(data), ExpiresAt: expires}})
}

func (m *fakeMailbox) Read(_ context.Context, _, after string, count int64) ([]services.MailboxEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []services.MailboxEntry
	for _, entry := range m.entries {
		if entry.ID > after && int64(len(out)) < count {
			out = append(out, entry)
		}
	}
	return out, nil
}

func (m *fakeMailbox) CheckCursor(context.Context, string, string) error {
	if m.lost {
		return services.ErrMailboxCursorLost
	}
	return nil
}

type fakeFeed struct{ payload []byte }

func (f fakeFeed) PublishPosition(context.Context, string, string, []byte) error { return nil }
func (f fakeFeed) LastPosition(context.Context, string) ([]byte, error)          { return f.payload, nil }

type syncRecorder struct {
	*httptest.ResponseRecorder
	mu sync.Mutex
}

func (r *syncRecorder) Write(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ResponseRecorder.Write(b)
}

func (r *syncRecorder) Flush() {}

func (r *syncRecorder) Unwrap() http.ResponseWriter { return r.ResponseRecorder }

func (r *syncRecorder) text() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Body.String()
}

func settings() sse.Settings {
	return sse.Settings{Heartbeat: 20 * time.Millisecond, PollInterval: time.Hour, DeadAfter: 50 * time.Millisecond}
}

func serve(t *testing.T, hub *sse.Hub, mailbox services.IMailbox, feed services.IPositionFeed, session sse.Session) (*syncRecorder, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	recorder := &syncRecorder{ResponseRecorder: httptest.NewRecorder()}
	done := make(chan error, 1)
	go func() { done <- sse.NewStreamer(hub, mailbox, feed, settings()).Serve(ctx, recorder, session) }()
	return recorder, cancel, done
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	require.Eventually(t, condition, 2*time.Second, 5*time.Millisecond)
}

func TestReplaySendsLiveCriticalsFirstAndSkipsExpired(t *testing.T) {
	mailbox := &fakeMailbox{}
	future := time.Now().Add(10 * time.Second)
	past := time.Now().Add(-time.Second)
	mailbox.add("1-0", "request.status", services.ClassState, `{"n":"accepted"}`, nil)
	mailbox.add("2-0", "request.offer", services.ClassCritical, `{"n":"live-offer"}`, &future)
	mailbox.add("3-0", "request.offer", services.ClassCritical, `{"n":"dead-offer"}`, &past)

	recorder, cancel, done := serve(t, sse.NewHub(0), mailbox, fakeFeed{}, sse.Session{UserID: user})
	eventually(t, func() bool { return strings.Contains(recorder.text(), "id: 3-0") })
	cancel()
	<-done

	text := recorder.text()
	assert.NotContains(t, text, "dead-offer", "an expired offer never appears in the replay")
	assert.Less(t, strings.Index(text, "live-offer"), strings.Index(text, `"accepted"`))
	assert.Regexp(t, `"expires_in_s":(9|10),"n":"live-offer"`, text)
	assert.Contains(t, text, "id: 1-0\nevent: request.status")
	assert.Contains(t, text, "id: 3-0\nevent: control")
}

func TestLostCursorAsksForResyncAndContinuesFromTheEnd(t *testing.T) {
	mailbox := &fakeMailbox{lost: true}
	mailbox.add("1-0", "request.status", services.ClassState, `{"n":"old"}`, nil)
	hub := sse.NewHub(0)

	recorder, cancel, done := serve(t, hub, mailbox, fakeFeed{}, sse.Session{UserID: user, LastEventID: "0-1"})
	eventually(t, func() bool { return strings.Contains(recorder.text(), "event: resync") })
	assert.NotContains(t, recorder.text(), `"old"`)

	mailbox.add("2-0", "request.status", services.ClassState, `{"n":"new"}`, nil)
	eventually(t, func() bool { hub.Signal(user); return strings.Contains(recorder.text(), `"new"`) })
	cancel()
	<-done
}

func TestSignalDeliversInOrderAndCollapsedSignalsLoseNothing(t *testing.T) {
	mailbox := &fakeMailbox{}
	hub := sse.NewHub(0)
	recorder, cancel, done := serve(t, hub, mailbox, fakeFeed{}, sse.Session{UserID: user})
	eventually(t, func() bool { return hub.Subscribers() == 1 && strings.Contains(recorder.text(), "configure") })

	for _, id := range []string{"1-0", "1-1", "1-2", "1-3", "1-4"} {
		mailbox.add(id, "request.status", services.ClassState, `{"id":"`+id+`"}`, nil)
		hub.Signal(user)
		hub.Signal(user)
	}

	eventually(t, func() bool { return strings.Contains(recorder.text(), "id: 1-4") })
	text := recorder.text()
	last := -1
	for _, id := range []string{"id: 1-0", "id: 1-1", "id: 1-2", "id: 1-3", "id: 1-4"} {
		position := strings.Index(text, id)
		require.Greater(t, position, last, id)
		last = position
	}
	cancel()
	<-done
}

func TestPeriodicPollRecoversALostSignal(t *testing.T) {
	mailbox := &fakeMailbox{}
	hub := sse.NewHub(0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	recorder := &syncRecorder{ResponseRecorder: httptest.NewRecorder()}
	polling := sse.Settings{Heartbeat: time.Hour, PollInterval: 10 * time.Millisecond, DeadAfter: time.Hour}
	go func() {
		_ = sse.NewStreamer(hub, mailbox, fakeFeed{}, polling).Serve(ctx, recorder, sse.Session{UserID: user})
	}()
	eventually(t, func() bool { return hub.Subscribers() == 1 })

	mailbox.add("9-0", "request.status", services.ClassState, `{"n":"unsignaled"}`, nil)

	eventually(t, func() bool { return strings.Contains(recorder.text(), "unsignaled") })
}

func TestHeartbeatKeepsTheStreamAlive(t *testing.T) {
	recorder, cancel, done := serve(t, sse.NewHub(0), &fakeMailbox{}, fakeFeed{}, sse.Session{UserID: user})
	eventually(t, func() bool { return strings.Contains(recorder.text(), ": hb") })
	assert.Contains(t, recorder.text(), `"heartbeat_s"`)
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	cancel()
	<-done
}

func TestPositionIsLastValueOnlyWithoutACursorId(t *testing.T) {
	hub := sse.NewHub(0)
	recorder, cancel, done := serve(t, hub, &fakeMailbox{}, fakeFeed{payload: []byte(`{"lat":-8.8}`)}, sse.Session{UserID: user})
	eventually(t, func() bool { return hub.Subscribers() == 1 })

	hub.SignalPosition(user, "req-1")
	eventually(t, func() bool { return strings.Contains(recorder.text(), "event: request.position") })
	assert.NotContains(t, recorder.text(), "id: ")
	cancel()
	<-done
}

func TestActiveRequestPositionIsSentOnConnect(t *testing.T) {
	session := sse.Session{UserID: user, ActiveRequestID: func(context.Context) (string, error) { return "req-1", nil }}
	recorder, cancel, done := serve(t, sse.NewHub(0), &fakeMailbox{}, fakeFeed{payload: []byte(`{"lat":-8.8}`)}, session)
	eventually(t, func() bool { return strings.Contains(recorder.text(), "request.position") })
	cancel()
	<-done
}

func TestDrainAsksForASpreadReconnectAndCloses(t *testing.T) {
	hub := sse.NewHub(time.Minute)
	recorder, _, done := serve(t, hub, &fakeMailbox{}, fakeFeed{}, sse.Session{UserID: user})
	eventually(t, func() bool { return hub.Subscribers() == 1 })

	hub.Drain()

	require.NoError(t, <-done)
	assert.Contains(t, recorder.text(), `"action":"reconnect"`)
	assert.Contains(t, recorder.text(), `"retry_in_ms"`)
	assert.Zero(t, hub.Subscribers())
}

func TestExpiringTokenClosesTheStreamAskingForRenewal(t *testing.T) {
	session := sse.Session{UserID: user, TokenExpiresAt: time.Now().Add(time.Second)}
	recorder, _, done := serve(t, sse.NewHub(0), &fakeMailbox{}, fakeFeed{}, session)
	require.NoError(t, <-done)
	assert.Contains(t, recorder.text(), `"action":"renew_token"`)
}

type failingMailbox struct{ fakeMailbox }

func (*failingMailbox) CheckCursor(context.Context, string, string) error {
	return errors.New("redis down")
}

func TestMailboxFailureEndsTheStream(t *testing.T) {
	_, _, done := serve(t, sse.NewHub(0), &failingMailbox{}, fakeFeed{}, sse.Session{UserID: user})
	assert.Error(t, <-done)
}
