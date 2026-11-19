package notifications

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
)

const (
	templateRideLive = "ride_live"
	rideLiveType     = "ride.live"
	// positionTTLSeconds: a position update that cannot be delivered within this
	// time is worth nothing, the next one is already on its way
	positionTTLSeconds = 60
)

var rideLiveAlerts = map[entities.RidePhase]pushText{
	entities.RidePhaseEnRoute:   {"Motorista a caminho", "O teu motorista está a caminho do ponto de recolha."},
	entities.RidePhaseArrived:   {"O motorista chegou", "O teu motorista está à tua espera."},
	entities.RidePhaseInTrip:    {"Viagem iniciada", "Boa viagem!"},
	entities.RidePhaseCompleted: {"Viagem concluída", "Chegaste ao destino. Obrigado!"},
	entities.RidePhaseCancelled: {"Corrida cancelada", "A corrida foi cancelada."},
	entities.RidePhaseExpired:   {"Sem motoristas disponíveis", "Não encontrámos motorista. Tenta de novo dentro de alguns minutos."},
}

type RideLiveSettings struct {
	RoadFactor      float64
	AverageSpeedKmh float64
	// StaleAfter is when iOS shows the Live Activity as out of date if no update came
	StaleAfter time.Duration
	// DismissAfter is how long a completed ride stays on the lock screen
	DismissAfter time.Duration
}

// RideLiveNotifier keeps the lock screen of the customer in step with the ride:
// a data-only FCM message on Android and an APNs liveactivity push on iOS, with
// the status, the ETA, the distance and the position of the driver.
type RideLiveNotifier struct {
	tokens   repository.IRidePushTokenRepository
	requests repository.IRequestRepository
	locator  repository.IProviderLocator
	devices  repository.IDeviceRepository
	push     services.IPushSender
	live     services.ILiveActivitySender
	settings RideLiveSettings
	now      func() time.Time
}

var _ dispatch.RideLiveChannel = (*RideLiveNotifier)(nil)

func NewRideLiveNotifier(
	tokens repository.IRidePushTokenRepository, requests repository.IRequestRepository, locator repository.IProviderLocator,
	devices repository.IDeviceRepository, push services.IPushSender, live services.ILiveActivitySender, settings RideLiveSettings,
) *RideLiveNotifier {
	return &RideLiveNotifier{
		tokens: tokens, requests: requests, locator: locator, devices: devices, push: push, live: live, settings: settings,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// RideLiveBindingKeys are the transitions the lock screen shows.
func RideLiveBindingKeys() []string {
	types := []string{
		events.RequestAccepted, events.RequestProviderEnRoute, events.RequestInProgress,
		events.RequestCompleted, events.RequestCancelled, events.RequestExpired,
	}
	keys := make([]string, 0, len(types))
	for _, eventType := range types {
		keys = append(keys, events.RoutingKey(eventType, 1))
	}
	return keys
}

func (n *RideLiveNotifier) OnEvent(ctx context.Context, env events.Envelope) error {
	var event events.RequestTransitionV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	status := entities.RequestStatus(event.To)
	if status.IsTerminal() {
		state := entities.RideLiveState{RequestID: event.RequestID, Status: status, Phase: entities.RidePhaseOf(status, false), At: event.OccurredAt}
		return n.end(ctx, env.EventID.String(), state)
	}
	return n.refresh(ctx, env.EventID.String(), event.RequestID, event.OccurredAt)
}

func (n *RideLiveNotifier) Refresh(ctx context.Context, requestID uuid.UUID) error {
	return n.refresh(ctx, "", requestID, n.now())
}

func (n *RideLiveNotifier) Position(ctx context.Context, state entities.RideLiveState) error {
	tokens, err := n.tokens.OfRequest(ctx, state.RequestID)
	if err != nil || len(tokens) == 0 {
		return err
	}
	return n.send(ctx, "", tokens, state, services.LiveActivityUpdate, false)
}

func (n *RideLiveNotifier) refresh(ctx context.Context, eventKey string, requestID uuid.UUID, at time.Time) error {
	tokens, err := n.tokens.OfRequest(ctx, requestID)
	if err != nil || len(tokens) == 0 {
		return err
	}
	request, err := n.requests.Find(ctx, requestID)
	if err != nil || request == nil {
		return err
	}
	state := entities.RideLiveState{RequestID: requestID, Status: request.Status, Phase: entities.RidePhaseOf(request.Status, request.ArrivedAt != nil), At: at}
	if request.Status.IsTerminal() {
		return n.end(ctx, eventKey, state)
	}
	if err := n.addDriver(ctx, request, &state); err != nil {
		return err
	}
	return n.send(ctx, eventKey, tokens, state, services.LiveActivityUpdate, true)
}

func (n *RideLiveNotifier) addDriver(ctx context.Context, request *entities.ServiceRequest, state *entities.RideLiveState) error {
	if request.ProviderID == nil || !request.Status.HasLiveProvider() {
		return nil
	}
	position, err := n.locator.LastPosition(ctx, *request.ProviderID)
	if err != nil || position == nil {
		return err
	}
	point := position.Point
	state.Driver, state.Heading = &point, position.Heading
	target := request.Pickup.Point
	if request.Status == entities.RequestInProgress {
		if request.Destination == nil {
			return nil
		}
		target = request.Destination.Point
	}
	meters, seconds := entities.StraightETA(point, target, n.settings.RoadFactor, n.settings.AverageSpeedKmh)
	state.DistanceM, state.ETASecond = &meters, &seconds
	return nil
}

// end closes the Live Activity and drops the tokens. When a send fails and can
// be retried the tokens stay, so the retry of the event finds them.
func (n *RideLiveNotifier) end(ctx context.Context, eventKey string, state entities.RideLiveState) error {
	tokens, err := n.tokens.OfRequest(ctx, state.RequestID)
	if err != nil || len(tokens) == 0 {
		return err
	}
	if err := n.send(ctx, eventKey, tokens, state, services.LiveActivityEnd, true); err != nil {
		return err
	}
	return n.tokens.DeleteOfRequest(ctx, state.RequestID)
}

func (n *RideLiveNotifier) send(ctx context.Context, eventKey string, tokens []entities.RidePushToken, state entities.RideLiveState, event string, statusChange bool) error {
	var failures []error
	for _, token := range tokens {
		channel, id, err := n.deliver(ctx, token, state, event, statusChange)
		key := ""
		if statusChange {
			key = deliveryKey(eventKey, channel, templateRideLive, token.Token)
		}
		switch {
		case errors.Is(err, services.ErrPushTokenInvalid):
			if removeErr := n.tokens.Remove(ctx, token.Token); removeErr != nil {
				failures = append(failures, removeErr)
			}
			n.record(ctx, token, channel, statusFailed, "", err.Error(), key)
		case errors.Is(err, services.ErrPushRejected):
			logger.FromContext(ctx).Warn("ride live push rejected", zap.String("request_id", state.RequestID.String()), zap.String("platform", token.Platform), zap.Error(err))
			n.record(ctx, token, channel, statusFailed, "", err.Error(), key)
		case err != nil:
			failures = append(failures, err)
			if statusChange {
				n.record(ctx, token, channel, statusFailed, "", err.Error(), key)
			}
		case statusChange:
			n.record(ctx, token, channel, statusSent, id, "", key)
		}
	}
	return errors.Join(failures...)
}

func (n *RideLiveNotifier) deliver(ctx context.Context, token entities.RidePushToken, state entities.RideLiveState, event string, statusChange bool) (string, string, error) {
	ttl := positionTTLSeconds
	if statusChange {
		ttl = 0
	}
	if token.Platform == entities.PlatformIOS {
		message := services.LiveActivityMessage{
			Token: token.Token, Event: event, ContentState: contentState(state), Timestamp: state.At, HighPriority: statusChange, TTLSeconds: ttl,
		}
		if event == services.LiveActivityEnd {
			dismiss := state.At
			if state.Phase == entities.RidePhaseCompleted {
				dismiss = state.At.Add(n.settings.DismissAfter)
			}
			message.DismissAt = &dismiss
		} else if n.settings.StaleAfter > 0 {
			stale := state.At.Add(n.settings.StaleAfter)
			message.StaleAt = &stale
		}
		if alert, known := rideLiveAlerts[state.Phase]; known && statusChange {
			message.AlertTitle, message.AlertBody = alert.title, alert.body
		}
		id, err := n.live.Send(ctx, message)
		return entities.ChannelAPNS, id, err
	}
	id, err := n.push.Send(ctx, services.PushMessage{Token: token.Token, Data: androidData(state, event), HighPriority: true, TTLSeconds: ttl})
	return entities.ChannelFCM, id, err
}

// liveContentState is the ContentState of the Live Activity of the customer app:
// its Swift struct has to decode these keys. updatedAt is in seconds since 1970.
type liveContentState struct {
	Status         string   `json:"status"`
	Phase          string   `json:"phase"`
	ETASeconds     *int     `json:"etaSeconds,omitempty"`
	DistanceMeters *int     `json:"distanceMeters,omitempty"`
	DriverLat      *float64 `json:"driverLat,omitempty"`
	DriverLng      *float64 `json:"driverLng,omitempty"`
	Heading        *float64 `json:"heading,omitempty"`
	UpdatedAt      int64    `json:"updatedAt"`
}

func contentState(state entities.RideLiveState) liveContentState {
	out := liveContentState{
		Status: string(state.Status), Phase: string(state.Phase), ETASeconds: state.ETASecond, DistanceMeters: state.DistanceM,
		Heading: state.Heading, UpdatedAt: state.At.Unix(),
	}
	if state.Driver != nil {
		out.DriverLat, out.DriverLng = &state.Driver.Lat, &state.Driver.Lng
	}
	return out
}

func androidData(state entities.RideLiveState, event string) map[string]string {
	data := map[string]string{
		"type": rideLiveType, "event": event, "request_id": state.RequestID.String(), "status": string(state.Status), "phase": string(state.Phase),
		"updated_at": strconv.FormatInt(state.At.Unix(), 10),
	}
	if state.ETASecond != nil {
		data["eta_s"] = strconv.Itoa(*state.ETASecond)
	}
	if state.DistanceM != nil {
		data["distance_m"] = strconv.Itoa(*state.DistanceM)
	}
	if state.Driver != nil {
		data["lat"] = strconv.FormatFloat(state.Driver.Lat, 'f', 6, 64)
		data["lng"] = strconv.FormatFloat(state.Driver.Lng, 'f', 6, 64)
	}
	if state.Heading != nil {
		data["heading"] = strconv.FormatFloat(*state.Heading, 'f', 1, 64)
	}
	if alert, known := rideLiveAlerts[state.Phase]; known {
		data["title"], data["body"] = alert.title, alert.body
	}
	return data
}

func (n *RideLiveNotifier) record(ctx context.Context, token entities.RidePushToken, channel, status, messageID, failure, key string) {
	userID := token.UserID
	err := n.devices.RecordNotification(ctx, repository.NotificationRecord{
		UserID: &userID, Channel: channel, Template: templateRideLive, Status: status, ProviderMessageID: messageID, Error: failure, DeliveryKey: key,
	})
	if err != nil {
		logger.FromContext(ctx).Warn("notification not recorded", zap.Error(err))
	}
}
