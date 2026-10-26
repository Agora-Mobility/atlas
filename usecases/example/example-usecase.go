// Package example is the reference usecase: it shows how a usecase writes an
// entity and its domain event atomically through the outbox.
package example

import (
	"context"
	"fmt"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/example/dtos"
)

type ExampleUseCase struct {
	repo   repository.IExampleRepository
	outbox repository.IOutboxRepository
	tx     repository.ITransactionManager
}

func NewExampleUseCase(
	repo repository.IExampleRepository,
	outbox repository.IOutboxRepository,
	tx repository.ITransactionManager,
) *ExampleUseCase {
	return &ExampleUseCase{repo: repo, outbox: outbox, tx: tx}
}

// Create stores the example and its example.created.v1 event in one
// transaction, so the event is published if and only if the row exists.
func (uc *ExampleUseCase) Create(ctx context.Context, input dtos.CreateExampleInput) (*dtos.ExampleOutput, error) {
	example, err := entities.NewExample(input.Name, input.Description, input.Latitude, input.Longitude)
	if err != nil {
		return nil, err
	}

	env, err := events.NewEnvelope(events.ExampleCreatedV1{
		ID:        example.ID,
		Name:      example.Name,
		Latitude:  example.Location.Latitude,
		Longitude: example.Location.Longitude,
		CreatedAt: example.CreatedAt,
	}, logger.CorrelationID(ctx))
	if err != nil {
		return nil, err
	}

	err = uc.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := uc.repo.Create(ctx, example); err != nil {
			return err
		}
		return uc.outbox.Save(ctx, env)
	})
	if err != nil {
		return nil, fmt.Errorf("create example: %w", err)
	}

	out := dtos.ToExampleOutput(example)
	return &out, nil
}

func (uc *ExampleUseCase) GetByID(ctx context.Context, input dtos.GetExampleInput) (*dtos.ExampleOutput, error) {
	example, err := uc.repo.FindByID(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	out := dtos.ToExampleOutput(example)
	return &out, nil
}

func (uc *ExampleUseCase) List(ctx context.Context, input dtos.ListExamplesInput) (*dtos.ExamplePageOutput, error) {
	params := input.Normalize()
	items, total, err := uc.repo.List(ctx, params)
	if err != nil {
		return nil, err
	}
	out := make([]dtos.ExampleOutput, len(items))
	for i, e := range items {
		out[i] = dtos.ToExampleOutput(e)
	}
	page := pagination.NewPage(out, params, total)
	return &page, nil
}
