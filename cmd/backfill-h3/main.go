package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/infra/database/postgres"
	"github.com/Agora-Mobility/atlas/infra/geo/h3index"
	"github.com/Agora-Mobility/atlas/infra/repository"
	"github.com/Agora-Mobility/atlas/infra/rest/containers"
	"github.com/Agora-Mobility/atlas/usecases/dispatch"
)

const (
	defaultBatch = 1000
	rebuildStep  = 24 * time.Hour
	busyRetry    = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("backfill-h3: %v", err)
	}
}

func run() error {
	batch := flag.Int("batch", defaultBatch, "requests updated per statement")
	rebuild := flag.Duration("rebuild", 0, "rebuild heatmap_cells for this much history after the backfill, e.g. 168h")
	flag.Parse()
	if *batch < 1 || *rebuild < 0 {
		return errors.New("-batch must be positive and -rebuild cannot be negative")
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger.MustInit(logger.Options{ServiceName: cfg.App.Name, Environment: cfg.App.Env.String(), Output: cfg.Log.Output, Level: cfg.Log.Level})
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connection(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer pool.Close()

	uc := dispatch.NewUsecase(dispatch.Deps{
		Heatmap: repository.NewHeatmapRepository(pool),
		Geo:     h3index.New(),
		Tx:      postgres.NewTxManager(pool),
	}, dispatch.Settings{Heatmap: containers.HeatmapSettings(cfg.Heatmap)})

	updated, err := uc.BackfillPickupCells(ctx, *batch)
	if err != nil {
		return err
	}
	log.Printf("pickup cells set on %d requests", updated)

	if *rebuild == 0 {
		return nil
	}
	now := time.Now().UTC()
	for from := now.Add(-*rebuild); from.Before(now); from = from.Add(rebuildStep) {
		to := from.Add(rebuildStep)
		if to.After(now) {
			to = now
		}
		if err := rebuildBetween(ctx, uc, from, to); err != nil {
			return err
		}
	}
	log.Printf("heatmap cells rebuilt for the last %s", *rebuild)
	return nil
}

func rebuildBetween(ctx context.Context, uc *dispatch.Usecase, from, to time.Time) error {
	for {
		err := uc.AggregateHeatmapBetween(ctx, from, to)
		if !errors.Is(err, dispatch.ErrHeatmapBusy) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(busyRetry):
		}
	}
}
