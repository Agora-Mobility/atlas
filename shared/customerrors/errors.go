// Package customerrors defines the application error type and the JSON shape
// every error response uses.
package customerrors

import (
	"errors"
	"fmt"
	"net/http"
)

type Code string

const (
	CodeValidation      Code = "VALIDATION_ERROR"
	CodeBadRequest      Code = "BAD_REQUEST"
	CodeUnauthorized    Code = "UNAUTHORIZED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeNotFound        Code = "NOT_FOUND"
	CodeConflict        Code = "CONFLICT"
	CodeTooManyRequests Code = "TOO_MANY_REQUESTS"
	CodeUnavailable     Code = "SERVICE_UNAVAILABLE"
	CodeInternal        Code = "INTERNAL_ERROR"
)

var statusByCode = map[Code]int{
	CodeValidation:      http.StatusUnprocessableEntity,
	CodeBadRequest:      http.StatusBadRequest,
	CodeUnauthorized:    http.StatusUnauthorized,
	CodeForbidden:       http.StatusForbidden,
	CodeNotFound:        http.StatusNotFound,
	CodeConflict:        http.StatusConflict,
	CodeTooManyRequests: http.StatusTooManyRequests,
	CodeUnavailable:     http.StatusServiceUnavailable,
	CodeInternal:        http.StatusInternalServerError,
}

// AppError is an error that carries an HTTP-friendly code and a message safe
// to show to clients. The wrapped Err is only logged.
type AppError struct {
	Code    Code              `json:"code"`
	Message string            `json:"message"`
	Details map[string]string `json:"details,omitempty"`
	Err     error             `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Err }

// Status returns the HTTP status for the error code.
func (e *AppError) Status() int {
	if s, ok := statusByCode[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// WithDetails returns a copy of the error with field-level details.
func (e *AppError) WithDetails(details map[string]string) *AppError {
	c := *e
	c.Details = details
	return &c
}

func New(code Code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Err: err}
}

func Validation(message string, details map[string]string) *AppError {
	return &AppError{Code: CodeValidation, Message: message, Details: details}
}

func BadRequest(message string) *AppError {
	return &AppError{Code: CodeBadRequest, Message: message}
}

func Unauthorized(message string) *AppError {
	return &AppError{Code: CodeUnauthorized, Message: message}
}

func Forbidden(message string) *AppError {
	return &AppError{Code: CodeForbidden, Message: message}
}

func NotFound(resource string) *AppError {
	return &AppError{Code: CodeNotFound, Message: resource + " not found"}
}

func Conflict(message string) *AppError {
	return &AppError{Code: CodeConflict, Message: message}
}

func Internal(err error) *AppError {
	return &AppError{Code: CodeInternal, Message: "internal server error", Err: err}
}

// As extracts an *AppError from err. Unknown errors become internal errors.
func As(err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal(err)
}

// Response is the JSON body of every error response.
type Response struct {
	Error ResponseBody `json:"error"`
}

type ResponseBody struct {
	Code          Code              `json:"code"`
	Message       string            `json:"message"`
	Details       map[string]string `json:"details,omitempty"`
	CorrelationID string            `json:"correlation_id,omitempty"`
}

func NewResponse(e *AppError, correlationID string) Response {
	return Response{Error: ResponseBody{
		Code:          e.Code,
		Message:       e.Message,
		Details:       e.Details,
		CorrelationID: correlationID,
	}}
}
