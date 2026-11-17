package notifications_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
)

type fakeNotices struct{ repository.IRequestRepository }

func (fakeNotices) PassengerNotice(context.Context, uuid.UUID) (*repository.PassengerNotice, error) {
	return &repository.PassengerNotice{Template: "AGORA: reboque de {provider}, matrícula {plate}. {link}", ProviderName: "João", VehiclePlate: "LD-1"}, nil
}

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
func (d *fakeDevices) Delivered(_ context.Context, key string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, record := range d.records {
		if key != "" && record.DeliveryKey == key && record.Status == "sent" {
			return true, nil
		}
	}
	return false, nil
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

type fakeSMS struct {
	sent  map[string]string
	count int
}

func (s *fakeSMS) Send(_ context.Context, phone, message string) error {
	s.sent[phone] = message
	s.count++
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
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, sms, "agora://app", "")
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
	notifier := notifications.NewRequestNotifier(&fakeDevices{}, fakeNotices{}, push, sms, "agora://app", "")
	event := events.RequestOfferV1{Type: events.OfferCreated, OfferID: uuid.New(), ProviderID: provider, ExpiresAt: time.Now().Add(time.Minute), Channels: []string{"sse"}}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Empty(t, push.sent)
	assert.Empty(t, sms.sent)
}

func TestAnOfferRetriedAfterItsDeadlineIsDropped(t *testing.T) {
	provider := uuid.New()
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, sms, "agora://app", "")
	event := events.RequestOfferV1{Type: events.OfferCreated, OfferID: uuid.New(), ProviderID: provider, ExpiresAt: time.Now().Add(-time.Second), Channels: []string{"fcm", "sms"}}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Empty(t, push.sent)
	assert.Empty(t, sms.sent)
}

func TestFallbackGoesThroughTheRequestedChannel(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, sms, "agora://app", "")
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
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, &fakePush{err: services.ErrPushTokenInvalid}, &fakeSMS{}, "", "")

	require.NoError(t, notifier.SendOffer(context.Background(), "fcm", dispatch.OfferNotice{ProviderID: provider}))

	assert.Equal(t, []string{"gone"}, devices.removed)
	assert.Equal(t, "failed", devices.records[0].Status)
}

func TestPushOutageSurfacesForRetry(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, &fakePush{err: errors.New("fcm down")}, &fakeSMS{}, "", "")

	assert.Error(t, notifier.SendOffer(context.Background(), "fcm", dispatch.OfferNotice{ProviderID: provider}))
}

func TestAcceptedConfirmsBySMSWhenThePolicyAsksAndTellsThePassenger(t *testing.T) {
	customer := uuid.New()
	devices := &fakeDevices{phones: map[uuid.UUID]string{customer: "+244911111111"}}
	sms := &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, &fakePush{}, sms, "", "")
	event := events.RequestTransitionV1{Type: events.RequestAccepted, RequestID: uuid.New(), CustomerID: customer, To: "accepted", SMSOnAccept: true, PassengerPhone: "+244922222222"}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Contains(t, sms.sent["+244911111111"], "aceite")
	assert.Equal(t, "AGORA: reboque de João, matrícula LD-1.", sms.sent["+244922222222"], "the text comes from the category, with the driver and the plate")
}

func TestOperatorAlertUsesTheConfiguredPhone(t *testing.T) {
	sms := &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(&fakeDevices{}, fakeNotices{}, &fakePush{}, sms, "", "+244933333333")
	event := events.OperatorAlertedV1{RequestID: uuid.New(), Category: "towing", Reason: "no_provider"}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Contains(t, sms.sent["+244933333333"], "no_provider")
}

func TestASubmittedVerificationIsAPushWithoutSMS(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244944444444"}}
	push, sms := &fakePush{}, &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, sms, "", "")
	event := events.ProviderReviewV1{Type: events.ProviderApplicationSubmitted, ProviderID: provider, OccurredAt: time.Now()}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	require.Len(t, push.sent, 1)
	assert.Equal(t, "Verificação enviada", push.sent[0].Title)
	assert.Empty(t, sms.sent)
}

func TestExpiryReminderNamesTheDocumentAndTheDay(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}}
	push := &fakePush{}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, &fakeSMS{sent: map[string]string{}}, "", "")
	expires := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	event := events.ProviderVerificationReminderV1{ReminderID: uuid.New(), ProviderID: provider, Kind: "document_expiry", DocumentType: "vehicle_insurance", ExpiresOn: &expires, OccurredAt: time.Now()}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	require.Len(t, push.sent, 1)
	assert.Equal(t, "Documento a expirar", push.sent[0].Title)
	assert.Contains(t, push.sent[0].Body, "Seguro")
	assert.Contains(t, push.sent[0].Body, "01/12/2026")
	assert.Equal(t, "vehicle_insurance", push.sent[0].Data["document_type"])
}

func TestCriminalRecordReminderAsksForTheCertificate(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}}
	push := &fakePush{}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, &fakeSMS{sent: map[string]string{}}, "", "")

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, events.ProviderVerificationReminderV1{ProviderID: provider, Kind: "criminal_record", OccurredAt: time.Now()})))

	require.Len(t, push.sent, 1)
	assert.Contains(t, push.sent[0].Body, "registo criminal")
}

func TestAChatMessageIsPushedToTheOtherSide(t *testing.T) {
	driver, customer := uuid.New(), uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{driver: {"driver-device"}, customer: {"customer-device"}}}
	push := &fakePush{}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, &fakeSMS{sent: map[string]string{}}, "", "")
	event := events.RequestMessageV1{MessageID: uuid.New(), RequestID: uuid.New(), SenderType: "customer", RecipientID: driver, Body: "Estou junto à farmácia", OccurredAt: time.Now()}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	require.Len(t, push.sent, 1)
	sent := push.sent[0]
	assert.Equal(t, "driver-device", sent.Token, "only the recipient")
	assert.Equal(t, "Mensagem do cliente", sent.Title)
	assert.Equal(t, "Estou junto à farmácia", sent.Body)
	assert.True(t, sent.HighPriority)
	assert.Equal(t, "request.message", sent.Data["type"])
	assert.Equal(t, event.RequestID.String(), sent.Data["request_id"])
	assert.Equal(t, event.MessageID.String(), sent.Data["message_id"])
}

func TestALongChatMessageIsCutInThePush(t *testing.T) {
	customer := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{customer: {"device"}}}
	push := &fakePush{}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, &fakeSMS{sent: map[string]string{}}, "", "")
	event := events.RequestMessageV1{MessageID: uuid.New(), RequestID: uuid.New(), SenderType: "workshop", RecipientID: customer, Body: strings.Repeat("ã", 300), OccurredAt: time.Now()}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	require.Len(t, push.sent, 1)
	assert.Equal(t, "Mensagem da oficina", push.sent[0].Title)
	assert.Equal(t, 120, utf8.RuneCountInString(push.sent[0].Body))
	assert.True(t, strings.HasSuffix(push.sent[0].Body, "…"))
}

func TestAChatMessageListensOnTheNotificationQueue(t *testing.T) {
	assert.Contains(t, notifications.RequestBindingKeys(), events.RoutingKey(events.RequestMessage, 1))
}

func TestARetriedApprovalSendsTheSMSOnce(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	push, sms := &fakePush{err: errors.New("fcm down")}, &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, push, sms, "", "")
	env := requestEnvelope(t, events.ProviderReviewV1{Type: events.ProviderApproved, ProviderID: provider})

	for range 5 {
		require.Error(t, notifier.OnEvent(context.Background(), env), "the push is still owed")
	}
	push.err = nil
	require.NoError(t, notifier.OnEvent(context.Background(), env))
	require.NoError(t, notifier.OnEvent(context.Background(), env))

	assert.Equal(t, 1, sms.count, "each retry only sends what is still missing")
	assert.Len(t, push.sent, 1)
}

func TestAPushRejectedForGoodIsNotRetried(t *testing.T) {
	provider := uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{provider: {"device"}}, phones: map[uuid.UUID]string{provider: "+244900000000"}}
	sms := &fakeSMS{sent: map[string]string{}}
	notifier := notifications.NewRequestNotifier(devices, fakeNotices{}, &fakePush{err: fmt.Errorf("%w: THIRD_PARTY_AUTH_ERROR", services.ErrPushRejected)}, sms, "", "")

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, events.ProviderReviewV1{Type: events.ProviderApproved, ProviderID: provider})))

	assert.Equal(t, 1, sms.count)
	assert.Empty(t, devices.removed, "the token is fine, the app setup is not")
	require.NotEmpty(t, devices.records)
	assert.Contains(t, devices.records[0].Error, "THIRD_PARTY_AUTH_ERROR")
}

type fakeAccount struct {
	repository.IAccountRepository
	items []*entities.InboxItem
}

func (a *fakeAccount) AddInboxItem(_ context.Context, item *entities.InboxItem) error {
	a.items = append(a.items, item)
	return nil
}

func (a *fakeAccount) Preferences(context.Context, uuid.UUID) (*entities.Preferences, error) {
	return &entities.Preferences{NotifyStatus: true, NotifySMS: true}, nil
}

func (a *fakeAccount) itemsOf(userID uuid.UUID) []*entities.InboxItem {
	var out []*entities.InboxItem
	for _, item := range a.items {
		if item.UserID == userID {
			out = append(out, item)
		}
	}
	return out
}

func driverSetup() (customer, driver uuid.UUID, push *fakePush, account *fakeAccount, notifier *notifications.RequestNotifier) {
	customer, driver = uuid.New(), uuid.New()
	devices := &fakeDevices{tokens: map[uuid.UUID][]string{customer: {"customer-phone"}, driver: {"driver-phone"}}}
	push, account = &fakePush{}, &fakeAccount{}
	notifier = notifications.NewRequestNotifier(devices, fakeNotices{}, push, &fakeSMS{sent: map[string]string{}}, "", "")
	notifier.UseAccount(account)
	return customer, driver, push, account, notifier
}

func pushesTo(push *fakePush, token string) []services.PushMessage {
	var out []services.PushMessage
	for _, message := range push.sent {
		if message.Token == token {
			out = append(out, message)
		}
	}
	return out
}

func TestTheDriverHearsWhenSomeoneElseCancels(t *testing.T) {
	for actor, body := range map[string]string{"customer": "O cliente cancelou o pedido.", "operator": "A operação cancelou o pedido.", "system": "O pedido foi cancelado."} {
		customer, driver, push, account, notifier := driverSetup()
		event := events.RequestTransitionV1{Type: events.RequestCancelled, RequestID: uuid.New(), CustomerID: customer, ProviderID: &driver, To: "cancelled", ActorType: actor}

		require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

		toDriver := pushesTo(push, "driver-phone")
		require.Len(t, toDriver, 1, actor)
		assert.Equal(t, body, toDriver[0].Body, actor)
		assert.Equal(t, "request.status", toDriver[0].Data["type"])
		assert.Equal(t, "cancelled", toDriver[0].Data["status"])
		assert.Len(t, account.itemsOf(driver), 1, actor)
		assert.Len(t, pushesTo(push, "customer-phone"), 1, "the customer still hears it")
	}
}

func TestTheDriverIsNotToldWhatTheDriverDid(t *testing.T) {
	customer, driver, push, account, notifier := driverSetup()
	event := events.RequestTransitionV1{Type: events.RequestCancelled, RequestID: uuid.New(), CustomerID: customer, ProviderID: &driver, To: "cancelled", ActorType: "provider"}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	assert.Empty(t, pushesTo(push, "driver-phone"))
	assert.Empty(t, account.itemsOf(driver))
}

func TestTheDriverHearsAManualAssignment(t *testing.T) {
	customer, driver, push, account, notifier := driverSetup()
	event := events.RequestTransitionV1{Type: events.RequestAccepted, RequestID: uuid.New(), CustomerID: customer, ProviderID: &driver, To: "accepted", ActorType: "operator"}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	toDriver := pushesTo(push, "driver-phone")
	require.Len(t, toDriver, 1)
	assert.Equal(t, "Pedido atribuído", toDriver[0].Title)
	assert.True(t, toDriver[0].HighPriority)
	require.Len(t, account.itemsOf(driver), 1)
	assert.Equal(t, event.RequestID, *account.itemsOf(driver)[0].LinkID)
}

func TestTheDriverHearsANewDestination(t *testing.T) {
	customer, driver, push, account, notifier := driverSetup()
	event := events.RouteChangedV1{RequestID: uuid.New(), CustomerID: customer, ProviderID: &driver, Version: 7}

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, event)))

	toDriver := pushesTo(push, "driver-phone")
	require.Len(t, toDriver, 1)
	assert.Equal(t, "request.route_changed", toDriver[0].Data["type"])
	assert.Equal(t, "7", toDriver[0].Data["version"])
	assert.Empty(t, pushesTo(push, "customer-phone"), "the customer made the change")
	assert.Len(t, account.itemsOf(driver), 1)
	assert.Contains(t, notifications.RequestBindingKeys(), events.RoutingKey(events.RouteChanged, 1))
}

func TestTheDriverKeepsAccountNoticesInTheInbox(t *testing.T) {
	_, driver, push, account, notifier := driverSetup()
	expires := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)

	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, events.ProviderReviewV1{Type: events.ProviderApproved, ProviderID: driver})))
	require.NoError(t, notifier.OnEvent(context.Background(), requestEnvelope(t, events.ProviderVerificationReminderV1{ProviderID: driver, Kind: entities.ReminderDocumentExpiry, DocumentType: "driver_license", ExpiresOn: &expires})))

	items := account.itemsOf(driver)
	require.Len(t, items, 2)
	assert.Equal(t, "Conta aprovada", items[0].Title)
	assert.Contains(t, items[1].Body, "01/12/2026")
	assert.Len(t, pushesTo(push, "driver-phone"), 2)
}
