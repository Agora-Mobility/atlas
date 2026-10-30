package dtos

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type RegisterInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923000000"`
}

type LoginInput struct {
	Identifier     string     `json:"identifier" binding:"required,max=254" example:"+244923000000"`
	Password       string     `json:"password" binding:"required,max=128" example:"S3gredo!forte"`
	OrganizationID *uuid.UUID `json:"organization_id,omitempty"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required,max=128"`
}

type PhoneVerificationRequestInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923000000"`
}

type PhoneVerificationConfirmInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923000000"`
	Code  string `json:"code" binding:"required,len=6,numeric" example:"123456"`
}

type RecoveryRequestInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923000000"`
}

type RecoveryConfirmInput struct {
	Phone       string `json:"phone" binding:"required,max=32" example:"+244923000000"`
	Code        string `json:"code" binding:"required,len=6,numeric" example:"123456"`
	NewPassword string `json:"new_password" binding:"required" example:"N0vo!segredo"`
}

type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password" binding:"required,max=128"`
	NewPassword     string `json:"new_password" binding:"required"`
}

type SetPasswordInput struct {
	Password string `json:"password" binding:"required" example:"S3gredo!forte"`
}

type MessageOutput struct {
	Message string `json:"message"`
}

type UserView struct {
	ID          uuid.UUID        `json:"id"`
	Name        string           `json:"name"`
	Email       string           `json:"email,omitempty"`
	Phone       string           `json:"phone,omitempty"`
	Role        enums.Role       `json:"role"`
	Status      enums.UserStatus `json:"status"`
	IsVerified  bool             `json:"is_verified"`
	LastLoginAt *time.Time       `json:"last_login_at,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

type SessionOutput struct {
	AccessToken      string      `json:"access_token"`
	TokenType        string      `json:"token_type" example:"Bearer"`
	ExpiresIn        int64       `json:"expires_in" example:"900"`
	RefreshToken     string      `json:"refresh_token"`
	RefreshExpiresAt time.Time   `json:"refresh_expires_at"`
	Client           string      `json:"client" example:"agora-customer-app"`
	User             UserView    `json:"user"`
	Access           *AccessView `json:"access,omitempty"`
	MFARecoveryCodes []string    `json:"mfa_recovery_codes,omitempty" example:"7KQM-2XRT-PL9D"`
}

func ToUserView(u *entities.User) UserView {
	return UserView{
		ID:          u.ID,
		Name:        u.Name,
		Email:       u.Email,
		Phone:       u.Phone,
		Role:        u.Role,
		Status:      u.Status,
		IsVerified:  u.IsVerified,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}

func ToBlockReasonView(r *entities.BlockReason) BlockReasonView {
	return BlockReasonView{ID: r.ID, Code: r.Code, Label: r.Label}
}

// UpdateProfileInput changes only the fields that are present. An empty
// e-mail removes it; changing or removing the e-mail needs the current password.
type UpdateProfileInput struct {
	Name            *string `json:"name" binding:"omitempty,max=120" example:"Ana Domingos"`
	Email           *string `json:"email" binding:"omitempty,max=254" example:"ana@exemplo.ao"`
	CurrentPassword string  `json:"current_password" binding:"max=128"`
}

type PhoneChangeRequestInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923111111"`
}

type PhoneChangeConfirmInput struct {
	Phone string `json:"phone" binding:"required,max=32" example:"+244923111111"`
	Code  string `json:"code" binding:"required,len=6,numeric" example:"123456"`
}

type DeleteAccountInput struct {
	Password string `json:"password" binding:"required,max=128"`
}

type UserListQuery struct {
	Role   enums.Role       `form:"role" binding:"omitempty,oneof=customer provider"`
	Status enums.UserStatus `form:"status" binding:"omitempty,oneof=active inactive blocked"`
	Search string           `form:"search" binding:"max=120"`
	Page   int              `form:"page" binding:"omitempty,min=1"`
	Size   int              `form:"size" binding:"omitempty,min=1,max=100"`
}

type BlockUserInput struct {
	ReasonIDs []uuid.UUID `json:"reason_ids" binding:"required,min=1,max=20" example:"0192f1d2-7c3a-7b4e-9a10-2f3c4d5e6f70"`
	Note      string      `json:"note" binding:"max=500" example:"Fraude reportada pelo cliente"`
}

type BlockReasonView struct {
	ID    uuid.UUID `json:"id"`
	Code  string    `json:"code" example:"fraud"`
	Label string    `json:"label" example:"Fraude ou actividade suspeita"`
}

type BlockReasonList struct {
	Items []BlockReasonView `json:"items"`
}

type UnblockUserInput struct {
	Reason string `json:"reason" binding:"max=500" example:"Situação resolvida"`
}

type ResetPasswordInput struct {
	NewPassword string `json:"new_password" binding:"required" example:"N0vo!segredo"`
}

type UserPage struct {
	Items      []UserView `json:"items"`
	Page       int        `json:"page"`
	Size       int        `json:"size"`
	Total      int64      `json:"total"`
	TotalPages int64      `json:"total_pages"`
}
