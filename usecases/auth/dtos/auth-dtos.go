package dtos

import (
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type RegisterInput struct {
	Name     string `json:"name" binding:"required,max=120" example:"Ana Domingos"`
	Phone    string `json:"phone" binding:"required,max=32" example:"+244923000000"`
	Email    string `json:"email" binding:"omitempty,email,max=254" example:"ana@exemplo.ao"`
	Password string `json:"password" binding:"required" example:"S3gredo!forte"`
}

type LoginInput struct {
	Identifier string `json:"identifier" binding:"required,max=254" example:"+244923000000"`
	Password   string `json:"password" binding:"required,max=128" example:"S3gredo!forte"`
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
	AccessToken      string    `json:"access_token"`
	TokenType        string    `json:"token_type" example:"Bearer"`
	ExpiresIn        int64     `json:"expires_in" example:"900"`
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	Client           string    `json:"client" example:"agora-customer-app"`
	User             UserView  `json:"user"`
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
