package enums

type OTPPurpose string

const (
	OTPPurposeVerifyPhone   OTPPurpose = "verify_phone"
	OTPPurposeResetPassword OTPPurpose = "reset_password"
)

func (p OTPPurpose) String() string {
	return string(p)
}
