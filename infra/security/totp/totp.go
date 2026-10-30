package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP uses HMAC-SHA1, the algorithm every authenticator app supports
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const (
	secretBytes = 20
	period      = 30
	digits      = 6
	skewSteps   = 1
)

var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type Authenticator struct {
	issuer string
}

var _ services.ITOTP = (*Authenticator)(nil)

func NewAuthenticator(issuer string) *Authenticator {
	return &Authenticator{issuer: issuer}
}

func (a *Authenticator) NewSecret() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return encoding.EncodeToString(raw), nil
}

func (a *Authenticator) URI(secret, account string) string {
	label := url.PathEscape(a.issuer + ":" + account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", a.issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(digits))
	query.Set("period", fmt.Sprint(period))
	return "otpauth://totp/" + label + "?" + query.Encode()
}

func (a *Authenticator) Match(secret, code string, at time.Time) (int64, bool) {
	key, err := encoding.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil || len(strings.TrimSpace(code)) != digits {
		return 0, false
	}
	current := at.Unix() / period
	for offset := int64(-skewSteps); offset <= skewSteps; offset++ {
		step := current + offset
		if subtle.ConstantTimeCompare([]byte(Code(key, step)), []byte(strings.TrimSpace(code))) == 1 {
			return step, true
		}
	}
	return 0, false
}

func Code(key []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step)) // #nosec G115 -- time steps are positive
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", digits, value%1_000_000)
}

func CodeAt(secret string, at time.Time) (string, error) {
	key, err := encoding.DecodeString(secret)
	if err != nil {
		return "", err
	}
	return Code(key, at.Unix()/period), nil
}
