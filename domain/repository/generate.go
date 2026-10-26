package repository

//go:generate go tool mockgen -source=example.go -destination=mocks/example_mock.go -package=mocks
//go:generate go tool mockgen -source=outbox.go -destination=mocks/outbox_mock.go -package=mocks
//go:generate go tool mockgen -source=transaction.go -destination=mocks/transaction_mock.go -package=mocks
