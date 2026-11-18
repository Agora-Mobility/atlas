package business

import "strings"

// Every label value goes through one of these functions: a value outside the
// known set becomes "other", so a bug or a new enum value can never create
// an unbounded number of series.

const other = "other"

var (
	paymentMethods = set("cash", "wallet", "gpo", "reference")
	paymentKinds   = set("fare", "cancellation_fee", "tip", "extra")
	paymentSources = set("api", "webhook", "reconcile", "provider", "admin")
	requestActors  = set("customer", "provider", "operator", "system")
	bookingActors  = set("customer", "workshop", "provider", "operator", "system")
	channels       = set("customer_app", "provider_app", "partner_panel", "backoffice")
	roles          = set("customer", "provider", "staff")
	reviewOutcomes = set("approved", "rejected", "suspended")
	refundSources  = set("admin", "automatic")
	settledStates  = set("paid", "failed", "expired", "duplicate")
	bookingStates  = set("awaiting_payment", "confirmed", "vehicle_received", "in_service", "ready", "completed", "cancelled")
	// Statuses a request only reaches once a provider accepted it.
	acceptedStates = set("accepted", "provider_en_route", "in_progress")
)

func set(values ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(values))
	for _, v := range values {
		m[v] = struct{}{}
	}
	return m
}

func oneOf(known map[string]struct{}, value string) string {
	if _, ok := known[value]; ok {
		return value
	}
	return other
}

// category keeps the slug of the service category. Categories are a short
// catalog managed by the backoffice (taxi, towing, removals, maintenance),
// the same label the existing atlas_business_* metrics use.
func category(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(slug) > 32 {
		return other
	}
	return slug
}

// gateway is the payment gateway behind a method. AppyPay is the only one
// today; cash and wallet never leave Atlas.
func gateway(method string) string {
	switch method {
	case "gpo", "reference":
		return "appypay"
	case "cash", "wallet":
		return "none"
	default:
		return other
	}
}

// currency keeps ISO 4217 codes and nothing else.
func currency(code string) string {
	if len(code) != 3 {
		return other
	}
	return strings.ToUpper(code)
}

// failureReason sorts a payment that did not go through into a closed set.
// The gateway code itself goes to the log line and the span, not the metric.
func failureReason(status string, declined bool) string {
	switch {
	case status == "expired":
		return "expired"
	case status != "failed":
		return "none"
	case declined:
		return "declined"
	default:
		return "charge_failed"
	}
}

func stage(from string) string {
	if _, ok := acceptedStates[from]; ok {
		return "after_accept"
	}
	return "before_accept"
}

func mode(scheduled bool) string {
	if scheduled {
		return "scheduled"
	}
	return "now"
}
