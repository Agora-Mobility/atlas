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
	"github.com/Agora-Mobility/atlas/shared/enums"
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
	templateVerification   = "verification_reminder"
	templatePayment        = "payment_status"
	templateBooking        = "booking_status"
	templateBookingExtra   = "booking_extra"
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
	account       repository.IAccountRepository
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

func (n *RequestNotifier) UseAccount(account repository.IAccountRepository) {
	n.account = account
}

func (n *RequestNotifier) UseSharer(sharer TripSharer) {
	n.shares = sharer
}

func RequestBindingKeys() []string {
	types := make([]string, 0, len(statusMessages)+4)
	for eventType := range statusMessages {
		types = append(types, eventType)
	}
	slices.Sort(types)
	types = append(types, events.PaymentChanged, events.OfferCreated, events.OfferCancelled, events.QuoteSubmitted, events.ReminderDue, events.OperatorAlerted,
		events.ProviderApproved, events.ProviderRejected, events.ProviderSuspended, events.BookingChanged, events.BookingExtra,
		events.ProviderApplicationSubmitted, events.ProviderVerificationReminder)
	keys := make([]string, 0, len(types))
	for _, eventType := range types {
		keys = append(keys, events.RoutingKey(eventType, 1))
	}
	return keys
}

func (n *RequestNotifier) OnEvent(ctx context.Context, env events.Envelope) error {
	if err := n.addToInbox(ctx, env); err != nil {
		return err
	}
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
	case events.ProviderApproved, events.ProviderRejected, events.ProviderSuspended, events.ProviderApplicationSubmitted:
		return n.onProviderReview(ctx, env)
	case events.ProviderVerificationReminder:
		return n.onVerificationReminder(ctx, env)
	case events.PaymentChanged:
		return n.onPayment(ctx, env)
	case events.BookingChanged:
		return n.onBooking(ctx, env)
	case events.BookingExtra:
		return n.onBookingExtra(ctx, env)
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
	err := n.statusPush(ctx, event.CustomerID, templateStatus, services.PushMessage{Title: message.title, Body: message.body, Data: data})
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
	events.ProviderApproved:             {"Conta aprovada", "A tua conta foi aprovada. Já podes ficar online."},
	events.ProviderRejected:             {"Corrige o teu perfil", "O teu perfil precisa de correções. Abre a aplicação para veres o motivo."},
	events.ProviderSuspended:            {"Conta suspensa", "A tua conta foi suspensa. Abre a aplicação para veres o motivo."},
	events.ProviderApplicationSubmitted: {"Verificação enviada", "Recebemos os teus dados e documentos. Vamos analisá-los e avisamos-te."},
}

func (n *RequestNotifier) onVerificationReminder(ctx context.Context, env events.Envelope) error {
	var event events.ProviderVerificationReminderV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	title, body := "Registo criminal", "Falta enviar o registo criminal para concluíres a verificação."
	if event.Kind == entities.ReminderDocumentExpiry {
		title, body = "Documento a expirar", "Um dos teus documentos ("+entities.DocumentName(enums.DocumentType(event.DocumentType))+") está quase a expirar. Envia o novo na aplicação."
		if event.ExpiresOn != nil {
			body = "O documento " + entities.DocumentName(enums.DocumentType(event.DocumentType)) + " expira a " + event.ExpiresOn.Format("02/01/2006") + ". Envia o novo na aplicação."
		}
	}
	return n.pushTo(ctx, event.ProviderID, templateVerification, services.PushMessage{
		Title: title, Body: body, Data: map[string]string{"type": "provider.verification", "kind": event.Kind, "document_type": event.DocumentType, "event_id": env.EventID.String()},
	})
}

func (n *RequestNotifier) onProviderReview(ctx context.Context, env events.Envelope) error {
	var event events.ProviderReviewV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	message := providerReviewMessages[env.EventType]
	push := n.pushTo(ctx, event.ProviderID, templateProviderReview, services.PushMessage{
		Title: message.title, Body: message.body, Data: map[string]string{"type": "provider.review", "event_id": env.EventID.String()},
	})
	if env.EventType == events.ProviderApplicationSubmitted {
		return push
	}
	return errors.Join(push, n.smsToUser(ctx, event.ProviderID, templateProviderReview, "AGORA: "+message.body))
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

func (n *RequestNotifier) preferencesOf(ctx context.Context, userID uuid.UUID) (*entities.Preferences, error) {
	if n.account == nil {
		return nil, nil
	}
	return n.account.Preferences(ctx, userID)
}

func (n *RequestNotifier) statusPush(ctx context.Context, userID uuid.UUID, template string, message services.PushMessage) error {
	preferences, err := n.preferencesOf(ctx, userID)
	if err != nil {
		return err
	}
	if preferences != nil && !preferences.AllowsStatusPush() {
		n.record(ctx, &userID, entities.ChannelFCM, template, statusSkipped, "", "the user turned this off")
		return nil
	}
	return n.pushTo(ctx, userID, template, message)
}

func (n *RequestNotifier) smsToUser(ctx context.Context, userID uuid.UUID, template, body string) error {
	if template != templateOffer {
		preferences, err := n.preferencesOf(ctx, userID)
		if err != nil {
			return err
		}
		if preferences != nil && !preferences.AllowsSMS() {
			n.record(ctx, &userID, entities.ChannelSMS, template, statusSkipped, "", "the user turned this off")
			return nil
		}
	}
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

var bookingMessages = map[string]struct{ title, body string }{
	"awaiting_payment": {"Marcação à espera de pagamento", "Paga a referência para confirmar a marcação."},
	"confirmed":        {"Marcação confirmada", "A tua marcação está confirmada."},
	"vehicle_received": {"Viatura recebida", "A oficina recebeu a tua viatura."},
	"in_service":       {"Em serviço", "A tua viatura está em serviço."},
	"ready":            {"Pronta para levantar", "A viatura está pronta. Aprova o serviço para pagar."},
	"completed":        {"Serviço concluído", "Obrigado! Já podes pagar e avaliar."},
	"cancelled":        {"Marcação cancelada", "A marcação foi cancelada."},
}

func (n *RequestNotifier) onBooking(ctx context.Context, env events.Envelope) error {
	var event events.BookingChangedV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	message, known := bookingMessages[event.Status]
	if !known {
		return nil
	}
	err := n.statusPush(ctx, event.CustomerID, templateBooking, services.PushMessage{
		Title: message.title, Body: message.body,
		Data: map[string]string{"type": "booking.status", "booking_id": event.BookingID.String(), "request_id": event.RequestID.String(), "status": event.Status, "event_id": env.EventID.String()},
	})
	if event.Status != "confirmed" {
		return err
	}
	sms := fmt.Sprintf("AGORA: marcação %s confirmada em %s, %s.", event.Code, event.WorkshopName, event.StartsAt.In(bookingZone).Format("02/01 15:04"))
	return errors.Join(err, n.smsToUser(ctx, event.CustomerID, templateBooking, sms))
}

func (n *RequestNotifier) onBookingExtra(ctx context.Context, env events.Envelope) error {
	var event events.BookingExtraV1
	if err := env.DecodePayload(&event); err != nil {
		return err
	}
	return n.statusPush(ctx, event.CustomerID, templateBookingExtra, services.PushMessage{
		Title: "Orçamento extra", Body: event.Description,
		Data: map[string]string{"type": "booking.extra", "booking_id": event.BookingID.String(), "extra_id": event.ExtraID.String(), "event_id": env.EventID.String()},
	})
}

var bookingZone = time.FixedZone("WAT", 3600)

const (
	linkRequest = "request"
	linkOffer   = "offer"
	linkBooking = "booking"
)

func (n *RequestNotifier) addToInbox(ctx context.Context, env events.Envelope) error {
	if n.account == nil {
		return nil
	}
	item, err := inboxItemOf(env)
	if err != nil || item == nil {
		return err
	}
	if item.ID, err = uuid.NewV7(); err != nil {
		return err
	}
	item.EventID, item.CreatedAt = env.EventID.String(), time.Now().UTC()
	return n.account.AddInboxItem(ctx, item)
}

func inboxItemOf(env events.Envelope) (*entities.InboxItem, error) {
	switch env.EventType {
	case events.OfferCreated:
		var event events.RequestOfferV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &entities.InboxItem{UserID: event.ProviderID, Kind: "offer", Title: "Novo pedido", Body: "Tens uma nova oferta.", LinkType: linkOffer, LinkID: &event.OfferID}, nil
	case events.BookingChanged:
		var event events.BookingChangedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		message, known := bookingMessages[event.Status]
		if !known {
			return nil, nil
		}
		return &entities.InboxItem{UserID: event.CustomerID, Kind: "booking", Title: message.title, Body: message.body, LinkType: linkBooking, LinkID: &event.BookingID}, nil
	case events.BookingExtra:
		var event events.BookingExtraV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		return &entities.InboxItem{UserID: event.CustomerID, Kind: "booking", Title: "Orçamento extra", Body: event.Description, LinkType: linkBooking, LinkID: &event.BookingID}, nil
	case events.PaymentChanged:
		var event events.PaymentChangedV1
		if err := env.DecodePayload(&event); err != nil {
			return nil, err
		}
		message, known := paymentInbox[event.Status]
		if !known {
			return nil, nil
		}
		return &entities.InboxItem{UserID: event.CustomerID, Kind: "payment", Title: message.title, Body: message.body, LinkType: linkRequest, LinkID: &event.RequestID}, nil
	}
	message, known := statusMessages[env.EventType]
	if !known {
		return nil, nil
	}
	var event events.RequestTransitionV1
	if err := env.DecodePayload(&event); err != nil {
		return nil, err
	}
	return &entities.InboxItem{UserID: event.CustomerID, Kind: "request", Title: message.title, Body: message.body, LinkType: linkRequest, LinkID: &event.RequestID}, nil
}

var paymentInbox = map[string]struct{ title, body string }{
	"paid":    {"Pagamento confirmado", "Recebemos o teu pagamento."},
	"failed":  {"Pagamento recusado", "O pagamento não foi aceite. Tenta de novo."},
	"expired": {"Pagamento expirado", "O prazo do pagamento terminou."},
}
