package repository

import "errors"

var (
	ErrNotFound           = errors.New("record not found")
	ErrEmailAlreadyExists = errors.New("email already registered")
	ErrPhoneAlreadyExists = errors.New("phone already registered")
)
