package sealer

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, keyBytes)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(raw)
}

func TestSealAndOpen(t *testing.T) {
	s, err := NewAESGCM(newKey(t))
	require.NoError(t, err)

	sealed, err := s.Seal("JBSWY3DPEHPK3PXP")
	require.NoError(t, err)
	assert.NotContains(t, sealed, "JBSWY3DPEHPK3PXP")

	opened, err := s.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "JBSWY3DPEHPK3PXP", opened)
}

func TestOpen_RejectsAnotherKeyAndTampering(t *testing.T) {
	s, err := NewAESGCM(newKey(t))
	require.NoError(t, err)
	other, err := NewAESGCM(newKey(t))
	require.NoError(t, err)
	sealed, err := s.Seal("secret")
	require.NoError(t, err)

	_, err = other.Open(sealed)
	assert.Error(t, err)

	raw, _ := base64.StdEncoding.DecodeString(sealed)
	raw[len(raw)-1] ^= 0xff
	_, err = s.Open(base64.StdEncoding.EncodeToString(raw))
	assert.Error(t, err)
}

func TestNewAESGCM_RejectsShortKeys(t *testing.T) {
	_, err := NewAESGCM(base64.StdEncoding.EncodeToString([]byte("short")))
	assert.ErrorIs(t, err, ErrInvalidKey)
}
