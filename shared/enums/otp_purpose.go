package enums

type OTPPurpose string

const (
	OTPPurposeVerifyPhone   OTPPurpose = "verify_phone"
	OTPPurposeResetPassword OTPPurpose = "reset_password"
	OTPPurposeChangePhone   OTPPurpose = "change_phone"
	OTPPurposeInvitation    OTPPurpose = "invitation"
)

func (p OTPPurpose) String() string {
	return string(p)
}
