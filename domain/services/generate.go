package services

//go:generate go tool mockgen -source=security.go -destination=mocks/security_mock.go -package=mocks
//go:generate go tool mockgen -source=notification.go -destination=mocks/notification_mock.go -package=mocks
//go:generate go tool mockgen -source=storage.go -destination=mocks/storage_mock.go -package=mocks
//go:generate go tool mockgen -source=verification.go -destination=mocks/verification_mock.go -package=mocks
