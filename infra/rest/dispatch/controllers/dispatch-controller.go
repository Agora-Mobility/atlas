package controllers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
	"github.com/Agora-Mobility/atlas/infra/middleware"
	"github.com/Agora-Mobility/atlas/infra/rest/binding"
	"github.com/Agora-Mobility/atlas/shared"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
	"github.com/Agora-Mobility/atlas/usecases/dispatch/dtos"
)

type DispatchController struct {
	dispatch *dispatch.Usecase
}

func NewDispatchController(uc *dispatch.Usecase) *DispatchController {
	return &DispatchController{dispatch: uc}
}

func userWithRole(ctx *gin.Context, role enums.Role) (uuid.UUID, bool) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return uuid.Nil, false
	}
	current, _ := middleware.RoleFromContext(reqCtx)
	if current != role {
		shared.Forbidden(ctx, "WRONG_ACCOUNT_TYPE", "This action is not available to this account.")
		return uuid.Nil, false
	}
	return userID, true
}

func idParam(ctx *gin.Context, code, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		shared.NotFound(ctx, code, message)
		return uuid.Nil, false
	}
	return id, true
}

func requestParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "REQUEST_NOT_FOUND", "Request not found.")
}

func idParamNamed(ctx *gin.Context, name, code, message string) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param(name))
	if err != nil {
		shared.NotFound(ctx, code, message)
		return uuid.Nil, false
	}
	return id, true
}

func offerParam(ctx *gin.Context) (uuid.UUID, bool) {
	return idParam(ctx, "OFFER_NOT_FOUND", "Offer not found.")
}

func respond(ctx *gin.Context, status int, body any, err error) {
	if err != nil {
		handleError(ctx, err)
		return
	}
	if body == nil {
		ctx.Status(status)
		return
	}
	ctx.JSON(status, body)
}

func handleError(ctx *gin.Context, err error) {
	var feeChanged *dispatch.CancelFeeChangedError
	if errors.As(err, &feeChanged) {
		ctx.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"error":     shared.ErrorBody{Code: "CANCEL_FEE_CHANGED", Message: "Confirm the current cancellation fee."},
			"fee_cents": feeChanged.FeeCents, "currency": feeChanged.Currency,
		})
		return
	}
	var expired *dispatch.FareEstimateExpiredError
	if errors.As(err, &expired) {
		ctx.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"error":  shared.ErrorBody{Code: "FARE_ESTIMATE_EXPIRED", Message: "The price expired. Confirm the new one."},
			"quotes": expired.Fresh,
		})
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
}

var errorTable = []struct {
	target  error
	status  int
	code    string
	message string
}{
	{dispatch.ErrRequestNotFound, http.StatusNotFound, "REQUEST_NOT_FOUND", "Request not found."},
	{entities.ErrPaymentOutstanding, http.StatusConflict, "PAYMENT_OUTSTANDING", "Pay the completed request before asking for another."},
	{entities.ErrPaymentMethodUnavailable, http.StatusUnprocessableEntity, "PAYMENT_METHOD_UNAVAILABLE", "This payment method is not available for this service."},
	{dispatch.ErrInvalidGrouping, http.StatusUnprocessableEntity, "INVALID_GROUPING", "group_by must be category, service, status, zone or organization."},
	{dispatch.ErrOfferNotFound, http.StatusNotFound, "OFFER_NOT_FOUND", "Offer not found."},
	{dispatch.ErrZoneNotFound, http.StatusNotFound, "ZONE_NOT_FOUND", "Zone not found."},
	{dispatch.ErrPolicyNotFound, http.StatusNotFound, "POLICY_NOT_FOUND", "Policy not found."},
	{dispatch.ErrServiceNotOffered, http.StatusUnprocessableEntity, "SERVICE_NOT_OFFERED", "This service is not available."},
	{dispatch.ErrOutsideServiceArea, http.StatusUnprocessableEntity, "OUTSIDE_SERVICE_AREA", "The service is not available at this location yet."},
	{dispatch.ErrFareEstimateRequired, http.StatusUnprocessableEntity, "FARE_ESTIMATE_REQUIRED", "This service needs a fare estimate."},
	{dispatch.ErrFareEstimateInvalid, http.StatusConflict, "FARE_ESTIMATE_INVALID", "The price does not match this route. Ask for a new one."},
	{dispatch.ErrFareEstimateUsed, http.StatusConflict, "FARE_ESTIMATE_USED", "This price was already used. Ask for a new one."},
	{entities.ErrBookingOnly, http.StatusUnprocessableEntity, "USE_BOOKINGS", "This service works through a booking: POST /bookings."},
	{entities.ErrBookingManaged, http.StatusConflict, "BOOKING_MANAGED", "This request is managed through its booking."},
	{dispatch.ErrDuplicateRequest, http.StatusConflict, "DUPLICATE_REQUEST", "You already have this request active."},
	{dispatch.ErrReasonNotApplicable, http.StatusUnprocessableEntity, "REASON_NOT_APPLICABLE", "This reason does not exist or does not apply here."},
	{dispatch.ErrUnknownReasonActor, http.StatusUnprocessableEntity, "UNKNOWN_REASON_ACTOR", "actor must be customer, provider or offer."},
	{dispatch.ErrReportScope, http.StatusUnprocessableEntity, "INVALID_REPORT_SCOPE", "scope must be customer_cancel, provider_cancel or offer_reject."},
	{entities.ErrVoucherNotFound, http.StatusUnprocessableEntity, "VOUCHER_NOT_FOUND", "This voucher does not exist."},
	{entities.ErrVoucherExpired, http.StatusUnprocessableEntity, "VOUCHER_EXPIRED", "This voucher is outside its validity."},
	{entities.ErrVoucherNotApplicable, http.StatusUnprocessableEntity, "VOUCHER_NOT_APPLICABLE", "This voucher does not apply to this service."},
	{entities.ErrVoucherUsed, http.StatusConflict, "VOUCHER_USED", "This voucher was already used."},
	{entities.ErrVoucherBelowMinimum, http.StatusUnprocessableEntity, "VOUCHER_BELOW_MINIMUM", "The price is below the minimum of this voucher."},
	{dispatch.ErrReceiptNotReady, http.StatusConflict, "RECEIPT_NOT_READY", "The receipt exists after the request is completed."},
	{dispatch.ErrStopNotFound, http.StatusNotFound, "STOP_NOT_FOUND", "This stop does not exist."},
	{entities.ErrChatClosed, http.StatusConflict, "CHAT_CLOSED", "The chat only works while the request has a provider and is not finished."},
	{entities.ErrCatalogItemNotFound, http.StatusUnprocessableEntity, "QUICK_REPLY_NOT_FOUND", "This quick reply does not exist."},
	{entities.ErrShareNotAvailable, http.StatusConflict, "SHARE_NOT_AVAILABLE", "Only an active request can be shared."},
	{entities.ErrShareNotFound, http.StatusNotFound, "SHARE_NOT_FOUND", "This link does not exist or expired."},
	{entities.ErrSOSNotFound, http.StatusNotFound, "SOS_NOT_FOUND", "Alert not found."},
	{entities.ErrNoETA, http.StatusConflict, "ETA_NOT_AVAILABLE", "There is no position to calculate the arrival."},
	{entities.ErrCallNotAvailable, http.StatusConflict, "CALL_NOT_AVAILABLE", "A call needs an active request with a provider."},
	{services.ErrCallsUnavailable, http.StatusServiceUnavailable, "CALLS_UNAVAILABLE", "Calls are not available."},
	{entities.ErrCompletionCodeWrong, http.StatusUnprocessableEntity, "COMPLETION_CODE_WRONG", "The code is wrong."},
	{entities.ErrCompletionCodeLocked, http.StatusConflict, "COMPLETION_CODE_LOCKED", "Too many wrong codes. An operator has to finish this request."},
	{entities.ErrTowingDetailsMissing, http.StatusUnprocessableEntity, "TOWING_DETAILS_REQUIRED", "Towing needs the incident and whether the vehicle moves."},
	{entities.ErrUnknownIncident, http.StatusUnprocessableEntity, "INCIDENT_NOT_FOUND", "This incident does not exist."},
	{dispatch.ErrNoTariff, http.StatusUnprocessableEntity, "NO_TARIFF", "There is no tariff for this service here yet."},
	{dispatch.ErrRequestConflict, http.StatusConflict, "REQUEST_CONFLICT", "The request changed in the meantime."},
	{dispatch.ErrProviderNotApproved, http.StatusForbidden, "PROVIDER_NOT_APPROVED", "The provider is not approved for this."},
	{dispatch.ErrProviderBusy, http.StatusConflict, "PROVIDER_BUSY", "You already have an active job."},
	{dispatch.ErrProviderNotEligible, http.StatusUnprocessableEntity, "PROVIDER_NOT_ELIGIBLE", "This provider cannot take this request."},
	{dispatch.ErrQuoteRequired, http.StatusConflict, "QUOTE_REQUIRED", "This offer is answered with a quote."},
	{dispatch.ErrQuoteNotAllowed, http.StatusConflict, "QUOTE_NOT_ALLOWED", "This offer does not take quotes."},
	{dispatch.ErrQuoteExpired, http.StatusConflict, "QUOTE_EXPIRED", "The quote is no longer valid."},
	{dispatch.ErrRouteChangeForbidden, http.StatusConflict, "ROUTE_CHANGE_FORBIDDEN", "The route of this request cannot change."},
	{dispatch.ErrInvalidChannel, http.StatusUnprocessableEntity, "INVALID_CHANNEL", "Unknown delivery channel."},
	{entities.ErrOfferClosed, http.StatusConflict, "OFFER_ENDED", "This offer has ended."},
	{entities.ErrOfferExpired, http.StatusConflict, "OFFER_EXPIRED", "This offer has expired."},
	{entities.ErrInvalidTransition, http.StatusConflict, "INVALID_TRANSITION", "This action is not possible in the current state."},
	{entities.ErrTransitionForbidden, http.StatusForbidden, "TRANSITION_FORBIDDEN", "You cannot make this change."},
	{entities.ErrCancelReasonMissing, http.StatusUnprocessableEntity, "CANCEL_REASON_REQUIRED", "A cancel reason is required."},
	{entities.ErrScheduleNotAllowed, http.StatusUnprocessableEntity, "SCHEDULE_NOT_ALLOWED", "This service cannot be scheduled."},
	{entities.ErrBoostNotAllowed, http.StatusConflict, "BOOST_NOT_ALLOWED", "This request cannot raise its offer."},
	{entities.ErrBoostNotHigher, http.StatusConflict, "BOOST_NOT_HIGHER", "The raise must be higher than the current one."},
	{repository.ErrInvalidGeometry, http.StatusUnprocessableEntity, "INVALID_GEOMETRY", "The geometry is not a valid polygon."},
}

// Nearby godoc
//
//	@Summary		Providers near a point
//	@Description	Customers only. Aggregates by H3 cell, without identity or exact position, and only above a minimum per cell.
//	@Tags			Dispatch
//	@Produce		json
//	@Param			lat			query		number	true	"Latitude"
//	@Param			lng			query		number	true	"Longitude"
//	@Param			category	query		string	true	"Category slug"
//	@Success		200			{object}	dtos.NearbyView
//	@Failure		422			{object}	shared.ErrorResponse	"Outside the service area"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/nearby [get]
func (c *DispatchController) Nearby(ctx *gin.Context) {
	if _, ok := userWithRole(ctx, enums.RoleCustomer); !ok {
		return
	}
	lat, latErr := strconv.ParseFloat(ctx.Query("lat"), 64)
	lng, lngErr := strconv.ParseFloat(ctx.Query("lng"), 64)
	if latErr != nil || lngErr != nil || ctx.Query("category") == "" {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "lat, lng and category are required.")
		return
	}
	view, err := c.dispatch.Nearby(ctx.Request.Context(), entities.GeoPoint{Lat: lat, Lng: lng}, ctx.Query("category"))
	respond(ctx, http.StatusOK, view, err)
}

// PreviewVoucher godoc
//
//	@Summary		What a voucher takes off a price
//	@Description	Same rules as the request: validity, service and use per customer. Nothing is reserved.
//	@Tags			Vouchers
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.VoucherPreviewInput	true	"Code and price"
//	@Success		200		{object}	dtos.VoucherPreview
//	@Failure		409		{object}	shared.ErrorResponse	"Already used, or another price"
//	@Failure		422		{object}	shared.ErrorResponse	"Expired or of another service"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/vouchers/preview [post]
func (c *DispatchController) PreviewVoucher(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	var in dtos.VoucherPreviewInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.PreviewVoucher(ctx.Request.Context(), customerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// CreateFareEstimates godoc
//
//	@Summary		Fixed price before confirming
//	@Description	Customers only. One priced option per active service of the category. route_source tells whether the distance came from Mapbox or from the fallback estimate.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.FareEstimateInput	true	"Route"
//	@Success		201		{object}	dtos.FareEstimateList
//	@Failure		422		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/fare-estimates [post]
func (c *DispatchController) CreateFareEstimates(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	var in dtos.FareEstimateInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	list, err := c.dispatch.CreateEstimates(ctx.Request.Context(), customerID, in)
	respond(ctx, http.StatusCreated, list, err)
}

// RegisterDevice godoc
//
//	@Summary	Register the push token of this device
//	@Tags		Dispatch
//	@Accept		json
//	@Param		body	body	dtos.DeviceInput	true	"Device"
//	@Success	204
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/devices [put]
func (c *DispatchController) RegisterDevice(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	var in dtos.DeviceInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.RegisterDevice(ctx.Request.Context(), userID, in))
}

// UnregisterDevice godoc
//
//	@Summary	Forget a push token
//	@Tags		Dispatch
//	@Param		token	path	string	true	"Push token"
//	@Success	204
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/devices/{token} [delete]
func (c *DispatchController) UnregisterDevice(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.UnregisterDevice(ctx.Request.Context(), userID, ctx.Param("token")))
}

// GoOnline godoc
//
//	@Summary	Enter the live index
//	@Tags		Provider work
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dtos.OnlineInput	false	"First position"
//	@Success	200		{object}	dtos.CadenceView
//	@Failure	403		{object}	shared.ErrorResponse	"Not approved"
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/online [post]
func (c *DispatchController) GoOnline(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	var in dtos.OnlineInput
	if ctx.Request.ContentLength != 0 {
		if err := binding.JSON(ctx, &in); err != nil {
			shared.InvalidRequest(ctx, err)
			return
		}
	}
	view, err := c.dispatch.GoOnline(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// GoOffline godoc
//
//	@Summary	Leave the live index
//	@Tags		Provider work
//	@Produce	json
//	@Success	200	{object}	dtos.CadenceView
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/providers/me/offline [post]
func (c *DispatchController) GoOffline(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	view, err := c.dispatch.GoOffline(ctx.Request.Context(), providerID)
	respond(ctx, http.StatusOK, view, err)
}

// ReportLocations godoc
//
//	@Summary		Send a batch of positions
//	@Description	Points use age_s, seconds before now, so a wrong phone clock does not matter. Implausible points are dropped. The answer carries the next cadence.
//	@Tags			Provider work
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.LocationsInput	true	"Positions"
//	@Success		200		{object}	dtos.CadenceView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/locations [post]
func (c *DispatchController) ReportLocations(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	var in dtos.LocationsInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.ReportPositions(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// SetCoverage godoc
//
//	@Summary		Set the base and service radius
//	@Description	Recomputes the H3 coverage cells of the categories dispatched by coverage. Without radius_km the default of the policy applies.
//	@Tags			Provider work
//	@Accept			json
//	@Produce		json
//	@Param			body	body		dtos.CoverageInput	true	"Base"
//	@Success		200		{object}	dtos.CoverageView
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/providers/me/coverage [put]
func (c *DispatchController) SetCoverage(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	var in dtos.CoverageInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.SetCoverage(ctx.Request.Context(), providerID, in)
	respond(ctx, http.StatusOK, view, err)
}

// CreateRequest godoc
//
//	@Summary		Create a service request
//	@Description	Requires Idempotency-Key, so the app can resend on a bad network without creating a second request. The pin is the truth; reference and address only help. When the fare estimate expired the answer is 409 with new quotes and no request is created.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			Idempotency-Key	header		string					true	"Unique per attempt"
//	@Param			body			body		dtos.CreateRequestInput	true	"Request"
//	@Success		201				{object}	dtos.RequestView
//	@Success		200				{object}	dtos.RequestView		"The same key was already used"
//	@Failure		409				{object}	shared.ErrorResponse	"FARE_ESTIMATE_EXPIRED or FARE_ESTIMATE_INVALID"
//	@Failure		422				{object}	shared.ErrorResponse	"OUTSIDE_SERVICE_AREA and others"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests [post]
func (c *DispatchController) CreateRequest(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	var in dtos.CreateRequestInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, created, err := c.dispatch.CreateRequest(ctx.Request.Context(), customerID, ctx.GetHeader(middleware.HeaderIdempotencyKey), in)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	respond(ctx, status, view, err)
}

// ListRequests godoc
//
//	@Summary		History of requests
//	@Description	Customers only, most recent first.
//	@Tags			Dispatch
//	@Produce		json
//	@Param			status		query		string	false	"Comma-separated statuses"
//	@Param			category	query		string	false	"Category slug"
//	@Param			page		query		int		false	"Page"
//	@Param			size		query		int		false	"Page size"
//	@Success		200			{object}	dtos.RequestList
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests [get]
func (c *DispatchController) ListRequests(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	var page pagination.Params
	if err := ctx.ShouldBindQuery(&page); err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid pagination.")
		return
	}
	var statuses []string
	if raw := ctx.Query("status"); raw != "" {
		statuses = strings.Split(raw, ",")
	}
	list, err := c.dispatch.ListRequests(ctx.Request.Context(), customerID, statuses, ctx.Query("category"), page)
	respond(ctx, http.StatusOK, list, err)
}

// ActiveRequest godoc
//
//	@Summary		Current state
//	@Description	What the app reloads after a resync: the active request and, for providers, the open offers.
//	@Tags			Dispatch
//	@Produce		json
//	@Success		200	{object}	dtos.ActiveState
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/active [get]
func (c *DispatchController) ActiveRequest(ctx *gin.Context) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	switch role {
	case enums.RoleProvider:
		state, err := c.dispatch.ActiveForProvider(reqCtx, userID)
		respond(ctx, http.StatusOK, state, err)
	case enums.RoleCustomer:
		state, err := c.dispatch.ActiveForCustomer(reqCtx, userID)
		respond(ctx, http.StatusOK, state, err)
	default:
		shared.Forbidden(ctx, "WRONG_ACCOUNT_TYPE", "This action is not available to this account.")
	}
}

// GetRequest godoc
//
//	@Summary	Get a request
//	@Tags		Dispatch
//	@Produce	json
//	@Param		id	path		string	true	"Request id"
//	@Success	200	{object}	dtos.RequestView
//	@Failure	404	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/requests/{id} [get]
func (c *DispatchController) GetRequest(ctx *gin.Context) {
	userID, ok := middleware.UserIDFromContext(ctx.Request.Context())
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.GetRequest(ctx.Request.Context(), userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// CancelRequest godoc
//
//	@Summary		Cancel a request
//	@Description	Customer or provider of the request. The reason comes from GET /cancellation-reasons, and a reason that needs a note needs the note. When the customer owes a fee, expected_fee_cents must be the fee shown by GET /requests/{id}/cancellation, or the answer is 409 CANCEL_FEE_CHANGED with the current fee. A provider who cancels never makes the customer pay.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Request id"
//	@Param			body	body		dtos.CancelInput	true	"Reason"
//	@Success		200		{object}	dtos.RequestView
//	@Failure		409		{object}	shared.ErrorResponse	"Not possible in this state, or the fee changed"
//	@Failure		422		{object}	shared.ErrorResponse	"Unknown reason or missing note"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/cancel [post]
func (c *DispatchController) CancelRequest(ctx *gin.Context) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.CancelInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	role, _ := middleware.RoleFromContext(reqCtx)
	if role == enums.RoleProvider {
		view, err := c.dispatch.CancelByProvider(reqCtx, userID, id, in)
		respond(ctx, http.StatusOK, view, err)
		return
	}
	view, err := c.dispatch.CancelByCustomer(reqCtx, userID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// CancellationPreview godoc
//
//	@Summary		Cost of a cancellation now
//	@Description	The fee the customer would pay at this moment, until when it is free, and the reasons to choose from. A provider sees the provider reasons and fee 0.
//	@Tags			Dispatch
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.CancellationPreview
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/cancellation [get]
func (c *DispatchController) CancellationPreview(ctx *gin.Context) {
	reqCtx := ctx.Request.Context()
	userID, ok := middleware.UserIDFromContext(reqCtx)
	if !ok {
		shared.Unauthorized(ctx, shared.CodeUnauthorized, "Authentication required.")
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.PreviewCancellation(reqCtx, userID, id)
	respond(ctx, http.StatusOK, view, err)
}

// CancellationReasons godoc
//
//	@Summary		Reasons to cancel or refuse
//	@Description	List managed in the backoffice, per actor and category.
//	@Tags			Dispatch
//	@Produce		json
//	@Param			actor		query		string	true	"customer, provider or offer"
//	@Param			category	query		string	true	"Category slug"
//	@Success		200			{object}	dtos.ReasonOptions
//	@Failure		422			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/cancellation-reasons [get]
func (c *DispatchController) CancellationReasons(ctx *gin.Context) {
	view, err := c.dispatch.ReasonOptions(ctx.Request.Context(), ctx.Query("actor"), ctx.Query("category"))
	respond(ctx, http.StatusOK, view, err)
}

// ListCancellationRules godoc
//
//	@Summary	Cancellation rules of every service
//	@Tags		Backoffice
//	@Produce	json
//	@Success	200	{object}	dtos.CancellationRuleList
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/cancellation-rules [get]
func (c *DispatchController) ListCancellationRules(ctx *gin.Context) {
	list, err := c.dispatch.ListCancellationRules(ctx.Request.Context())
	respond(ctx, http.StatusOK, list, err)
}

// SetCancellationRule godoc
//
//	@Summary	Set the cancellation rule of a service
//	@Tags		Backoffice
//	@Accept		json
//	@Produce	json
//	@Param		serviceId	path		string						true	"Service id"
//	@Param		body		body		dtos.CancellationRuleInput	true	"Rule"
//	@Success	200			{object}	dtos.CancellationRuleView
//	@Failure	422			{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/admin/cancellation-rules/{serviceId} [put]
func (c *DispatchController) SetCancellationRule(ctx *gin.Context) {
	actor, ok := staffActor(ctx)
	if !ok {
		return
	}
	id, ok := idParamNamed(ctx, "serviceId", "SERVICE_NOT_FOUND", "Service not found.")
	if !ok {
		return
	}
	var in dtos.CancellationRuleInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.SetCancellationRule(ctx.Request.Context(), actor, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// CancellationReport godoc
//
//	@Summary		Requests cancelled or offers refused, per reason
//	@Description	Grouped by the reason id, so a label can change without breaking the report.
//	@Tags			Backoffice
//	@Produce		json
//	@Param			scope		query		string	true	"customer_cancel, provider_cancel or offer_reject"
//	@Param			category	query		string	false	"Category slug"
//	@Param			from		query		string	false	"RFC3339"
//	@Param			to			query		string	false	"RFC3339"
//	@Success		200			{object}	dtos.CancellationReport
//	@Failure		422			{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/admin/reports/cancellations [get]
func (c *DispatchController) CancellationReport(ctx *gin.Context) {
	from, to, ok := reportPeriod(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.CancellationReport(ctx.Request.Context(), ctx.Query("scope"), ctx.Query("category"), from, to)
	respond(ctx, http.StatusOK, view, err)
}

func reportPeriod(ctx *gin.Context) (from, to *time.Time, ok bool) {
	for _, item := range []struct {
		name string
		into **time.Time
	}{{"from", &from}, {"to", &to}} {
		raw := ctx.Query(item.name)
		if raw == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid "+item.name+".")
			return nil, nil, false
		}
		*item.into = &parsed
	}
	return from, to, true
}

// BoostRequest godoc
//
//	@Summary		Raise the offer
//	@Description	10 or 20 percent over the original price, all of it for the provider. Starts a new search round.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Request id"
//	@Param			body	body		dtos.BoostInput	true	"Percent"
//	@Success		200		{object}	dtos.RequestView
//	@Failure		409		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/boost [post]
func (c *DispatchController) BoostRequest(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.BoostInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.Boost(ctx.Request.Context(), customerID, id, in.Percent)
	respond(ctx, http.StatusOK, view, err)
}

// ChangeRoute godoc
//
//	@Summary		Change the destination or stops
//	@Description	Reprices with the same tariff and records the difference as a fare adjustment.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Request id"
//	@Param			body	body		dtos.ChangeRouteInput	true	"New route"
//	@Success		200		{object}	dtos.RequestView
//	@Failure		409		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/route [post]
func (c *DispatchController) ChangeRoute(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.ChangeRouteInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.ChangeRoute(ctx.Request.Context(), customerID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// SelectQuote godoc
//
//	@Summary		Choose a quote
//	@Description	The transaction checks that the quote is still valid. The other providers get their offer cancelled.
//	@Tags			Dispatch
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Request id"
//	@Param			body	body		dtos.SelectQuoteInput	true	"Quote"
//	@Success		200		{object}	dtos.RequestView
//	@Failure		409		{object}	shared.ErrorResponse	"QUOTE_EXPIRED or REQUEST_CONFLICT"
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/select-quote [post]
func (c *DispatchController) SelectQuote(ctx *gin.Context) {
	customerID, ok := userWithRole(ctx, enums.RoleCustomer)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.SelectQuoteInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	view, err := c.dispatch.SelectQuote(ctx.Request.Context(), customerID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

// StartRoute godoc
//
//	@Summary	Provider leaves for the pickup
//	@Tags		Provider work
//	@Produce	json
//	@Param		id	path		string	true	"Request id"
//	@Success	200	{object}	dtos.RequestView
//	@Failure	409	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/requests/{id}/en-route [post]
func (c *DispatchController) StartRoute(ctx *gin.Context) {
	c.providerAction(ctx, c.dispatch.StartRoute)
}

// Arrive godoc
//
//	@Summary		Provider arrived at the pickup
//	@Description	Starts counting the waiting time.
//	@Tags			Provider work
//	@Produce		json
//	@Param			id	path		string	true	"Request id"
//	@Success		200	{object}	dtos.RequestView
//	@Failure		409	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/requests/{id}/arrive [post]
func (c *DispatchController) Arrive(ctx *gin.Context) {
	c.providerAction(ctx, c.dispatch.Arrive)
}

// StartJob godoc
//
//	@Summary	Start the trip or service
//	@Tags		Provider work
//	@Produce	json
//	@Param		id	path		string	true	"Request id"
//	@Success	200	{object}	dtos.RequestView
//	@Failure	409	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/requests/{id}/start [post]
func (c *DispatchController) StartJob(ctx *gin.Context) {
	c.providerAction(ctx, c.dispatch.StartJob)
}

// CompleteJob godoc
//
//	@Summary	Complete the trip or service
//	@Tags		Provider work
//	@Produce	json
//	@Param		id	path		string	true	"Request id"
//	@Success	200	{object}	dtos.RequestView
//	@Failure	409	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Param		body	body	dtos.CompleteInput	false	"Code the customer shows, when the service needs one"
//	@Router		/api/v1/requests/{id}/complete [post]
func (c *DispatchController) CompleteJob(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	var in dtos.CompleteInput
	if ctx.Request.ContentLength != 0 {
		if err := binding.JSON(ctx, &in); err != nil {
			shared.InvalidRequest(ctx, err)
			return
		}
	}
	view, err := c.dispatch.CompleteJob(ctx.Request.Context(), providerID, id, in)
	respond(ctx, http.StatusOK, view, err)
}

func (c *DispatchController) providerAction(ctx *gin.Context, action providerRequestAction) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := requestParam(ctx)
	if !ok {
		return
	}
	view, err := action(ctx.Request.Context(), providerID, id)
	respond(ctx, http.StatusOK, view, err)
}

// GetOffer godoc
//
//	@Summary		Detail of an offer
//	@Description	The event only carries the essentials; the rest comes from here.
//	@Tags			Provider work
//	@Produce		json
//	@Param			id	path		string	true	"Offer id"
//	@Success		200	{object}	dtos.OfferView
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/offers/{id} [get]
func (c *DispatchController) GetOffer(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := offerParam(ctx)
	if !ok {
		return
	}
	view, err := c.dispatch.GetOffer(ctx.Request.Context(), providerID, id)
	respond(ctx, http.StatusOK, view, err)
}

// OfferDelivered godoc
//
//	@Summary		Acknowledge an offer
//	@Description	Tells the server the offer reached the phone, and through which channel. Without it the watcher moves to the next channel or provider.
//	@Tags			Provider work
//	@Accept			json
//	@Param			id		path	string				true	"Offer id"
//	@Param			body	body	dtos.DeliveredInput	true	"Channel"
//	@Success		204
//	@Failure		404	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/offers/{id}/delivered [post]
func (c *DispatchController) OfferDelivered(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := offerParam(ctx)
	if !ok {
		return
	}
	var in dtos.DeliveredInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.MarkDelivered(ctx.Request.Context(), providerID, id, in.Channel))
}

// AcceptOffer godoc
//
//	@Summary		Accept an offer
//	@Description	Send the version received with the offer. Only one provider wins; the others get 409.
//	@Tags			Provider work
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Offer id"
//	@Param			body	body		dtos.AcceptInput	true	"Version"
//	@Success		200		{object}	dtos.AcceptResult
//	@Failure		409		{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/offers/{id}/accept [post]
func (c *DispatchController) AcceptOffer(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := offerParam(ctx)
	if !ok {
		return
	}
	var in dtos.AcceptInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	result, err := c.dispatch.Accept(ctx.Request.Context(), providerID, id, in.Version)
	respond(ctx, http.StatusOK, result, err)
}

// RejectOffer godoc
//
//	@Summary		Reject an offer
//	@Description	The reason comes from GET /cancellation-reasons?actor=offer and is kept for the reports.
//	@Tags			Provider work
//	@Accept			json
//	@Param			id		path	string					true	"Offer id"
//	@Param			body	body	dtos.RejectOfferInput	true	"Reason"
//	@Success		204
//	@Failure		409	{object}	shared.ErrorResponse
//	@Failure		422	{object}	shared.ErrorResponse
//	@Security		BearerAuth
//	@Security		ClientId
//	@Router			/api/v1/offers/{id}/reject [post]
func (c *DispatchController) RejectOffer(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := offerParam(ctx)
	if !ok {
		return
	}
	var in dtos.RejectOfferInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.Reject(ctx.Request.Context(), providerID, id, in))
}

// QuoteOffer godoc
//
//	@Summary	Answer an offer with a quote
//	@Tags		Provider work
//	@Accept		json
//	@Param		id		path	string			true	"Offer id"
//	@Param		body	body	dtos.QuoteInput	true	"Quote"
//	@Success	204
//	@Failure	409	{object}	shared.ErrorResponse
//	@Security	BearerAuth
//	@Security	ClientId
//	@Router		/api/v1/offers/{id}/quote [post]
func (c *DispatchController) QuoteOffer(ctx *gin.Context) {
	providerID, ok := userWithRole(ctx, enums.RoleProvider)
	if !ok {
		return
	}
	id, ok := offerParam(ctx)
	if !ok {
		return
	}
	var in dtos.QuoteInput
	if err := binding.JSON(ctx, &in); err != nil {
		shared.InvalidRequest(ctx, err)
		return
	}
	respond(ctx, http.StatusNoContent, nil, c.dispatch.SubmitQuote(ctx.Request.Context(), providerID, id, in))
}
