package entities

import (
	"strings"
	"time"

	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/shared/phone"
)

type User struct {
	Base        `valid:"-"`
	Name        string           `json:"name" valid:"required~O nome é obrigatório.,runelength(2|120)~O nome deve ter entre 2 e 120 caracteres."`
	Email       string           `json:"email" valid:"email~O e-mail é inválido.,optional"`
	Password    string           `json:"-" valid:"required~A palavra-passe é obrigatória."`
	Phone       string           `json:"phone" valid:"phone~O número de telefone é inválido (ex: 923000000 ou +244923000000).,optional"`
	IsVerified  bool             `json:"is_verified" valid:"-"`
	Status      enums.UserStatus `json:"status" valid:"required~O estado é obrigatório.,user_status~O estado é inválido."`
	Role        enums.Role       `json:"role" valid:"required~O perfil é obrigatório.,user_role~O perfil é inválido."`
	LastLoginAt *time.Time       `json:"last_login_at" valid:"-"`
}

func NewUser(name, email, phoneNumber, passwordHash string, role enums.Role) (*User, error) {
	base, err := newBase()
	if err != nil {
		return nil, err
	}
	u := &User{
		Base:     base,
		Name:     strings.TrimSpace(name),
		Email:    normalizeEmail(email),
		Password: passwordHash,
		Phone:    phone.Normalize(phoneNumber),
		Status:   enums.UserStatusActive,
		Role:     role,
	}
	if err := u.isValid(); err != nil {
		return nil, err
	}
	return u, nil
}

func (u *User) UpdateProfile(name, email, phoneNumber string) error {
	updated := *u
	updated.Name = strings.TrimSpace(name)
	updated.Email = normalizeEmail(email)
	updated.Phone = phone.Normalize(phoneNumber)
	if err := updated.isValid(); err != nil {
		return err
	}
	updated.Touch()
	*u = updated
	return nil
}

func (u *User) ChangePassword(passwordHash string) error {
	updated := *u
	updated.Password = passwordHash
	if err := updated.isValid(); err != nil {
		return err
	}
	updated.Touch()
	*u = updated
	return nil
}

func (u *User) MarkVerified() {
	u.IsVerified = true
	u.Touch()
}

func (u *User) Block() {
	u.Status = enums.UserStatusBlocked
	u.Touch()
}

func (u *User) Activate() {
	u.Status = enums.UserStatusActive
	u.Touch()
}

func (u *User) RecordLogin() {
	now := time.Now().UTC()
	u.LastLoginAt = &now
}

func (u *User) IsBlocked() bool {
	return u.Status == enums.UserStatusBlocked
}

func (u *User) CanLogin() bool {
	return !u.IsDeleted() && u.Status == enums.UserStatusActive
}

func (u *User) IsInactive() bool {
	return u.Status == enums.UserStatusInactive
}

func (u *User) isValid() error {
	if err := validate(u); err != nil {
		return err
	}
	if u.Email == "" && u.Phone == "" {
		return validationError("email", "Indique o e-mail ou o telefone.")
	}
	return nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
