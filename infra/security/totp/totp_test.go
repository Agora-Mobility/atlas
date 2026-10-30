package totp

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCode_MatchesRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	vectors := map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	}
	for unix, want := range vectors {
		assert.Equal(t, want, Code(key, unix/period), unix)
	}
}

func TestMatch(t *testing.T) {
	a := NewAuthenticator("AGORA")
	secret, err := a.NewSecret()
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)

	code, err := CodeAt(secret, now)
	require.NoError(t, err)
	step, ok := a.Match(secret, code, now)
	assert.True(t, ok)
	assert.Equal(t, now.Unix()/period, step)

	previous, err := CodeAt(secret, now.Add(-period*time.Second))
	require.NoError(t, err)
	_, ok = a.Match(secret, previous, now)
	assert.True(t, ok, "one step of clock skew is accepted")

	old, err := CodeAt(secret, now.Add(-3*period*time.Second))
	require.NoError(t, err)
	_, ok = a.Match(secret, old, now)
	assert.False(t, ok)

	_, ok = a.Match(secret, "12345", now)
	assert.False(t, ok)
}

func TestURI(t *testing.T) {
	uri := NewAuthenticator("AGORA").URI("JBSWY3DPEHPK3PXP", "ana@agora.ao")

	assert.True(t, strings.HasPrefix(uri, "otpauth://totp/AGORA:ana@agora.ao?"))
	assert.Contains(t, uri, "secret=JBSWY3DPEHPK3PXP")
	assert.Contains(t, uri, "issuer=AGORA")
}
