package enums

type OTPPurpose string

const (
	OTPPurposeVerifyPhone     OTPPurpose = "verify_phone"
	OTPPurposeLogin           OTPPurpose = "login"
	OTPPurposeResetPassword   OTPPurpose = "reset_password"
	OTPPurposeChangePhone     OTPPurpose = "change_phone"
	OTPPurposeInvitation      OTPPurpose = "invitation"
	OTPPurposeConfirmIdentity OTPPurpose = "confirm_identity"
)

func (p OTPPurpose) String() string {
	return string(p)
}
