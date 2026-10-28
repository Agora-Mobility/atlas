package enums

type ClientType string

const (
	ClientTypeCustomerApp  ClientType = "customer_app"
	ClientTypeProviderApp  ClientType = "provider_app"
	ClientTypePartnerPanel ClientType = "partner_panel"
	ClientTypeBackoffice   ClientType = "backoffice"
)

func (t ClientType) IsValid() bool {
	switch t {
	case ClientTypeCustomerApp, ClientTypeProviderApp, ClientTypePartnerPanel, ClientTypeBackoffice:
		return true
	}
	return false
}

func (t ClientType) String() string {
	return string(t)
}
