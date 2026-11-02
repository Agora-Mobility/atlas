package dispatch

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/simplepdf"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

const centsPerUnit = 100

func money(cents int64, currency string) string {
	negative := ""
	if cents < 0 {
		negative, cents = "-", -cents
	}
	whole := fmt.Sprint(cents / centsPerUnit)
	var grouped strings.Builder
	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(' ')
		}
		grouped.WriteRune(digit)
	}
	symbol := currency
	if currency == "AOA" {
		symbol = "Kz"
	}
	return fmt.Sprintf("%s%s,%02d %s", negative, grouped.String(), cents%centsPerUnit, symbol)
}

func (u *Usecase) Receipt(ctx context.Context, userID, requestID uuid.UUID) ([]byte, string, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, "", err
	}
	if request == nil || !request.IsCustomer(userID) {
		return nil, "", ErrRequestNotFound
	}
	if request.Status != entities.RequestCompleted {
		return nil, "", ErrReceiptNotReady
	}
	view, err := u.customerView(ctx, request)
	if err != nil {
		return nil, "", err
	}
	zone, err := u.Config.FindZone(ctx, request.ZoneID)
	if err != nil {
		return nil, "", err
	}
	location := time.UTC
	if zone != nil {
		location = zone.Location()
	}
	return simplepdf.Render(receiptLines(view, location)), request.PublicCode, nil
}

func receiptLines(view *dtos.RequestView, location *time.Location) []simplepdf.Line {
	lines := []simplepdf.Line{{Text: "AGORA", Bold: true, Size: 18}, {Text: "Recibo do pedido " + view.Code, Bold: true, Size: 13}, {Text: "Serviço: " + view.ServiceName}}
	stamp := func(at *time.Time) string {
		if at == nil {
			return ""
		}
		return at.In(location).Format("02/01/2006 15:04")
	}
	lines = append(lines, simplepdf.Line{Text: "Concluído em: " + stamp(view.CompletedAt)})
	if view.Provider != nil {
		lines = append(lines, simplepdf.Line{Text: "Prestador: " + view.Provider.Name + ", " + view.Provider.VehiclePlate})
	}
	lines = append(lines, simplepdf.Line{Text: "Recolha: " + placeLabel(view.Pickup)})
	for i := range view.Stops {
		text := "Paragem: " + placeLabel(view.Stops[i])
		if view.Stops[i].ArrivedAt != nil {
			text += " (" + stamp(view.Stops[i].ArrivedAt) + ")"
		}
		lines = append(lines, simplepdf.Line{Text: text})
	}
	if view.Destination != nil {
		lines = append(lines, simplepdf.Line{Text: "Destino: " + placeLabel(*view.Destination)})
	}
	lines = append(lines, simplepdf.Line{Text: ""})
	breakdown := view.Breakdown
	for i := range breakdown.Items {
		lines = append(lines, simplepdf.Line{Text: breakdown.Items[i].Label + ": " + money(breakdown.Items[i].AmountCents, breakdown.Currency)})
	}
	lines = append(lines, simplepdf.Line{Text: "Total: " + money(breakdown.TotalCents, breakdown.Currency), Bold: true, Size: 13}, simplepdf.Line{Text: "Método: " + breakdown.Method + " | Pago: " + money(breakdown.PaidCents, breakdown.Currency)})
	if breakdown.TipCents > 0 {
		lines = append(lines, simplepdf.Line{Text: "Gorjeta: " + money(breakdown.TipCents, breakdown.Currency)})
	}
	if breakdown.RefundedCents > 0 {
		lines = append(lines, simplepdf.Line{Text: "Reembolsado: " + money(breakdown.RefundedCents, breakdown.Currency)})
	}
	return lines
}

func placeLabel(place dtos.PlaceView) string {
	if place.Address != "" {
		return place.Address
	}
	if place.Reference != "" {
		return place.Reference
	}
	return fmt.Sprintf("%.5f, %.5f", place.Lat, place.Lng)
}

func placeInput(place entities.RequestPlace) dtos.PlaceInput {
	return dtos.PlaceInput{Lat: place.Point.Lat, Lng: place.Point.Lng, Reference: place.Reference, Address: place.Address}
}

func (u *Usecase) RepeatRequest(ctx context.Context, customerID, requestID uuid.UUID) (*dtos.RepeatView, error) {
	request, err := u.Requests.Find(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if request == nil || !request.IsCustomer(customerID) {
		return nil, ErrRequestNotFound
	}
	view := &dtos.RepeatView{ServiceID: request.ServiceID, Category: request.CategorySlug, Pickup: placeInput(request.Pickup), Stops: make([]dtos.PlaceInput, 0, len(request.Stops))}
	if request.Destination != nil {
		destination := placeInput(*request.Destination)
		view.Destination = &destination
	}
	for i := range request.Stops {
		view.Stops = append(view.Stops, placeInput(request.Stops[i]))
	}
	estimates, err := u.CreateEstimates(ctx, customerID, dtos.FareEstimateInput{Category: request.CategorySlug, Pickup: view.Pickup, Destination: view.Destination, Stops: view.Stops})
	if err != nil {
		return nil, err
	}
	view.Estimates = estimates.Items
	return view, nil
}

func (u *Usecase) ArriveAtStop(ctx context.Context, providerID, requestID uuid.UUID, position int) (*dtos.RequestView, error) {
	var request *entities.ServiceRequest
	err := u.Tx.WithinTransaction(ctx, func(ctx context.Context) error {
		var err error
		if request, err = u.Requests.FindForUpdate(ctx, requestID); err != nil {
			return err
		}
		if request == nil || !request.IsProvider(providerID) {
			return ErrRequestNotFound
		}
		if request.Status != entities.RequestInProgress {
			return entities.ErrInvalidTransition
		}
		found, err := u.Requests.MarkStopArrived(ctx, requestID, position, u.now())
		if err != nil {
			return err
		}
		if !found {
			return ErrStopNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if request, err = u.Requests.Find(ctx, requestID); err != nil {
		return nil, err
	}
	return u.providerView(ctx, request)
}

func (u *Usecase) RecentDestinations(ctx context.Context, customerID uuid.UUID, limit int) (*dtos.RecentDestinationList, error) {
	rows, err := u.Requests.RecentDestinations(ctx, customerID, limit)
	if err != nil {
		return nil, err
	}
	list := &dtos.RecentDestinationList{Items: make([]dtos.RecentDestination, 0, len(rows))}
	for i := range rows {
		list.Items = append(list.Items, dtos.RecentDestination{Lat: rows[i].Point.Lat, Lng: rows[i].Point.Lng, Address: rows[i].Line, Reference: rows[i].Reference, UsedAt: rows[i].UsedAt})
	}
	return list, nil
}
