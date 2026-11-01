package notifications_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
)

type fakeDevices struct {
	mu      sync.Mutex
	tokens  map[uuid.UUID][]string
	phones  map[uuid.UUID]string
	removed []string
	records []repository.NotificationRecord
}

func (d *fakeDevices) Register(context.Context, uuid.UUID, string, string) error { return nil }
func (d *fakeDevices) Unregister(context.Context, uuid.UUID, string) error       { return nil }
func (d *fakeDevices) Remove(_ context.Context, token string) error {
	d.removed = append(d.removed, token)
	return nil
}
func (d *fakeDevices) Tokens(_ context.Context, userID uuid.UUID) ([]string, error) {
	return d.tokens[userID], nil
}
func (d *fakeDevices) Contact(_ context.Context, userID uuid.UUID) (string, string, error) {
	return "Nome", d.phones[userID], nil
}
func (d *fakeDevices) RecordNotification(_ context.Context, record repository.NotificationRecord) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.records = append(d.records, record)
	return nil
}

type fakePush struct {
	sent []services.PushMessage
	err  error
}

func (p *fakePush) Send(_ context.Context, message services.PushMessage) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	p.sent = append(p.sent, message)
	return "msg-1", nil
}

type fakeSMS struct{ sent map[string]string }

func (s *fakeSMS) Send(_ context.Context, phone, message string) error {
	s.sent[phone] = message
	return nil
}

func requestEnvelope(t *testing.T, event events.Event) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(event, "")
	require.NoError(t, err)
	return env
}

func TestOfferChannelsFollowThePolicy(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, push, sms, "agora://app", "")
	event := events.RequestOfferV1{
		Type: events.OfferCreated, OfferID: uuid.New(), RequestID: uuid.New(), ProviderID: provider, Category: "maintenance",
		ExpiresAt: time.Now().Add(10 * time.Minute), Channels: []string{"fcm", "sms", "sse"},
	}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	require.Len(t, push.sent, 1)
	assert.True(t, push.sent[0].HighPriority)
	assert.Equal(t, event.OfferID.String(), push.sent[0].Data["offer_id"])
	assert.Contains(t, sms.sent["+244900000000"], "agora://app/offers/"+event.OfferID.String())
}

func TestTaxiOfferStartsOnSSEOnly(t *testing.T) {
	provider := uuid.New()
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(&fakeDevices{}, push, sms, "agora://app", "")
	event := events.RequestOfferV1{Type: events.OfferCreated, OfferID: uuid.New(), ProviderID: provider, ExpiresAt: time.Now().Add(time.Minute), Channels: []string{"sse"}}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Empty(t, push.sent)
	assert.Empty(t, sms.sent)
}

func TestAnOfferRetriedAfterItsDeadlineIsDropped(t *testing.T) {
	provider := uuid.New()
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	notifier := notifications.NewRequestNotifier(devices, push, sms, "agora://app", "")
	event := events.RequestOfferV1{Type: events.OfferCreated, OfferID: uuid.New(), ProviderID: provider, ExpiresAt: time.Now().Add(-time.Second), Channels: []string{"fcm", "sms"}}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Empty(t, push.sent)
	assert.Empty(t, sms.sent)
}

func TestFallbackGoesThroughTheRequestedChannel(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, push, sms, "agora://app", "")
	notice := dispatch.OfferNotice{OfferID: uuid.New(), ProviderID: provider, Category: "taxi", ExpiresIn: 17}

	require.NoError(t, notifier.SendOffer(context.Background(), "fcm", notice))
	assert.Len(t, push.sent, 1)
	assert.Equal(t, 17, push.sent[0].TTLSeconds)
	assert.Empty(t, sms.sent)

	require.NoError(t, notifier.SendOffer(context.Background(), "sms", notice))
	assert.Contains(t, sms.sent["+244900000000"], "17 s")
}

func TestInvalidTokenIsRemoved(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"gone"}}}
	notifier := notifications.NewRequestNotifier(devices, &fakePush{err: services.ErrPushTokenInvalid}, &fakeSMS{}, "", "")

	require.NoError(t, notifier.SendOffer(context.Background(), "fcm", dispatch.OfferNotice{ProviderID: provider}))

	assert.Equal(t, []string{"gone"}, devices.removed)
	assert.Equal(t, "failed", devices.records[0].Status)
}

func TestPushOutageSurfacesForRetry(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}}
	notifier := notifications.NewRequestNotifier(devices, &fakePush{err: errors.New("fcm down")}, &fakeSMS{}, "", "")

	assert.Error(t, notifier.SendOffer(context.Background(), "fcm", dispatch.OfferNotice{ProviderID: provider}))
}

func TestAcceptedConfirmsBySMSWhenThePolicyAsksAndTellsThePassenger(t *testing.T) {
	customer := uuid.New()
	devices := &fakeDevices{phones: map[uuid.UUID]string{customer: "+244911111111"}}
	sms := &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, &fakePush{}, sms, "", "")
	event := events.RequestTransitionV1{Type: events.RequestAccepted, RequestID: uuid.New(), CustomerID: customer, To: "accepted", SMSOnAccept: true, PassengerPhone: "+244922222222"}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Contains(t, sms.sent["+244911111111"], "aceite")
	assert.Contains(t, sms.sent["+244922222222"], "táxi")
}

func TestOperatorAlertUsesTheConfiguredPhone(t *testing.T) {
	sms := &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(&fakeDevices{}, &fakePush{}, sms, "", "+244933333333")
	event := events.OperatorAlertedV1{RequestID: uuid.New(), Category: "towing", Reason: "no_provider"}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Contains(t, sms.sent["+244933333333"], "no_provider")
}
