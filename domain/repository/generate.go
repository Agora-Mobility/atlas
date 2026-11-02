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
//go:generate go tool mockgen -source=provider_documents.go -destination=mocks/provider_documents_mock.go -package=mocks
//go:generate go tool mockgen -source=address.go -destination=mocks/address_mock.go -package=mocks
//go:generate go tool mockgen -source=fleet.go -destination=mocks/fleet_mock.go -package=mocks
//go:generate go tool mockgen -source=payments.go -destination=mocks/payments_mock.go -package=mocks
//go:generate go tool mockgen -source=cancellation.go -destination=mocks/cancellation_mock.go -package=mocks
//go:generate go tool mockgen -source=reference.go -destination=mocks/reference_mock.go -package=mocks
//go:generate go tool mockgen -source=vouchers.go -destination=mocks/vouchers_mock.go -package=mocks
//go:generate go tool mockgen -source=support.go -destination=mocks/support_mock.go -package=mocks
//go:generate go tool mockgen -source=live.go -destination=mocks/live_mock.go -package=mocks
//go:generate go tool mockgen -source=garage.go -destination=mocks/garage_mock.go -package=mocks
//go:generate go tool mockgen -source=bookings.go -destination=mocks/bookings_mock.go -package=mocks
//go:generate go tool mockgen -source=workshops.go -destination=mocks/workshops_mock.go -package=mocks
//go:generate go tool mockgen -source=ledger.go -destination=mocks/ledger_mock.go -package=mocks
