package notifications

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
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
	templateOffer          = "offer"
	templateOfferCancelled = "offer_cancelled"
	templateStatus         = "request_status"
	templateAssigned       = "request_assigned"
	templateQuote          = "quote_received"
	templateReminder       = "request_reminder"
	templateOperator       = "operator_alert"
	templateProviderReview = "provider_review"
	templatePayment        = "payment_status"
	statusSent             = "sent"
	statusFailed           = "failed"
	statusSkipped          = "skipped"
)

var statusMessages = map[string]struct{ title, body string }{
	events.RequestAccepted:        {"Prestador atribuído", "O teu pedido foi aceite."},
	events.RequestProviderEnRoute: {"A caminho", "O prestador está a caminho."},
	events.RequestInProgress:      {"Serviço iniciado", "O teu serviço começou."},
	events.RequestCompleted:       {"Serviço concluído", "O teu serviço terminou. Obrigado!"},
	events.RequestExpired:         {"Sem prestadores disponíveis", "Não encontrámos prestador. Tenta de novo dentro de alguns minutos."},
	events.RequestCancelled:       {"Pedido cancelado", "O pedido foi cancelado."},
}

type TripSharer interface {
	ShareLink(ctx context.Context, requestID uuid.UUID) (string, error)
}

type RequestNotifier struct {
	devices       repository.IDeviceRepository
	requests      repository.IRequestRepository
	shares        TripSharer
	push          services.IPushSender
	sms           services.ISMSSender
	deepLinkBase  string
	operatorPhone string
}

var _ dispatch.OfferChannels = (*RequestNotifier)(nil)

func NewRequestNotifier(devices repository.IDeviceRepository, requests repository.IRequestRepository, push services.IPushSender, sms services.ISMSSender, deepLinkBase, operatorPhone string) *RequestNotifier {
	return &RequestNotifier{devices: devices, requests: requests, push: push, sms: sms, deepLinkBase: deepLinkBase, operatorPhone: operatorPhone}
}

func RequestBindingKeys() []string {
	types := make([]string, 0, len(statusMessages)+4)
	for eventType := range statusMessages {
		types = append(types, eventType)
	}
	slices.Sort(types)
	types = append(types, events.PaymentChanged, events.OfferCreated, events.OfferCancelled, events.QuoteSubmitted, events.ReminderDue, events.OperatorAlerted,
		events.ProviderApproved, events.ProviderRejected, events.ProviderSuspended)
	keys := make([]string, 0, len(types))
	for _, eventType := range types {
		keys = append(keys, events.RoutingKey(eventType, 1))
	}
	return keys
}

func (n *RequestNotifier) OnEvent(ctx context.Context, env events.Envelope) error {
	switch env.EventType {
	case events.OfferCreated:
		return n.onOfferCreated(ctx, env)
	case events.OfferCancelled:
		return n.onOfferCancelled(ctx, env)
	case events.QuoteSubmitted:
		return n.onQuote(ctx, env)
	case events.ReminderDue:
		return n.onReminder(ctx, env)
	case events.OperatorAlerted:
		return n.onOperatorAlert(ctx, env)
	case events.ProviderApproved, events.ProviderRejected, events.ProviderSuspended:
		return n.onProviderReview(ctx, env)
	case events.PaymentChanged:
		return n.onPayment(ctx, env)
	default:
		return n.onTransition(ctx, env)
	}
}

func (n *RequestNotifier) onOfferCreated(ctx context.Context, env events.Envelope) error {
	var event events.RequestOfferV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	remaining := int(time.Until(event.ExpiresAt).Seconds())
	if remaining <= 0 {
		return nil
	}
	notice := dispatch.OfferNotice{OfferID: event.OfferID, RequestID: event.RequestID, ProviderID: event.ProviderID, Category: event.Category, ExpiresIn: remaining, DistanceM: event.DistanceM}
	if event.Pickup != nil {
		notice.Reference = event.Pickup.Reference
	}
	var failures []error
	for _, channel := range event.Channels {
		if channel == entities.ChannelSSE {
			continue
		}
		if err := n.SendOffer(ctx, channel, notice); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (n *RequestNotifier) SendOffer(ctx context.Context, channel string, notice dispatch.OfferNotice) error {
	data := map[string]string{
		"type": "request.offer", "offer_id": notice.OfferID.String(), "request_id": notice.RequestID.String(), "category": notice.Category,
		"event_id": notice.OfferID.String(), "expires_in_s": strconv.Itoa(notice.ExpiresIn),
	}
	switch channel {
	case entities.ChannelFCM:
		return n.pushTo(ctx, notice.ProviderID, templateOffer, services.PushMessage{
			Title: "Novo pedido", Body: offerBody(notice), Data: data, HighPriority: true, TTLSeconds: notice.ExpiresIn,
		})
	case entities.ChannelSMS:
		return n.smsToUser(ctx, notice.ProviderID, templateOffer, offerSMS(notice, n.deepLinkBase))
	default:
		return nil
	}
}

func offerBody(notice dispatch.OfferNotice) string {
	if notice.Reference != "" {
		return notice.Reference
	}
	return "Tens uma nova oferta. Abre a aplicação para responder."
}

func offerSMS(notice dispatch.OfferNotice, deepLinkBase string) string {
	return fmt.Sprintf("AGORA: novo pedido de %s perto de ti. Responde em %d s: %s/offers/%s", notice.Category, notice.ExpiresIn, deepLinkBase, notice.OfferID)
}

func (n *RequestNotifier) onOfferCancelled(ctx context.Context, env events.Envelope) error {
	var event events.RequestOfferV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	return n.pushTo(ctx, event.ProviderID, templateOfferCancelled, services.PushMessage{
		Title: "Oferta terminada", Body: "Este pedido já não está disponível.",
		Data: map[string]string{"type": "request.offer_cancelled", "offer_id": event.OfferID.String(), "event_id": env.EventID.String()},
	})
}

func (n *RequestNotifier) onTransition(ctx context.Context, env events.Envelope) error {
	var event events.RequestTransitionV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	message, known := statusMessages[env.EventType]
	if !known {
		return nil
	}
	data := map[string]string{"type": "request.status", "request_id": event.RequestID.String(), "status": event.To, "version": strconv.Itoa(event.Version), "event_id": env.EventID.String()}
	err := n.pushTo(ctx, event.CustomerID, templateStatus, services.PushMessage{Title: message.title, Body: message.body, Data: data})
	if env.EventType != events.RequestAccepted {
		return err
	}
	var failures []error
	failures = append(failures, err)
	if event.SMSOnAccept {
		failures = append(failures, n.smsToUser(ctx, event.CustomerID, templateAssigned, "AGORA: o teu pedido foi aceite. O prestador está a preparar-se para ir ter contigo."))
	}
	if event.PassengerPhone != "" {
		text, err := n.passengerText(ctx, event.RequestID)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		failures = append(failures, n.smsToPhone(ctx, nil, event.PassengerPhone, templateAssigned, text))
	}
	return errors.Join(failures...)
}

func (n *RequestNotifier) passengerText(ctx context.Context, requestID uuid.UUID) (string, error) {
	notice, err := n.requests.PassengerNotice(ctx, requestID)
	if err != nil {
		return "", err
	}
	if notice == nil {
		return "", dispatch.ErrRequestNotFound
	}
	link := ""
	if n.shares != nil {
		if link, err = n.shares.ShareLink(ctx, requestID); err != nil {
			return "", err
		}
	}
	return passengerSMS(notice, link), nil
}

func passengerSMS(notice *repository.PassengerNotice, link string) string {
	text := strings.NewReplacer("{provider}", notice.ProviderName, "{plate}", notice.VehiclePlate, "{link}", link).Replace(notice.Template)
	return strings.Join(strings.Fields(text), " ")
}

func (n *RequestNotifier) onQuote(ctx context.Context, env events.Envelope) error {
	var event events.QuoteSubmittedV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	return n.pushTo(ctx, event.CustomerID, templateQuote, services.PushMessage{
		Title: "Novo orçamento", Body: "Recebeste um orçamento para o teu pedido.",
		Data: map[string]string{"type": "request.quote", "request_id": event.RequestID.String(), "event_id": env.EventID.String()},
	})
}

func (n *RequestNotifier) onReminder(ctx context.Context, env events.Envelope) error {
	var event events.ReminderDueV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	message := services.PushMessage{
		Title: "Lembrete", Body: "O teu serviço está marcado para amanhã.",
		Data: map[string]string{"type": "request.reminder", "request_id": event.RequestID.String(), "event_id": env.EventID.String()},
	}
	failures := []error{n.pushTo(ctx, event.CustomerID, templateReminder, message)}
	if event.ProviderID != nil {
		failures = append(failures, n.pushTo(ctx, *event.ProviderID, templateReminder, message))
	}
	return errors.Join(failures...)
}

func (n *RequestNotifier) onOperatorAlert(ctx context.Context, env events.Envelope) error {
	var event events.OperatorAlertedV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	logger.FromContext(ctx).Warn("operator alert", zap.String("request_id", event.RequestID.String()), zap.String("reason", event.Reason), zap.String("category", event.Category))
	if n.operatorPhone == "" {
		return nil
	}
	return n.smsToPhone(ctx, nil, n.operatorPhone, templateOperator, fmt.Sprintf("AGORA operação: pedido %s de %s precisa de atenção (%s).", event.RequestID, event.Category, event.Reason))
}

var providerReviewMessages = map[string]struct{ title, body string }{
	events.ProviderApproved:  {"Conta aprovada", "A tua conta foi aprovada. Já podes ficar online."},
	events.ProviderRejected:  {"Corrige o teu perfil", "O teu perfil precisa de correções. Abre a aplicação para veres o motivo."},
	events.ProviderSuspended: {"Conta suspensa", "A tua conta foi suspensa. Abre a aplicação para veres o motivo."},
}

func (n *RequestNotifier) onProviderReview(ctx context.Context, env events.Envelope) error {
	var event events.ProviderReviewV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	message := providerReviewMessages[env.EventType]
	return errors.Join(
		n.pushTo(ctx, event.ProviderID, templateProviderReview, services.PushMessage{
			Title: message.title, Body: message.body, Data: map[string]string{"type": "provider.review", "event_id": env.EventID.String()},
		}),
		n.smsToUser(ctx, event.ProviderID, templateProviderReview, "AGORA: "+message.body),
	)
}

func (n *RequestNotifier) pushTo(ctx context.Context, userID uuid.UUID, template string, message services.PushMessage) error {
	tokens, err := n.devices.Tokens(ctx, userID)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		n.record(ctx, &userID, entities.ChannelFCM, template, statusSkipped, "", "no device")
		return nil
	}
	var failures []error
	for _, token := range tokens {
		message.Token = token
		id, err := n.push.Send(ctx, message)
		switch {
		case errors.Is(err, services.ErrPushTokenInvalid):
			if removeErr := n.devices.Remove(ctx, token); removeErr != nil {
				failures = append(failures, removeErr)
			}
			n.record(ctx, &userID, entities.ChannelFCM, template, statusFailed, "", err.Error())
		case err != nil:
			failures = append(failures, err)
			n.record(ctx, &userID, entities.ChannelFCM, template, statusFailed, "", err.Error())
		default:
			n.record(ctx, &userID, entities.ChannelFCM, template, statusSent, id, "")
		}
	}
	return errors.Join(failures...)
}

func (n *RequestNotifier) smsToUser(ctx context.Context, userID uuid.UUID, template, body string) error {
	_, phone, err := n.devices.Contact(ctx, userID)
	if err != nil {
		return err
	}
	if phone == "" {
		n.record(ctx, &userID, entities.ChannelSMS, template, statusSkipped, "", "no phone")
		return nil
	}
	return n.smsToPhone(ctx, &userID, phone, template, body)
}

func (n *RequestNotifier) smsToPhone(ctx context.Context, userID *uuid.UUID, phone, template, body string) error {
	if err := n.sms.Send(ctx, phone, body); err != nil {
		n.record(ctx, userID, entities.ChannelSMS, template, statusFailed, "", err.Error())
		return err
	}
	n.record(ctx, userID, entities.ChannelSMS, template, statusSent, "", "")
	return nil
}

func (n *RequestNotifier) record(ctx context.Context, userID *uuid.UUID, channel, template, status, messageID, failure string) {
	err := n.devices.RecordNotification(ctx, repository.NotificationRecord{UserID: userID, Channel: channel, Template: template, Status: status, ProviderMessageID: messageID, Error: failure})
	if err != nil {
		logger.FromContext(ctx).Warn("notification not recorded", zap.Error(err))
	}
}

var paymentMessages = map[string]services.PushMessage{
	string(entities.PaymentPaid):    {Title: "Pagamento confirmado", Body: "Recebemos o pagamento. Obrigado!"},
	string(entities.PaymentFailed):  {Title: "O pagamento falhou", Body: "Não conseguimos confirmar o pagamento. Tenta de novo."},
	string(entities.PaymentExpired): {Title: "O pagamento expirou", Body: "O prazo terminou. Faz um novo pagamento."},
}

func (n *RequestNotifier) onPayment(ctx context.Context, env events.Envelope) error {
	var event events.PaymentChangedV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	message, known := paymentMessages[event.Status]
	if !known {
		return nil
	}
	message.Data = map[string]string{"type": "request.payment", "request_id": event.RequestID.String(), "status": event.Status, "event_id": env.EventID.String()}
	failures := []error{n.pushTo(ctx, event.CustomerID, templatePayment, message)}
	if event.ProviderID != nil && event.Status == string(entities.PaymentPaid) {
		failures = append(failures, n.pushTo(ctx, *event.ProviderID, templatePayment, message))
	}
	return errors.Join(failures...)
}
