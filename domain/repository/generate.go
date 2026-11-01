package repository

//go:generate go tool mockgen -source=outbox.go -destination=mocks/outbox_mock.go -package=mocks
//go:generate go tool mockgen -source=transaction.go -destination=mocks/transaction_mock.go -package=mocks
//go:generate go tool mockgen -source=user.go -destination=mocks/user_mock.go -package=mocks
//go:generate go tool mockgen -source=refresh_token.go -destination=mocks/refresh_token_mock.go -package=mocks
//go:generate go tool mockgen -source=client.go -destination=mocks/client_mock.go -package=mocks
//go:generate go tool mockgen -source=audit_log.go -destination=mocks/audit_log_mock.go -package=mocks
//go:generate go tool mockgen -source=block_reason.go -destination=mocks/block_reason_mock.go -package=mocks
//go:generate go tool mockgen -source=access.go -destination=mocks/access_mock.go -package=mocks
//go:generate go tool mockgen -source=catalog.go -destination=mocks/catalog_mock.go -package=mocks
//go:generate go tool mockgen -source=dispatch.go -destination=mocks/dispatch_mock.go -package=mocks
//go:generate go tool mockgen -source=onboarding.go -destination=mocks/onboarding_mock.go -package=mocks
