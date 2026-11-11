package entities

import (
	"time"

	"github.com/google/uuid"
)

const (
	NeutralDemandFactorBps = bpsDenominator
	MaxDemandFactorBps     = 30_000
	maxDemandTiers         = 10
	demandRiseWeightBps    = 7_000
	demandFallWeightBps    = 2_500
	demandSettledBps       = 100
)

type DemandMode string

const (
	DemandOff     DemandMode = "off"
	DemandObserve DemandMode = "observe"
	DemandOn      DemandMode = "on"
)

func (m DemandMode) Valid() bool {
	return m == DemandOff || m == DemandObserve || m == DemandOn
}

type DemandTier struct {
	AboveRatioBps int `json:"above_ratio_bps"`
	FactorBps     int `json:"factor_bps"`
}

type DemandPricing struct {
	ID                uuid.UUID
	ZoneID            uuid.UUID
	CategoryID        uuid.UUID
	Mode              DemandMode
	Tiers             []DemandTier
	MaxFactorBps      int
	CommissionOnSurge bool
	UpdatedBy         *uuid.UUID
	UpdatedAt         time.Time
}

func (d *DemandPricing) Charges() bool {
	return d.Mode == DemandOn
}

func (d *DemandPricing) Measures() bool {
	return d.Mode == DemandObserve || d.Mode == DemandOn
}

func (d *DemandPricing) Validate() error {
	if !d.Mode.Valid() {
		return validationError("mode", "O modo tem de ser off, observe ou on.")
	}
	if d.MaxFactorBps < NeutralDemandFactorBps || d.MaxFactorBps > MaxDemandFactorBps {
		return validationError("max_factor_bps", "O tecto tem de estar entre 10000 e 30000.")
	}
	if len(d.Tiers) > maxDemandTiers {
		return validationError("tiers", "No máximo 10 escalões.")
	}
	previousRatio, previousFactor := -1, NeutralDemandFactorBps
	for _, tier := range d.Tiers {
		if tier.AboveRatioBps <= previousRatio {
			return validationError("tiers", "As razões dos escalões têm de subir de escalão para escalão.")
		}
		if tier.FactorBps < previousFactor || tier.FactorBps > d.MaxFactorBps {
			return validationError("tiers", "O valor de cada escalão não pode descer nem passar o tecto.")
		}
		previousRatio, previousFactor = tier.AboveRatioBps, tier.FactorBps
	}
	return nil
}

func (d *DemandPricing) FactorFor(ratioBps int) int {
	factor := NeutralDemandFactorBps
	for _, tier := range d.Tiers {
		if ratioBps > tier.AboveRatioBps {
			factor = tier.FactorBps
		}
	}
	return d.withinCap(factor)
}

func (d *DemandPricing) Capped(factorBps int) int {
	if !d.Charges() {
		return NeutralDemandFactorBps
	}
	return d.withinCap(factorBps)
}

func (d *DemandPricing) withinCap(factorBps int) int {
	if factorBps < NeutralDemandFactorBps {
		return NeutralDemandFactorBps
	}
	if factorBps > d.MaxFactorBps {
		return d.MaxFactorBps
	}
	return factorBps
}

func DemandRatioBps(pending, free int) int {
	if pending < 0 {
		pending = 0
	}
	if free < 0 {
		free = 0
	}
	return (pending + 1) * bpsDenominator / (free + 1)
}

func SmoothDemandRatio(previousBps, currentBps int) int {
	weight := demandFallWeightBps
	if currentBps > previousBps {
		weight = demandRiseWeightBps
	}
	return previousBps + (currentBps-previousBps)*weight/bpsDenominator
}

func DemandSettled(smoothedBps, currentBps int) bool {
	return currentBps <= NeutralDemandFactorBps && smoothedBps <= NeutralDemandFactorBps+demandSettledBps
}

type DemandQuote struct {
	FactorBps   int
	Commissions bool
}

func NeutralDemand() DemandQuote {
	return DemandQuote{FactorBps: NeutralDemandFactorBps}
}

func (q DemandQuote) IsHigh() bool {
	return q.FactorBps > NeutralDemandFactorBps
}

type QuotedFare struct {
	BaseCents          int64
	DemandCents        int64
	TotalCents         int64
	CommissionCents    int64
	DemandFactorBps    int
	DemandCommissioned bool
}

func QuoteFare(baseCents int64, commissionBps int, demand DemandQuote) QuotedFare {
	factor := demand.FactorBps
	if factor < NeutralDemandFactorBps {
		factor = NeutralDemandFactorBps
	}
	total := roundedDivide(baseCents*int64(factor), bpsDenominator)
	commissionable := baseCents
	if demand.Commissions {
		commissionable = total
	}
	return QuotedFare{
		BaseCents: baseCents, DemandCents: total - baseCents, TotalCents: total, DemandFactorBps: factor, DemandCommissioned: demand.Commissions,
		CommissionCents: roundedDivide(commissionable*int64(commissionBps), bpsDenominator),
	}
}

type DemandArea struct {
	Area       int64
	Pending    int
	Free       int
	RatioBps   int
	FactorBps  int
	RecordedAt time.Time
}

type DemandSnapshot struct {
	ZoneID     uuid.UUID
	CategoryID uuid.UUID
	DemandArea
}
