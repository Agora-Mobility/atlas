package binding

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	ginbinding "github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"

	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

var (
	setupOnce         sync.Once
	ErrMalformedInput = errors.New("malformed request")
)

func Setup() {
	setupOnce.Do(func() {
		v, ok := ginbinding.Validator.Engine().(*validator.Validate)
		if !ok {
			return
		}
		v.RegisterTagNameFunc(func(f reflect.StructField) string {
			for _, tag := range []string{"json", "form", "uri"} {
				name, _, _ := strings.Cut(f.Tag.Get(tag), ",")
				if name != "" && name != "-" {
					return name
				}
			}
			return f.Name
		})
	})
}

func JSON(c *gin.Context, dest any) error {
	return toAppError(c.ShouldBindJSON(dest))
}

func Query(c *gin.Context, dest any) error {
	return toAppError(c.ShouldBindQuery(dest))
}

func toAppError(err error) error {
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return fmt.Errorf("%w: %w", ErrMalformedInput, err)
	}
	details := make(map[string]string, len(verrs))
	for _, fe := range verrs {
		details[fe.Field()] = message(fe)
	}
	return customerrors.NewValidationError(details)
}

func message(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "max":
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "min":
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", fe.Param())
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	case "e164":
		return "must be in international format (e.g. +244923000000)"
	case "email":
		return "must be a valid e-mail"
	case "len":
		return fmt.Sprintf("must have exactly %s characters", fe.Param())
	case "numeric":
		return "must contain only digits"
	default:
		return "is invalid (" + fe.Tag() + ")"
	}
}
