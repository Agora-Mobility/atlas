package containers

import (
	"context"
	"errors"
	"time"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/usecases/payments"
)

type prepaidRoutes struct {
	bookings payments.PrepaidHook
	removals payments.PrepaidHook
}

func (r prepaidRoutes) route(request *entities.ServiceRequest) payments.PrepaidHook {
	if request.WorkshopID != nil {
		return r.bookings
	}
	return r.removals
}

func (r prepaidRoutes) OnPrepaid(ctx context.Context, request *entities.ServiceRequest, now time.Time) error {
	hook := r.route(request)
	if hook == nil {
		return errors.New("no prepaid hook for this request")
	}
	return hook.OnPrepaid(ctx, request, now)
}

func (r prepaidRoutes) OnPrepayExpired(ctx context.Context, request *entities.ServiceRequest, now time.Time) error {
	hook := r.route(request)
	if hook == nil {
		return errors.New("no prepaid hook for this request")
	}
	return hook.OnPrepayExpired(ctx, request, now)
}
