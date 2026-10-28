package password_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/infra/security/password"
)

var fastParams = password.Params{MemoryKiB: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestArgon2HasherRoundTrip(t *testing.T) {
	hasher := password.NewArgon2Hasher(fastParams)

	hash, err := hasher.Hash("S3gredo!forte")
	require.NoError(t, err)
	assert.Contains(t, hash, "$argon2id$v=19$m=1024,t=1,p=1$")

	matches, needsRehash, err := hasher.Verify("S3gredo!forte", hash)
	require.NoError(t, err)
	assert.True(t, matches)
	assert.False(t, needsRehash)

	matches, _, err = hasher.Verify("errada", hash)
	require.NoError(t, err)
	assert.False(t, matches)
}

func TestArgon2HasherSaltsEveryHash(t *testing.T) {
	hasher := password.NewArgon2Hasher(fastParams)

	first, err := hasher.Hash("igual")
	require.NoError(t, err)
	second, err := hasher.Hash("igual")
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

func TestArgon2HasherFlagsOutdatedParams(t *testing.T) {
	oldHash, err := password.NewArgon2Hasher(fastParams).Hash("senha-antiga")
	require.NoError(t, err)

	stronger := fastParams
	stronger.Iterations = 2
	matches, needsRehash, err := password.NewArgon2Hasher(stronger).Verify("senha-antiga", oldHash)

	require.NoError(t, err)
	assert.True(t, matches)
	assert.True(t, needsRehash)
}

func TestArgon2HasherRejectsMalformedHash(t *testing.T) {
	_, _, err := password.NewArgon2Hasher(fastParams).Verify("x", "$bcrypt$nope")

	require.ErrorIs(t, err, password.ErrMalformedHash)
}
