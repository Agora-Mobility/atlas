package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"

	"github.com/Agora-Mobility/atlas/domain/services"
)

var ErrMalformedHash = errors.New("malformed argon2id hash")

type Params struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var DefaultParams = Params{
	MemoryKiB:   64 * 1024,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

type Argon2Hasher struct {
	params Params
	slots  chan struct{}
}

var _ services.IPasswordHasher = (*Argon2Hasher)(nil)

func NewArgon2Hasher(params Params) *Argon2Hasher {
	return &Argon2Hasher{params: params, slots: make(chan struct{}, runtime.NumCPU())}
}

func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, h.params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := h.derive(password, salt, h.params)
	return encode(h.params, salt, key), nil
}

func (h *Argon2Hasher) Verify(password, encodedHash string) (matches, needsRehash bool, err error) {
	params, salt, key, err := decode(encodedHash)
	if err != nil {
		return false, false, err
	}
	candidate := h.derive(password, salt, params)
	if subtle.ConstantTimeCompare(candidate, key) != 1 {
		return false, false, nil
	}
	return true, params != h.params, nil
}

func (h *Argon2Hasher) derive(password string, salt []byte, params Params) []byte {
	h.slots <- struct{}{}
	defer func() { <-h.slots }()
	return argon2.IDKey([]byte(password), salt, params.Iterations, params.MemoryKiB, params.Parallelism, params.KeyLength)
}

func encode(params Params, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.MemoryKiB, params.Iterations, params.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	)
}

func decode(encodedHash string) (Params, []byte, []byte, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return Params{}, nil, nil, ErrMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return Params{}, nil, nil, ErrMalformedHash
	}

	var params Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.MemoryKiB, &params.Iterations, &params.Parallelism); err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, ErrMalformedHash
	}
	params.SaltLength = uint32(len(salt)) // #nosec G115 -- salt length is bounded by the encoded hash
	params.KeyLength = uint32(len(key))   // #nosec G115 -- key length is bounded by the encoded hash
	return params, salt, key, nil
}
