package entities

import (
	"crypto/rand"
	"errors"
	"math/big"
	"slices"
	"time"

	"github.com/google/uuid"
)

const (
	referralCodeLength   = 8
	referralCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
)

var (
	ErrInboxItemNotFound = errors.New("notification not found")
	ErrReferralUnknown   = errors.New("this invitation code does not exist")
	ErrReferralOwnCode   = errors.New("you cannot use your own code")
	ErrReferralUsed      = errors.New("an invitation code was already used on this account")
	ErrReferralTooLate   = errors.New("an invitation only counts before the first paid request")
)

type Preferences struct {
	NotifyStatus     bool
	NotifyPromotions bool
	NotifySMS        bool
	ShareLocation    bool
	Language         string
	Currency         string
	// NavigationApp is the app the driver opens to navigate a ride; empty until chosen
	NavigationApp string
}

// NavigationApps are the values of Preferences.NavigationApp besides empty.
var NavigationApps = []string{"google_maps", "waze", "apple_maps"}

func (p *Preferences) Validate() error {
	if !slices.Contains([]string{"pt", "en", "fr"}, p.Language) {
		return validationError("language", "O idioma não é válido.")
	}
	if !slices.Contains([]string{"AOA", "USD", "EUR"}, p.Currency) {
		return validationError("currency", "A moeda não é válida.")
	}
	if p.NavigationApp != "" && !slices.Contains(NavigationApps, p.NavigationApp) {
		return validationError("navigation_app", "A aplicação de navegação não é válida.")
	}
	return nil
}

// AllowsPush says whether a push about the state of an order may go out. A push
// that asks for an action of a provider, such as an offer, is not filtered.
func (p *Preferences) AllowsStatusPush() bool {
	return p.NotifyStatus
}

func (p *Preferences) AllowsSMS() bool {
	return p.NotifySMS
}

type InboxItem struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Kind      string
	Title     string
	Body      string
	LinkType  string
	LinkID    *uuid.UUID
	EventID   string
	ReadAt    *time.Time
	CreatedAt time.Time
}

func NewReferralCode() (string, error) {
	code := make([]byte, referralCodeLength)
	limit := big.NewInt(int64(len(referralCodeAlphabet)))
	for i := range code {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", err
		}
		code[i] = referralCodeAlphabet[index.Int64()]
	}
	return string(code), nil
}

func ReferralLegs(referrerID uuid.UUID, cents int64) []LedgerLeg {
	return []LedgerLeg{{AccountType: AccountPlatform, AmountCents: -cents}, {AccountType: AccountWallet, AccountID: &referrerID, AmountCents: cents}}
}
