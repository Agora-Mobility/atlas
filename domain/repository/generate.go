package repository

//go:generate go tool mockgen -source=outbox.go -destination=mocks/outbox_mock.go -package=mocks
//go:generate go tool mockgen -source=transaction.go -destination=mocks/transaction_mock.go -package=mocks
//go:generate go tool mockgen -source=user.go -destination=mocks/user_mock.go -package=mocks
//go:generate go tool mockgen -source=refresh_token.go -destination=mocks/refresh_token_mock.go -package=mocks
//go:generate go tool mockgen -source=client.go -destination=mocks/client_mock.go -package=mocks
