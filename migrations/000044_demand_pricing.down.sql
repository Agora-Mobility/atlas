DELETE FROM fare_adjustments WHERE reason = 'demand';
ALTER TABLE fare_adjustments DROP CONSTRAINT fare_adjustments_reason_check;
ALTER TABLE fare_adjustments ADD CONSTRAINT fare_adjustments_reason_check CHECK (reason IN ('boost', 'waiting', 'route_change'));

ALTER TABLE fare_estimates
    DROP COLUMN demand_commissioned,
    DROP COLUMN demand_cents,
    DROP COLUMN demand_factor_bps;

DROP TABLE demand_snapshots;
DROP TABLE demand_pricing;
