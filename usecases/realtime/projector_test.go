package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/realtime"
)

type recordingMailbox struct {
	appended map[string][]services.MailboxMessage
	signaled []string
	failOn   string
}

func newMailbox() *recordingMailbox {
	return &recordingMailbox{appended: map[string][]services.MailboxMessage{}}
}

func (m *recordingMailbox) Append(_ context.Context, userID string, message services.MailboxMessage) error {
	if userID == m.failOn {
		return errors.New("redis down")
	}
	m.appended[userID] = append(m.appended[userID], message)
	return nil
}

func (m *recordingMailbox) Read(context.Context, string, string, int64) ([]services.MailboxEntry, error) {
	return nil, nil
}
func (m *recordingMailbox) CheckCursor(context.Context, string, string) error { return nil }
func (m *recordingMailbox) Signal(_ context.Context, userID string) error {
	m.signaled = append(m.signaled, userID)
	return nil
}

func envelope(t *testing.T, event events.Event) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(event, "corr")
	require.NoError(t, err)
	return env
}

func offerEvent(expiresAt time.Time) events.RequestOfferV1 {
	distance := 850
	return events.RequestOfferV1{
		Type: events.OfferCreated, OfferID: uuid.New(), RequestID: uuid.New(), ProviderID: uuid.New(), CustomerID: uuid.New(), Category: "taxi", Version: 3,
		ExpiresAt: expiresAt, DistanceM: &distance, Pickup: &events.RequestPoint{Lat: -8.84, Lng: 13.23, Reference: strings.Repeat("r", 140)},
	}
}

func TestOfferGoesToTheProviderAsACriticalMessageWithItsDeadline(t *testing.T) {
	mailbox := newMailbox()
	event := offerEvent(time.Now().Add(20 * time.Second))

	require.NoError(t, realtime.NewProjector(mailbox).Handle(context.Background(), envelope(t, event)))

	messages := mailbox.appended[event.ProviderID.String()]
	require.Len(t, messages, 1)
	assert.Equal(t, "request.offer", messages[0].Type)
	assert.Equal(t, services.ClassCritical, messages[0].Class)
	assert.NotNil(t, messages[0].ExpiresAt)
	assert.Less(t, len(messages[0].Data), 2048, "events stay under the 2 KB budget")
	assert.Empty(t, mailbox.appended[event.CustomerID.String()], "the customer never sees offers")
	assert.Equal(t, []string{event.ProviderID.String()}, mailbox.signaled)

	var body map[string]any
	require.NoError(t, json.Unmarshal(messages[0].Data, &body))
	assert.EqualValues(t, 3, body["request_version"])
	assert.Equal(t, "critical", body["class"])
	assert.NotEmpty(t, body["event_id"])
}

func TestExpiredOfferIsDroppedSoARetryDoesNotDeliverIt(t *testing.T) {
	mailbox := newMailbox()
	event := offerEvent(time.Now().Add(-time.Second))

	require.NoError(t, realtime.NewProjector(mailbox).Handle(context.Background(), envelope(t, event)))

	assert.Empty(t, mailbox.appended)
}

func TestTransitionReachesBothPartiesOnceAssigned(t *testing.T) {
	mailbox := newMailbox()
	provider := uuid.New()
	event := events.RequestTransitionV1{Type: events.RequestAccepted, RequestID: uuid.New(), CustomerID: uuid.New(), ProviderID: &provider, Category: "taxi", Version: 4, To: "accepted"}

	require.NoError(t, realtime.NewProjector(mailbox).Handle(context.Background(), envelope(t, event)))

	assert.Len(t, mailbox.appended[event.CustomerID.String()], 1)
	assert.Len(t, mailbox.appended[provider.String()], 1)
	assert.Equal(t, services.ClassState, mailbox.appended[provider.String()][0].Class)
}

func TestAppendFailureSurfacesForRetryWithStableEventID(t *testing.T) {
	mailbox := newMailbox()
	provider := uuid.New()
	event := events.RequestTransitionV1{Type: events.RequestAccepted, RequestID: uuid.New(), CustomerID: uuid.New(), ProviderID: &provider, To: "accepted"}
	env := envelope(t, event)
	mailbox.failOn = provider.String()
	projector := realtime.NewProjector(mailbox)

	require.Error(t, projector.Handle(context.Background(), env))
	mailbox.failOn = ""
	require.NoError(t, projector.Handle(context.Background(), env))

	customer := mailbox.appended[event.CustomerID.String()]
	require.Len(t, customer, 2)
	assert.Equal(t, customer[0].EventID, customer[1].EventID, "the mailbox deduplicates by event id per recipient")
}

func TestOfferCancelledIsCritical(t *testing.T) {
	mailbox := newMailbox()
	event := offerEvent(time.Now().Add(time.Minute))
	event.Type = events.OfferCancelled

	require.NoError(t, realtime.NewProjector(mailbox).Handle(context.Background(), envelope(t, event)))

	message := mailbox.appended[event.ProviderID.String()][0]
	assert.Equal(t, "request.offer_cancelled", message.Type)
	assert.Equal(t, services.ClassCritical, message.Class)
}

func TestEveryBindingIsVersioned(t *testing.T) {
	for _, key := range realtime.BindingKeys() {
		assert.True(t, strings.HasSuffix(key, ".v1"), key)
	}
}
