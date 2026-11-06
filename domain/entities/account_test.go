package entities_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/entities"
)

func TestPreferencesOnlyAcceptKnownValues(t *testing.T) {
	assert.NoError(t, (&entities.Preferences{Language: "pt", Currency: "AOA"}).Validate())
	assert.Error(t, (&entities.Preferences{Language: "xx", Currency: "AOA"}).Validate())
	assert.Error(t, (&entities.Preferences{Language: "pt", Currency: "XXX"}).Validate())
	assert.True(t, (&entities.Preferences{NotifySMS: true}).AllowsSMS())
	assert.False(t, (&entities.Preferences{NotifyStatus: false}).AllowsStatusPush())
}

func TestReferralCodeIsReadableAndUnique(t *testing.T) {
	first, err := entities.NewReferralCode()
	require.NoError(t, err)
	second, err := entities.NewReferralCode()
	require.NoError(t, err)
	assert.Regexp(t, `^[A-HJ-NP-Z2-9]{8}$`, first)
	assert.NotEqual(t, first, second)
}

func TestReferralLegsAddUpToZero(t *testing.T) {
	referrer := uuid.New()
	legs := entities.ReferralLegs(referrer, 100_000)
	assert.Zero(t, legs[0].AmountCents+legs[1].AmountCents)
	assert.Equal(t, entities.AccountWallet, legs[1].AccountType)
	assert.Equal(t, &referrer, legs[1].AccountID)
}
