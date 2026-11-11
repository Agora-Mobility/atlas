package entities

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func exampleDemand() *DemandPricing {
	return &DemandPricing{
		Mode: DemandOn, MaxFactorBps: 13_000,
		Tiers: []DemandTier{{AboveRatioBps: 10_000, FactorBps: 11_000}, {AboveRatioBps: 15_000, FactorBps: 12_000}, {AboveRatioBps: 20_000, FactorBps: 13_000}},
	}
}

func TestDemandPricingFollowsTheTiersUpToTheCap(t *testing.T) {
	pricing := exampleDemand()
	require.NoError(t, pricing.Validate())

	assert.Equal(t, 10_000, pricing.FactorFor(10_000))
	assert.Equal(t, 11_000, pricing.FactorFor(10_001))
	assert.Equal(t, 11_000, pricing.FactorFor(15_000))
	assert.Equal(t, 12_000, pricing.FactorFor(17_272))
	assert.Equal(t, 13_000, pricing.FactorFor(90_000))

	pricing.MaxFactorBps = 12_000
	assert.Equal(t, 12_000, pricing.Capped(13_000))
}

func TestDemandModeDecidesWhetherTheFactorIsCharged(t *testing.T) {
	pricing := exampleDemand()
	assert.True(t, pricing.Measures())
	assert.True(t, pricing.Charges())

	pricing.Mode = DemandObserve
	assert.True(t, pricing.Measures())
	assert.False(t, pricing.Charges())
	assert.Equal(t, 13_000, pricing.FactorFor(90_000), "observing still records the factor that would apply")
	assert.Equal(t, NeutralDemandFactorBps, pricing.Capped(13_000), "but no quote pays it")

	pricing.Mode = DemandOff
	assert.False(t, pricing.Measures())
	assert.Equal(t, NeutralDemandFactorBps, pricing.Capped(13_000))
}

func TestDemandPricingRejectsBadTiers(t *testing.T) {
	cases := map[string]func(*DemandPricing){
		"unknown mode":              func(d *DemandPricing) { d.Mode = "auto" },
		"cap above the hard limit":  func(d *DemandPricing) { d.MaxFactorBps = 30_001 },
		"cap below normal":          func(d *DemandPricing) { d.MaxFactorBps = 9_999 },
		"ratios out of order":       func(d *DemandPricing) { d.Tiers[1].AboveRatioBps = 10_000 },
		"factor going down":         func(d *DemandPricing) { d.Tiers[2].FactorBps = 11_500 },
		"factor above the cap":      func(d *DemandPricing) { d.Tiers[2].FactorBps = 14_000 },
		"factor below normal price": func(d *DemandPricing) { d.Tiers[0].FactorBps = 9_000 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			pricing := exampleDemand()
			mutate(pricing)
			assert.Error(t, pricing.Validate())
		})
	}
}

func TestDemandRatioDampensSmallNumbers(t *testing.T) {
	assert.Equal(t, 17_272, DemandRatioBps(18, 10))
	assert.Equal(t, 20_000, DemandRatioBps(1, 0))
	assert.Equal(t, 10_000, DemandRatioBps(0, 0))
	assert.Equal(t, 1_000, DemandRatioBps(0, 9))
	assert.Equal(t, 10_000, DemandRatioBps(-3, -1))
}

func TestDemandRatioRisesFastAndFallsSlowly(t *testing.T) {
	assert.Equal(t, 17_000, SmoothDemandRatio(10_000, 20_000))
	assert.Equal(t, 17_500, SmoothDemandRatio(20_000, 10_000))
	assert.True(t, DemandSettled(10_050, 9_000))
	assert.False(t, DemandSettled(10_500, 9_000))
	assert.False(t, DemandSettled(10_000, 12_000))
}

func TestQuoteFareMatchesTheDocumentExample(t *testing.T) {
	normal := QuoteFare(270_000, 2_000, NeutralDemand())
	assert.Equal(t, QuotedFare{BaseCents: 270_000, TotalCents: 270_000, CommissionCents: 54_000, DemandFactorBps: 10_000}, normal)

	high := QuoteFare(270_000, 2_000, DemandQuote{FactorBps: 12_000})
	assert.Equal(t, int64(324_000), high.TotalCents)
	assert.Equal(t, int64(54_000), high.DemandCents)
	assert.Equal(t, int64(54_000), high.CommissionCents)

	commissioned := QuoteFare(270_000, 2_000, DemandQuote{FactorBps: 12_000, Commissions: true})
	assert.Equal(t, int64(64_800), commissioned.CommissionCents)
	assert.True(t, commissioned.DemandCommissioned)
}

func TestQuoteFareRoundsTheDemandPart(t *testing.T) {
	quote := QuoteFare(1_005, 0, DemandQuote{FactorBps: 11_000})
	assert.Equal(t, int64(1_106), quote.TotalCents)
	assert.Equal(t, int64(101), quote.DemandCents)
}

func TestEstimateKeepsItsDemandFactor(t *testing.T) {
	estimate := &FareEstimate{}
	assert.Equal(t, NeutralDemand(), estimate.Demand())

	estimate.Apply(QuoteFare(270_000, 2_000, DemandQuote{FactorBps: 12_000, Commissions: true}))
	assert.Equal(t, DemandQuote{FactorBps: 12_000, Commissions: true}, estimate.Demand())
	assert.Equal(t, int64(324_000), estimate.TotalCents)
}
