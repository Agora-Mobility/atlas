package shared

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

const (
	CodeInvalidRequest  = "INVALID_REQUEST"
	CodeValidationError = "VALIDATION_ERROR"
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeNotFound        = "NOT_FOUND"
	CodeTooManyRequests = "TOO_MANY_REQUESTS"
	CodeInternalError   = "INTERNAL_ERROR"
)

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code          string            `json:"code" example:"INVALID_CREDENTIALS"`
	Message       string            `json:"message" example:"Invalid identifier or password."`
	Details       map[string]string `json:"details,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
}

func RespondError(ctx *gin.Context, status int, code, message string) {
	respond(ctx, status, ErrorBody{Code: code, Message: message})
}

func BadRequest(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusBadRequest, code, message)
}

func Unauthorized(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusUnauthorized, code, message)
}

func Forbidden(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusForbidden, code, message)
}

func NotFound(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusNotFound, code, message)
}

func Conflict(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusConflict, code, message)
}

func TooManyRequests(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusTooManyRequests, code, message)
}

func ServiceUnavailable(ctx *gin.Context, code, message string) {
	RespondError(ctx, http.StatusServiceUnavailable, code, message)
}

func ValidationFailed(ctx *gin.Context, validationErr *customerrors.ValidationError) {
	respond(ctx, http.StatusUnprocessableEntity, ErrorBody{
		Code:    CodeValidationError,
		Message: "Some fields are invalid.",
		Details: validationErr.Fields,
	})
}

func UnprocessableWithDetails(ctx *gin.Context, code, message string, details map[string]string) {
	respond(ctx, http.StatusUnprocessableEntity, ErrorBody{Code: code, Message: message, Details: details})
}

func InvalidRequest(ctx *gin.Context, err error) {
	if validationErr, ok := customerrors.AsValidation(err); ok {
		ValidationFailed(ctx, validationErr)
		return
	}
	BadRequest(ctx, CodeInvalidRequest, "Invalid request body.")
}

func InternalServerError(ctx *gin.Context, code, message string, err error) {
	logger.FromContext(ctx.Request.Context()).Error("request failed",
		zap.String("code", code), zap.String("path", ctx.FullPath()), zap.Error(err))
	RespondError(ctx, http.StatusInternalServerError, code, message)
}

func respond(ctx *gin.Context, status int, body ErrorBody) {
	body.CorrelationID = logger.CorrelationID(ctx.Request.Context())
	ctx.AbortWithStatusJSON(status, ErrorResponse{Error: body})
}
