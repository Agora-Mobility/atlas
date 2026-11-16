package bookings

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/bookings"
	"github.com/Agora-Mobility/atlas/usecases/bookings/dtos"
)

type Controller struct {
	bookings *bookings.Usecase
}

func NewController(uc *bookings.Usecase) *Controller {
	return &Controller{bookings: uc}
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{entities.ErrBookingNotFound, http.StatusNotFound, "BOOKING_NOT_FOUND", "Booking not found."},
	{entities.ErrWorkshopUnavailable, http.StatusNotFound, "WORKSHOP_NOT_FOUND", "Workshop not found."},
	{entities.ErrWorkshopServiceInvalid, http.StatusUnprocessableEntity, "WORKSHOP_SERVICE_NOT_FOUND", "A service does not exist in this workshop."},
	{entities.ErrBookingTransition, http.StatusConflict, "BOOKING_STATE_INVALID", "This booking cannot move to that state."},
	{entities.ErrBookingSlotFull, http.StatusConflict, "BOOKING_SLOT_FULL", "This slot is no longer free."},
	{entities.ErrBookingSlotInvalid, http.StatusUnprocessableEntity, "BOOKING_SLOT_INVALID", "The workshop does not take bookings at this time."},
	{entities.ErrBookingExtrasPending, http.StatusConflict, "BOOKING_EXTRAS_PENDING", "The customer still has to answer an extra."},
	{entities.ErrBookingExtraNotFound, http.StatusNotFound, "EXTRA_NOT_FOUND", "Extra not found."},
	{entities.ErrBookingExtraAnswered, http.StatusConflict, "EXTRA_ALREADY_ANSWERED", "This extra was already answered."},
	{entities.ErrBookingNotCancellable, http.StatusConflict, "BOOKING_NOT_CANCELLABLE", "The vehicle is already at the workshop."},
	{entities.ErrHomeServiceUnavailable, http.StatusUnprocessableEntity, "HOME_SERVICE_UNAVAILABLE", "This workshop does not offer home service."},
	{entities.ErrMechanicNotFound, http.StatusNotFound, "MECHANIC_NOT_FOUND", "Mechanic not found."},
	{entities.ErrPhotoLimit, http.StatusUnprocessableEntity, "PHOTO_LIMIT", "Too many photos."},
	{entities.ErrPhotoNotFound, http.StatusNotFound, "PHOTO_NOT_FOUND", "Photo not found."},
	{entities.ErrPhotoNotUploaded, http.StatusUnprocessableEntity, "PHOTO_NOT_UPLOADED", "The photo was not uploaded or does not match what was announced."},
	{entities.ErrPaymentOutstanding, http.StatusConflict, "PAYMENT_OUTSTANDING", "There is a request waiting to be paid."},
	{entities.ErrPaymentMethodUnavailable, http.StatusUnprocessableEntity, "PAYMENT_METHOD_UNAVAILABLE", "This payment method is not available."},
	{entities.ErrCallNotAvailable, http.StatusConflict, "CALL_NOT_AVAILABLE", "A call is not available now."},
	{services.ErrCallsUnavailable, http.StatusServiceUnavailable, "CALLS_UNAVAILABLE", "Calls are not available."},
	{bookings.ErrOutsideServiceArea, http.StatusUnprocessableEntity, "OUTSIDE_SERVICE_AREA", "This place is outside the service area."},
	{bookings.ErrServiceNotOffered, http.StatusUnprocessableEntity, "SERVICE_NOT_OFFERED", "This service is not offered here."},
	{bookings.ErrBadDate, http.StatusBadRequest, "INVALID_DATE", "The date is not valid."},
	{bookings.ErrTooFarAhead, http.StatusUnprocessableEntity, "BOOKING_TOO_FAR", "This date is too far ahead."},
	{bookings.ErrVehicleRequired, http.StatusUnprocessableEntity, "VEHICLE_NOT_FOUND", "Choose one of your vehicles."},
	{bookings.ErrHomeAddress, http.StatusUnprocessableEntity, "HOME_ADDRESS_REQUIRED", "A home service needs the address."},
	{bookings.ErrReasonNotValid, http.StatusUnprocessableEntity, "REASON_NOT_VALID", "This reason does not apply."},
	{bookings.ErrChatClosed, http.StatusConflict, "CHAT_CLOSED", "The chat of this booking is closed."},
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		var outstanding *entities.OutstandingPaymentError
		if errors.As(err, &outstanding) {
			shared.PaymentOutstanding(ctx, outstanding)
			return
		}
		for _, mapped := range errorTable {
			if errors.Is(err, mapped.target) {
				shared.RespondError(ctx, mapped.status, mapped.code, mapped.message)
				return
			}
		}
		if validationErr, ok := customerrors.AsValidation(err); ok {
			shared.ValidationFailed(ctx, validationErr)
			return
		}
		shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
		return
	}
	if body == nil {
		ctx.Status(status)
		return
	}
	ctx.JSON(status, body)
}

func customer(ctx *gin.Context) (uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	if role, _ := middleware.RoleFromContext(reqCtx); role != enums.RoleCustomer {
		shared.Forbidden(ctx, "WRONG_ACCOUNT_TYPE", "This action is not available to this account.")
		return uuid.Nil, false
	}
	return userID, true
}

func staff(ctx *gin.Context) (entities.Actor, uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	access, ok := middleware.AccessFromContext(reqCtx)
	if !ok {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return entities.Actor{}, uuid.Nil, false
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	actor := access.Actor(role, ctx.ClientIP(), ctx.Request.UserAgent())
	raw := ctx.Param("orgId")
	if raw == "" {
		if access.Scope != enums.OrganizationTypePartner {
			shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
			return actor, uuid.Nil, false
		}
		return actor, access.OrganizationID, true
	}
	if access.Scope != enums.OrganizationTypePlatform {
		shared.Forbidden(ctx, middleware.CodePermissionDenied, "You do not have permission to perform this action.")
		return actor, uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		shared.NotFound(ctx, "PARTNER_NOT_FOUND", "Partner not found.")
		return actor, uuid.Nil, false
	}
	return actor, id, true
}

func idParam(ctx *gin.Context, name, code, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param(name))
	if err != nil {
		shared.NotFound(ctx, code, message)
		return uuid.Nil, false
	}
	return id, true
}

func bookingParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "id", "BOOKING_NOT_FOUND", "Booking not found.")
}

func workshopParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "id", "WORKSHOP_NOT_FOUND", "Workshop not found.")
}

func pageOf(ctx *gin.Context) (pagination.Params, bool) {
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return page, false
	}
	return page, true
}

func floatQuery(ctx *gin.Context, name string, minimum, maximum float64) (float64, bool) {
	raw := ctx.Query(name)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < minimum || value > maximum {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid "+name+".")
		return 0, false
	}
	return value, true
}

// SearchWorkshops godoc
//
//	@Summary		List workshops for a customer
//	@Description	Search by name, filter by category code, distance, rating, favorites and home service. Each card has the calculated open state and the promotions the customer can still use.
//	@Tags			Maintenance
//	@Produce		json
//	@Param			lat				query		number	false	"Latitude"
//	@Param			lng				query		number	false	"Longitude"
//	@Param			search			query		string	false	"Name"
//	@Param			category		query		string	false	"Category code"
//	@Param			max_distance_km	query		number	false	"Radius"
//	@Param			min_rating		query		number	false	"Minimum stars"
//	@Param			favorites		query		bool	false	"Only favorites"
//	@Param			home_service	query		bool	false	"Only with home service"
//	@Param			sort			query		string	false	"distance or rating"
//	@Success		200				{object}	dtos.WorkshopCardList
//	@Failure		400				{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/workshops [get]
func (c *Controller) SearchWorkshops(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	page, ok := pageOf(ctx)
	if !ok {
		return
	}
	lat, ok := floatQuery(ctx, "lat", -90, 90)
	if !ok {
		return
	}
	lng, ok := floatQuery(ctx, "lng", -180, 180)
	if !ok {
		return
	}
	distance, ok := floatQuery(ctx, "max_distance_km", 0, 20000)
	if !ok {
		return
	}
	rating, ok := floatQuery(ctx, "min_rating", 0, 5)
	if !ok {
		return
	}
	list, err := c.bookings.Search(ctx.Request.Context(), userID, bookings.SearchInput{
		Lat: lat, Lng: lng, Search: ctx.Query("search"), Category: ctx.Query("category"), OnlyFavorites: ctx.Query("favorites") == "true", OnlyHome: ctx.Query("home_service") == "true",
		MinRating: rating, MaxDistanceKm: distance, SortByRating: ctx.Query("sort") == "rating",
	}, page)
	respond(ctx, http.StatusOK, list, err)
}

// GetWorkshop godoc
//
//	@Summary		Read a workshop
//	@Description	About, warranty, structured hours and active services with price and duration.
//	@Tags			Maintenance
//	@Produce		json
//	@Param			id	path		string	true	"Workshop id"
//	@Param			lat	query		number	false	"Latitude"
//	@Param			lng	query		number	false	"Longitude"
//	@Success		200	{object}	dtos.WorkshopDetailView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/workshops/{id} [get]
func (c *Controller) GetWorkshop(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	lat, ok := floatQuery(ctx, "lat", -90, 90)
	if !ok {
		return
	}
	lng, ok := floatQuery(ctx, "lng", -180, 180)
	if !ok {
		return
	}
	view, err := c.bookings.Detail(ctx.Request.Context(), userID, id, lat, lng)
	respond(ctx, http.StatusOK, view, err)
}

// WorkshopReviews godoc
//
//	@Summary	List the reviews of a workshop
//	@Tags		Maintenance
//	@Produce	json
//	@Param		id	path		string	true	"Workshop id"
//	@Success	200	{object}	dtos.ReviewList
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/workshops/{id}/reviews [get]
func (c *Controller) WorkshopReviews(ctx *gin.Context) {
	if _, ok := customer(ctx); !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	page, ok := pageOf(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.Reviews(ctx.Request.Context(), id, page)
	respond(ctx, http.StatusOK, list, err)
}

// AddFavorite godoc
//
//	@Summary	Add a workshop to the favorites
//	@Tags		Maintenance
//	@Produce	json
//	@Param		id	path	string	true	"Workshop id"
//	@Success	204	"Done"
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/workshops/{id}/favorite [post]
func (c *Controller) AddFavorite(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.bookings.SetFavorite(ctx.Request.Context(), userID, id, true))
}

// RemoveFavorite godoc
//
//	@Summary	Remove a workshop from the favorites
//	@Tags		Maintenance
//	@Produce	json
//	@Param		id	path	string	true	"Workshop id"
//	@Success	204	"Done"
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/workshops/{id}/favorite [delete]
func (c *Controller) RemoveFavorite(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.bookings.SetFavorite(ctx.Request.Context(), userID, id, false))
}

// WorkshopCalendar godoc
//
//	@Summary		Days and free slots of a workshop
//	@Description	Status free, closed or full per day, with the slots where the whole duration of the chosen services fits within the capacity.
//	@Tags			Maintenance
//	@Produce		json
//	@Param			id			path		string	true	"Workshop id"
//	@Param			service_ids	query		string	true	"Comma separated service ids"
//	@Param			from		query		string	true	"First day, YYYY-MM-DD"
//	@Param			days		query		int		false	"Number of days, default 1, at most 62"
//	@Success		200			{object}	dtos.CalendarView
//	@Failure		400			{object}	shared.ErrorResponse
//	@Failure		404			{object}	shared.ErrorResponse
//	@Failure		422			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/workshops/{id}/availability [get]
func (c *Controller) WorkshopCalendar(ctx *gin.Context) {
	if _, ok := customer(ctx); !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	var ids []uuid.UUID
	for _, raw := range strings.Split(ctx.Query("service_ids"), ",") {
		serviceID, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid service_ids.")
			return
		}
		ids = append(ids, serviceID)
	}
	days := 1
	if raw := ctx.Query("days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid days.")
			return
		}
		days = parsed
	}
	view, err := c.bookings.Calendar(ctx.Request.Context(), id, ids, ctx.Query("from"), days)
	respond(ctx, http.StatusOK, view, err)
}

// CreateBooking godoc
//
//	@Summary		Book a workshop
//	@Description	Needs an Idempotency-Key. The price comes from the services of the workshop, the home fee and the best promotion still unused by the customer. If two customers race for the last place of a slot, the second gets 409 BOOKING_SLOT_FULL. With a reference the booking waits for the payment; with Express or wallet it confirms now and is charged after the customer approves the finished work.
//	@Tags			Maintenance
//	@Accept			json
//	@Produce		json
//	@Param			Idempotency-Key	header		string					true	"Unique key"
//	@Param			body			body		dtos.CreateBookingInput	true	"Body"
//	@Success		201				{object}	dtos.BookingView
//	@Failure		409				{object}	shared.ErrorResponse
//	@Failure		422				{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/bookings [post]
func (c *Controller) CreateBooking(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	var in dtos.CreateBookingInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, created, err := c.bookings.Create(ctx.Request.Context(), userID, ctx.GetHeader("Idempotency-Key"), in)
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	respond(ctx, status, view, err)
}

// ListBookings godoc
//
//	@Summary	List my bookings
//	@Tags		Maintenance
//	@Produce	json
//	@Success	200	{object}	dtos.BookingList
//	@Failure	400	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/bookings [get]
func (c *Controller) ListBookings(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	page, ok := pageOf(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.List(ctx.Request.Context(), userID, page)
	respond(ctx, http.StatusOK, list, err)
}

// GetBooking godoc
//
//	@Summary	Read my booking
//	@Tags		Maintenance
//	@Produce	json
//	@Param		id	path		string	true	"Booking id"
//	@Success	200	{object}	dtos.BookingView
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/bookings/{id} [get]
func (c *Controller) GetBooking(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	view, err := c.bookings.Get(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// BookingCalendarFile godoc
//
//	@Summary		Download the booking as a calendar file
//	@Description	Text/calendar (.ics).
//	@Tags			Maintenance
//	@Produce		json
//	@Param			id	path	string	true	"Booking id"
//	@Success		200	"Done"
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/bookings/{id}/calendar [get]
func (c *Controller) BookingCalendarFile(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	data, code, err := c.bookings.CalendarFile(ctx.Request.Context(), userID, id)
	if err != nil {
		respond(ctx, http.StatusOK, nil, err)
		return
	}
	ctx.Header("Content-Disposition", "attachment; filename=\""+code+".ics\"")
	ctx.Data(http.StatusOK, "text/calendar; charset=utf-8", data)
}

// CancelBooking godoc
//
//	@Summary		Cancel my booking
//	@Description	Free until the vehicle arrives at the workshop. What was paid ahead by reference goes to the wallet.
//	@Tags			Maintenance
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Booking id"
//	@Param			body	body		dtos.CancelBookingInput	true	"Body"
//	@Success		200		{object}	dtos.BookingView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/bookings/{id}/cancel [post]
func (c *Controller) CancelBooking(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.CancelBookingInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.Cancel(ctx.Request.Context(), userID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// AnswerExtra godoc
//
//	@Summary		Approve or decline an extra budget
//	@Description	Only an approved extra enters the total.
//	@Tags			Maintenance
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Booking id"
//	@Param			extraId	path		string					true	"Extra id"
//	@Param			body	body		dtos.AnswerExtraInput	true	"Body"
//	@Success		200		{object}	dtos.BookingView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/bookings/{id}/extras/{extraId}/answer [post]
func (c *Controller) AnswerExtra(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	extraID, ok := idParam(ctx, "extraId", "EXTRA_NOT_FOUND", "Extra not found.")
	if !ok {
		return
	}
	var in dtos.AnswerExtraInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.AnswerExtra(ctx.Request.Context(), userID, id, extraID, *in.Approve)
	respond(ctx, http.StatusOK, view, err)
}

// ApproveBooking godoc
//
//	@Summary		Approve the finished work
//	@Description	Moves a ready booking to completed. Only from here can the service be paid through POST /requests/{request_id}/payments.
//	@Tags			Maintenance
//	@Produce		json
//	@Param			id	path		string	true	"Booking id"
//	@Success		200	{object}	dtos.BookingView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Failure		409	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/bookings/{id}/approve [post]
func (c *Controller) ApproveBooking(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	view, err := c.bookings.Approve(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// SendBookingMessage godoc
//
//	@Summary	Send a message to the workshop
//	@Tags		Maintenance
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string				true	"Booking id"
//	@Param		body	body		dtos.MessageInput	true	"Body"
//	@Success	201		{object}	dtos.MessageView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	409		{object}	shared.ErrorResponse
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/bookings/{id}/messages [post]
func (c *Controller) SendBookingMessage(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.MessageInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.CustomerSend(ctx.Request.Context(), userID, id, in)
	respond(ctx, http.StatusCreated, view, err)
}

// BookingMessages godoc
//
//	@Summary	Read the messages with the workshop
//	@Tags		Maintenance
//	@Produce	json
//	@Param		id	path		string	true	"Booking id"
//	@Success	200	{object}	dtos.MessageList
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/bookings/{id}/messages [get]
func (c *Controller) BookingMessages(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	page, ok := pageOf(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.CustomerMessages(ctx.Request.Context(), userID, id, page)
	respond(ctx, http.StatusOK, list, err)
}

// CallWorkshop godoc
//
//	@Summary		Call the workshop through a masked number
//	@Description	Answers 503 CALLS_UNAVAILABLE without a provider.
//	@Tags			Maintenance
//	@Produce		json
//	@Param			id	path		string	true	"Booking id"
//	@Success		200	{object}	dtos.CallView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Failure		409	{object}	shared.ErrorResponse
//	@Failure		503	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/bookings/{id}/call [post]
func (c *Controller) CallWorkshop(ctx *gin.Context) {
	userID, ok := customer(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	view, err := c.bookings.Call(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// SetSchedule godoc
//
//	@Summary	Set capacity, hours, about and warranty
//	@Tags		Workshop operations
//	@Accept		json
//	@Produce	json
//	@Param		id		path	string				true	"Workshop id"
//	@Param		body	body	dtos.ScheduleInput	true	"Body"
//	@Success	204		"Done"
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	422		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/schedule [put]
func (c *Controller) SetSchedule(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	var in dtos.ScheduleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.bookings.SetSchedule(ctx.Request.Context(), actor, org, id, in))
}

// AddClosure godoc
//
//	@Summary	Block a day
//	@Tags		Workshop operations
//	@Accept		json
//	@Produce	json
//	@Param		id		path	string				true	"Workshop id"
//	@Param		body	body	dtos.ClosureInput	true	"Body"
//	@Success	204		"Done"
//	@Failure	400		{object}	shared.ErrorResponse
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/closures [post]
func (c *Controller) AddClosure(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	var in dtos.ClosureInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.bookings.AddClosure(ctx.Request.Context(), actor, org, id, in))
}

// RemoveClosure godoc
//
//	@Summary	Open a blocked day again
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id		path	string	true	"Workshop id"
//	@Param		date	path	string	true	"YYYY-MM-DD"
//	@Success	204		"Done"
//	@Failure	400		{object}	shared.ErrorResponse
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/closures/{date} [delete]
func (c *Controller) RemoveClosure(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.bookings.RemoveClosure(ctx.Request.Context(), actor, org, id, ctx.Param("date")))
}

// WorkshopPhotos godoc
//
//	@Summary	List the photos of a workshop
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id	path		string	true	"Workshop id"
//	@Success	200	{object}	dtos.WorkshopPhotoList
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/photos [get]
func (c *Controller) WorkshopPhotos(ctx *gin.Context) {
	_, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.WorkshopPhotos(ctx.Request.Context(), org, id)
	respond(ctx, http.StatusOK, list, err)
}

// SetWorkshopPhotos godoc
//
//	@Summary		Set the photos of a workshop
//	@Description	Upload each photo with POST /api/v1/media/photos, then send the full gallery in order: the first photo is the cover shown in GET /api/v1/workshops. Photos left out are removed from the gallery and photos already in it may stay. At most max_workshop_photos photos.
//	@Tags			Workshop operations
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"Workshop id"
//	@Param			body	body		dtos.WorkshopPhotosInput	true	"Body"
//	@Success		200		{object}	dtos.WorkshopPhotoList
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id}/photos [put]
func (c *Controller) SetWorkshopPhotos(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	var in dtos.WorkshopPhotosInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	list, err := c.bookings.SetWorkshopPhotos(ctx.Request.Context(), actor, org, id, in)
	respond(ctx, http.StatusOK, list, err)
}

// ListMechanics godoc
//
//	@Summary	List the mechanics of a workshop
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id	path		string	true	"Workshop id"
//	@Success	200	{object}	dtos.MechanicList
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/mechanics [get]
func (c *Controller) ListMechanics(ctx *gin.Context) {
	_, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.Mechanics(ctx.Request.Context(), org, id)
	respond(ctx, http.StatusOK, list, err)
}

// AddMechanic godoc
//
//	@Summary		Register a mechanic
//	@Description	A mechanic is a record of the workshop, without an account.
//	@Tags			Workshop operations
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Workshop id"
//	@Param			body	body		dtos.MechanicInput	true	"Body"
//	@Success		201		{object}	dtos.MechanicView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/workshops/{id}/mechanics [post]
func (c *Controller) AddMechanic(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	var in dtos.MechanicInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.AddMechanic(ctx.Request.Context(), actor, org, id, in)
	respond(ctx, http.StatusCreated, view, err)
}

// RemoveMechanic godoc
//
//	@Summary	Deactivate a mechanic
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id			path	string	true	"Workshop id"
//	@Param		mechanicId	path	string	true	"Mechanic id"
//	@Success	204			"Done"
//	@Failure	404			{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/mechanics/{mechanicId} [delete]
func (c *Controller) RemoveMechanic(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	mechanicID, ok := idParam(ctx, "mechanicId", "MECHANIC_NOT_FOUND", "Mechanic not found.")
	if !ok {
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.bookings.RemoveMechanic(ctx.Request.Context(), actor, org, id, mechanicID))
}

// WorkshopBookings godoc
//
//	@Summary	List the bookings of a workshop
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id		path		string	true	"Workshop id"
//	@Param		status	query		string	false	"Status"
//	@Success	200		{object}	dtos.BookingList
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/workshops/{id}/bookings [get]
func (c *Controller) WorkshopBookings(ctx *gin.Context) {
	_, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := workshopParam(ctx)
	if !ok {
		return
	}
	page, ok := pageOf(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.WorkshopBookings(ctx.Request.Context(), org, id, ctx.Query("status"), page)
	respond(ctx, http.StatusOK, list, err)
}

// PartnerBooking godoc
//
//	@Summary	Read a booking of the partner
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id	path		string	true	"Booking id"
//	@Success	200	{object}	dtos.BookingView
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/bookings/{id} [get]
func (c *Controller) PartnerBooking(ctx *gin.Context) {
	_, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	view, err := c.bookings.GetForWorkshop(ctx.Request.Context(), org, id)
	respond(ctx, http.StatusOK, view, err)
}

// MoveBooking godoc
//
//	@Summary		Move a booking to its next state
//	@Description	Confirmed to vehicle_received to in_service to ready, without skipping. Ready is refused while an extra waits for an answer.
//	@Tags			Workshop operations
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Booking id"
//	@Param			body	body		dtos.MoveBookingInput	true	"Body"
//	@Success		200		{object}	dtos.BookingView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/bookings/{id}/status [post]
func (c *Controller) MoveBooking(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.MoveBookingInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.Move(ctx.Request.Context(), actor, org, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// AssignBooking godoc
//
//	@Summary	Set the mechanic and the expected time
//	@Tags		Workshop operations
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string				true	"Booking id"
//	@Param		body	body		dtos.AssignInput	true	"Body"
//	@Success	200		{object}	dtos.BookingView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/bookings/{id}/assignment [put]
func (c *Controller) AssignBooking(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.AssignInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.Assign(ctx.Request.Context(), actor, org, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// AddExtra godoc
//
//	@Summary		Propose an extra budget
//	@Description	Only while the vehicle is at the workshop. The customer approves or declines it.
//	@Tags			Workshop operations
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Booking id"
//	@Param			body	body		dtos.ExtraInput	true	"Body"
//	@Success		201		{object}	dtos.BookingView
//	@Failure		404		{object}	shared.ErrorResponse
//	@Failure		409		{object}	shared.ErrorResponse
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fleet/bookings/{id}/extras [post]
func (c *Controller) AddExtra(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.ExtraInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.AddExtra(ctx.Request.Context(), actor, org, id, in)
	respond(ctx, http.StatusCreated, view, err)
}

// PartnerCancelBooking godoc
//
//	@Summary	Cancel a booking on behalf of the workshop
//	@Tags		Workshop operations
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string				true	"Booking id"
//	@Param		body	body		dtos.MessageInput	true	"Body"
//	@Success	200		{object}	dtos.BookingView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	409		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/bookings/{id}/cancel [post]
func (c *Controller) PartnerCancelBooking(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.MessageInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.WorkshopCancel(ctx.Request.Context(), actor, org, id, in.Body)
	respond(ctx, http.StatusOK, view, err)
}

// PartnerSendMessage godoc
//
//	@Summary	Answer the customer
//	@Tags		Workshop operations
//	@Accept		json
//	@Produce	json
//	@Param		id		path		string				true	"Booking id"
//	@Param		body	body		dtos.MessageInput	true	"Body"
//	@Success	201		{object}	dtos.MessageView
//	@Failure	404		{object}	shared.ErrorResponse
//	@Failure	409		{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/bookings/{id}/messages [post]
func (c *Controller) PartnerSendMessage(ctx *gin.Context) {
	actor, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	var in dtos.MessageInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.bookings.WorkshopSend(ctx.Request.Context(), actor, org, id, in)
	respond(ctx, http.StatusCreated, view, err)
}

// PartnerMessages godoc
//
//	@Summary	Read the messages of a booking
//	@Tags		Workshop operations
//	@Produce	json
//	@Param		id	path		string	true	"Booking id"
//	@Success	200	{object}	dtos.MessageList
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/fleet/bookings/{id}/messages [get]
func (c *Controller) PartnerMessages(ctx *gin.Context) {
	_, org, ok := staff(ctx)
	if !ok {
		return
	}
	id, ok := bookingParam(ctx)
	if !ok {
		return
	}
	page, ok := pageOf(ctx)
	if !ok {
		return
	}
	list, err := c.bookings.WorkshopMessages(ctx.Request.Context(), org, id, page)
	respond(ctx, http.StatusOK, list, err)
}
