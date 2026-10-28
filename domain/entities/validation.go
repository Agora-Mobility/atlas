package entities

import (
	"errors"
	"regexp"
	"strings"
	"unicode"

	"github.com/asaskevich/govalidator"

	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

var phonePattern = regexp.MustCompile(`^\+[1-9]\d{7,14}$`)

func init() {
	govalidator.SetFieldsRequiredByDefault(true)

	govalidator.TagMap["user_role"] = func(s string) bool { return enums.Role(s).IsValid() }
	govalidator.TagMap["phone"] = phonePattern.MatchString
	govalidator.TagMap["user_status"] = func(s string) bool { return enums.UserStatus(s).IsValid() }
}

func validate(entity any) error {
	if _, err := govalidator.ValidateStruct(entity); err != nil {
		var errs govalidator.Errors
		if !errors.As(err, &errs) {
			return customerrors.FieldError("entity", err.Error())
		}
		details := map[string]string{}
		for field, message := range govalidator.ErrorsByField(errs) {
			details[snakeCase(field)] = message
		}
		return customerrors.NewValidationError(details)
	}
	return nil
}

func snakeCase(field string) string {
	var b strings.Builder
	for i, r := range field {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

func validationError(field, message string) error {
	return customerrors.FieldError(field, message)
}
