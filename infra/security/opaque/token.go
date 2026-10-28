package opaque

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const tokenBytes = 32

type TokenGenerator struct{}

var _ services.IOpaqueTokenGenerator = TokenGenerator{}

func (g TokenGenerator) Generate() (token, tokenHash string, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate opaque token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, g.Hash(token), nil
}

func (TokenGenerator) Hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
