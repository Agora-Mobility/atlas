package dispatch

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const (
	shareTokenBytes = 32
	senderCustomer  = "customer"
	senderProvider  = "provider"
)

func (u *Usecase) participant(ctx context.Context, userID, requestID uuid.UUID) (*entities.ServiceRequest, string, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, "", err
	}
	switch {
	case request == nil:
		return nil, "", ErrRequestNotFound
	case request.IsCustomer(userID):
		return request, senderCustomer, nil
	case request.IsProvider(userID):
		return request, senderProvider, nil
	}
	return nil, "", ErrRequestNotFound
}

func toMessageView(m *entities.RequestMessage) dtos.MessageView {
	return dtos.MessageView{ID: m.ID, Sender: m.SenderType, Body: m.Body, QuickReplyID: m.QuickReplyID, ReadAt: m.ReadAt, CreatedAt: m.CreatedAt}
}

func (u *Usecase) SendMessage(ctx context.Context, userID, requestID uuid.UUID, in dtos.SendMessageInput) (*dtos.MessageView, error) {
	request, sender, err := u.participant(ctx, userID, requestID)
	if err != nil {
		return nil, err
	}
	if !request.Status.HasLiveProvider() || request.ProviderID == nil {
		return nil, entities.ErrChatClosed
	}
	body := in.Body
	if in.QuickReplyID != nil {
		reply, err := u.Reference.Item(ctx, *in.QuickReplyID)
		if err != nil {
			return nil, err
		}
		if reply == nil || reply.Kind != entities.CatalogQuickReply || !reply.IsActive {
			return nil, entities.ErrCatalogItemNotFound
		}
		body = reply.Label
	}
	if body, err = entities.NormalizeMessage(body); err != nil {
		return nil, err
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	message := &entities.RequestMessage{ID: id, RequestID: requestID, SenderID: userID, SenderType: sender, Body: body, QuickReplyID: in.QuickReplyID, CreatedAt: u.now()}
	recipient := request.CustomerID
	if sender == senderCustomer {
		recipient = *request.ProviderID
	}
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := u.Live.AddMessage(ctx, message); err != nil {
			return err
		}
		return u.publish(ctx, events.RequestMessageV1{MessageID: id, RequestID: requestID, SenderType: sender, RecipientID: recipient, Body: body, OccurredAt: message.CreatedAt})
	})
	if err != nil {
		return nil, err
	}
	view := toMessageView(message)
	return &view, nil
}

func (u *Usecase) Messages(ctx context.Context, userID, requestID uuid.UUID, page pagination.Params) (*dtos.MessageList, error) {
	if _, _, err := u.participant(ctx, userID, requestID); err != nil {
		return nil, err
	}
	page = page.Normalize()
	messages, err := u.Live.Messages(ctx, requestID, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	if err := u.Live.MarkRead(ctx, requestID, userID, u.now()); err != nil {
		return nil, err
	}
	list := &dtos.MessageList{Items: make([]dtos.MessageView, 0, len(messages))}
	for i := range messages {
		list.Items = append(list.Items, toMessageView(&messages[i]))
	}
	return list, nil
}

func (u *Usecase) QuickReplies(ctx context.Context, actor string) (*dtos.QuickReplyList, error) {
	items, err := u.Reference.Items(ctx, entities.CatalogQuickReply, "", false)
	if err != nil {
		return nil, err
	}
	list := &dtos.QuickReplyList{Items: []dtos.QuickReplyView{}}
	for i := range items {
		if strings.Contains(string(items[i].Attrs), `"`+actor+`"`) {
			list.Items = append(list.Items, dtos.QuickReplyView{ID: items[i].ID, Label: items[i].Label})
		}
	}
	return list, nil
}

func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (u *Usecase) newShare(ctx context.Context, requestID uuid.UUID) (string, time.Time, error) {
	raw, err := u.Reference.Setting(ctx, entities.SettingShareTTLMinutes)
	if err != nil {
		return "", time.Time{}, err
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil {
		return "", time.Time{}, err
	}
	buffer := make([]byte, shareTokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	id, err := newUUID()
	if err != nil {
		return "", time.Time{}, err
	}
	now := u.now()
	expires := now.Add(time.Duration(minutes) * time.Minute)
	if err := u.Live.CreateShare(ctx, id, requestID, hashShareToken(token), expires, now); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

func (u *Usecase) ShareLink(ctx context.Context, requestID uuid.UUID) (string, error) {
	token, _, err := u.newShare(ctx, requestID)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(u.settings.ShareBaseURL, "/") + "/" + token, nil
}

func (u *Usecase) ShareTrip(ctx context.Context, customerID, requestID uuid.UUID) (*dtos.ShareView, error) {
	request, role, err := u.participant(ctx, customerID, requestID)
	if err != nil {
		return nil, err
	}
	if role != senderCustomer {
		return nil, ErrRequestNotFound
	}
	if request.Status.IsTerminal() {
		return nil, entities.ErrShareNotAvailable
	}
	token, expires, err := u.newShare(ctx, requestID)
	if err != nil {
		return nil, err
	}
	return &dtos.ShareView{URL: strings.TrimRight(u.settings.ShareBaseURL, "/") + "/" + token, Token: token, ExpiresAt: expires}, nil
}

func (u *Usecase) RevokeShares(ctx context.Context, customerID, requestID uuid.UUID) error {
	_, role, err := u.participant(ctx, customerID, requestID)
	if err != nil {
		return err
	}
	if role != senderCustomer {
		return ErrRequestNotFound
	}
	return u.Live.RevokeShares(ctx, requestID, u.now())
}

func (u *Usecase) PublicTrip(ctx context.Context, token string) (*dtos.PublicTrip, error) {
	share, err := u.Live.ShareByHash(ctx, hashShareToken(token))
	if err != nil {
		return nil, err
	}
	now := u.now()
	if share == nil || share.RevokedAt != nil || !now.Before(share.ExpiresAt) {
		return nil, entities.ErrShareNotFound
	}
	request, err := u.Requests.Find(ctx, share.RequestID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, entities.ErrShareNotFound
	}
	view := &dtos.PublicTrip{Code: request.PublicCode, Status: string(request.Status), Category: request.CategorySlug, ExpiresAt: share.ExpiresAt, PickupAddress: request.Pickup.Address}
	if request.Destination != nil {
		view.DestinationAddress = request.Destination.Address
	}
	if request.Status.IsTerminal() {
		return view, nil
	}
	provider, err := u.providerSummary(ctx, request.ProviderID)
	if err != nil {
		return nil, err
	}
	if provider != nil {
		view.ProviderName, view.VehiclePlate, view.VehicleDescription = provider.Name, provider.VehiclePlate, provider.VehicleDescription
	}
	if eta, err := u.etaOf(ctx, request); err == nil {
		view.Position, view.ETASeconds = eta.position, &eta.seconds
	}
	return view, nil
}

type liveETA struct {
	position *dtos.PositionView
	seconds  int
	meters   int
	source   string
	target   string
}

func (u *Usecase) etaOf(ctx context.Context, request *entities.ServiceRequest) (*liveETA, error) {
	if request.ProviderID == nil || !request.Status.HasLiveProvider() {
		return nil, entities.ErrNoETA
	}
	position, err := u.Locator.LastPosition(ctx, *request.ProviderID)
	if err != nil {
		return nil, err
	}
	if position == nil {
		return nil, entities.ErrNoETA
	}
	target, name := request.Pickup.Point, "pickup"
	if request.Status == entities.RequestInProgress && request.Destination != nil {
		target, name = request.Destination.Point, "destination"
	}
	route, err := u.Routing.Route(ctx, position.Point, target)
	if err != nil {
		return nil, err
	}
	return &liveETA{
		position: &dtos.PositionView{Lat: position.Point.Lat, Lng: position.Point.Lng, AgeS: int(u.now().Sub(position.RecordedAt).Seconds())},
		seconds:  int(route.Duration.Seconds()), meters: route.DistanceMeters, source: string(route.Source), target: name,
	}, nil
}

func (u *Usecase) ETA(ctx context.Context, userID, requestID uuid.UUID) (*dtos.ETAView, error) {
	request, _, err := u.participant(ctx, userID, requestID)
	if err != nil {
		return nil, err
	}
	eta, err := u.etaOf(ctx, request)
	if err != nil {
		return nil, err
	}
	return &dtos.ETAView{DurationS: eta.seconds, DistanceM: eta.meters, Source: eta.source, Target: eta.target, Position: eta.position}, nil
}

func (u *Usecase) Call(ctx context.Context, userID, requestID uuid.UUID) (*dtos.CallView, error) {
	request, role, err := u.participant(ctx, userID, requestID)
	if err != nil {
		return nil, err
	}
	if !request.Status.HasLiveProvider() || request.ProviderID == nil {
		return nil, entities.ErrCallNotAvailable
	}
	if !u.Calls.Enabled() {
		return nil, services.ErrCallsUnavailable
	}
	other := request.CustomerID
	if role == senderCustomer {
		other = *request.ProviderID
	}
	_, from, err := u.Devices.Contact(ctx, userID)
	if err != nil {
		return nil, err
	}
	_, to, err := u.Devices.Contact(ctx, other)
	if err != nil {
		return nil, err
	}
	if role == senderProvider && request.PassengerPhone != "" {
		to = request.PassengerPhone
	}
	call, err := u.Calls.Connect(ctx, from, to, request.PublicCode)
	if err != nil {
		return nil, err
	}
	return &dtos.CallView{ProxyNumber: call.ProxyNumber, ExpiresAt: call.ExpiresAt}, nil
}

func toSOSView(alert *entities.SOSAlert) dtos.SOSView {
	view := dtos.SOSView{ID: alert.ID, RequestID: alert.RequestID, UserID: alert.UserID, Note: alert.Note, Status: alert.Status, ResolvedAt: alert.ResolvedAt, CreatedAt: alert.CreatedAt}
	if alert.Point != nil {
		view.Lat, view.Lng = &alert.Point.Lat, &alert.Point.Lng
	}
	return view
}

func (u *Usecase) RaiseSOS(ctx context.Context, userID, requestID uuid.UUID, in dtos.SOSInput) (*dtos.SOSView, bool, error) {
	request, _, err := u.participant(ctx, userID, requestID)
	if err != nil {
		return nil, false, err
	}
	id, err := newUUID()
	if err != nil {
		return nil, false, err
	}
	now := u.now()
	alert := &entities.SOSAlert{ID: id, RequestID: requestID, UserID: userID, Note: in.Note, Status: entities.SOSOpen, CreatedAt: now}
	if in.Lat != nil && in.Lng != nil {
		alert.Point = &entities.GeoPoint{Lat: *in.Lat, Lng: *in.Lng}
	}
	if err := alert.Normalize(); err != nil {
		return nil, false, err
	}
	created := false
	err = u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if created, err = u.Live.CreateSOS(ctx, alert); err != nil {
			return err
		}
		if !created {
			alert, err = u.Live.OpenSOS(ctx, requestID, userID)
			return err
		}
		return u.publish(ctx, events.OperatorAlertedV1{RequestID: requestID, CustomerID: request.CustomerID, Category: request.CategorySlug, Reason: "sos", OccurredAt: now})
	})
	if err != nil {
		return nil, false, err
	}
	view := toSOSView(alert)
	return &view, created, nil
}

func (u *Usecase) ListSOS(ctx context.Context, status string, page pagination.Params) (*dtos.SOSList, error) {
	page = page.Normalize()
	alerts, total, err := u.Live.ListSOS(ctx, status, int(page.Limit()), int(page.Offset()))
	if err != nil {
		return nil, err
	}
	list := &dtos.SOSList{Items: make([]dtos.SOSView, 0, len(alerts)), Total: total, Page: page.Page, Size: page.Size}
	for i := range alerts {
		list.Items = append(list.Items, toSOSView(&alerts[i]))
	}
	return list, nil
}

func (u *Usecase) ResolveSOS(ctx context.Context, actor entities.Actor, id uuid.UUID) error {
	return u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		found, err := u.Live.ResolveSOS(ctx, id, actor.ID, u.now())
		if err != nil {
			return err
		}
		if !found {
			return entities.ErrSOSNotFound
		}
		return u.audit(ctx, actor, "sos.resolve", "sos_alert", id, nil, nil)
	})
}
