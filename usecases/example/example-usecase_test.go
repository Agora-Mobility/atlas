package example_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	"github.com/Agora-Mobility/atlas/domain/repository/mocks"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	"github.com/Agora-Mobility/atlas/shared/pagination"
	"github.com/Agora-Mobility/atlas/usecases/example"
	"github.com/Agora-Mobility/atlas/usecases/example/dtos"
)

type fixture struct {
	repo   *mocks.MockIExampleRepository
	outbox *mocks.MockIOutboxRepository
	tx     *mocks.MockITransactionManager
	uc     *example.ExampleUseCase
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := fixture{
		repo:   mocks.NewMockIExampleRepository(ctrl),
		outbox: mocks.NewMockIOutboxRepository(ctrl),
		tx:     mocks.NewMockITransactionManager(ctrl),
	}
	f.uc = example.NewExampleUseCase(f.repo, f.outbox, f.tx)
	return f
}

// runInline makes the transaction mock execute the callback directly.
func (f fixture) runInline() {
	f.tx.EXPECT().WithinTransaction(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
}

func validInput() dtos.CreateExampleInput {
	return dtos.CreateExampleInput{Name: "Talatona", Description: "x", Latitude: -8.91, Longitude: 13.18}
}

func TestCreate_PersistsExampleAndEventInSameTransaction(t *testing.T) {
	f := newFixture(t)
	ctx := logger.WithCorrelationID(context.Background(), "corr-1")

	var saved *entities.Example
	f.runInline()
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, e *entities.Example) error { saved = e; return nil })
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, env events.Envelope) error {
			assert.Equal(t, "example.created", env.EventType)
			assert.Equal(t, 1, env.Version)
			assert.Equal(t, "example.created.v1", env.RoutingKey())
			assert.Equal(t, "corr-1", env.CorrelationID)
			assert.Equal(t, saved.ID.String(), env.AggregateID)

			var payload events.ExampleCreatedV1
			require.NoError(t, env.DecodePayload(&payload))
			assert.Equal(t, "Talatona", payload.Name)
			return nil
		})

	out, err := f.uc.Create(ctx, validInput())

	require.NoError(t, err)
	assert.Equal(t, saved.ID, out.ID)
	assert.Equal(t, "Talatona", out.Name)
}

func TestCreate_InvalidInputTouchesNoRepository(t *testing.T) {
	f := newFixture(t)
	in := validInput()
	in.Name = "   "
	in.Latitude = 91

	_, err := f.uc.Create(context.Background(), in)

	appErr := customerrors.As(err)
	require.Equal(t, customerrors.CodeValidation, appErr.Code)
	assert.Contains(t, appErr.Details, "name")
	assert.Contains(t, appErr.Details, "latitude")
}

func TestCreate_OutboxFailureFailsTheOperation(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("outbox down")

	f.runInline()
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.outbox.EXPECT().Save(gomock.Any(), gomock.Any()).Return(boom)

	_, err := f.uc.Create(context.Background(), validInput())

	require.ErrorIs(t, err, boom)
}

func TestGetByID_PropagatesNotFound(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.repo.EXPECT().FindByID(gomock.Any(), id).Return(nil, customerrors.NotFound("example"))

	_, err := f.uc.GetByID(context.Background(), dtos.GetExampleInput{ID: id})

	assert.Equal(t, customerrors.CodeNotFound, customerrors.As(err).Code)
}

func TestList_NormalizesPaginationAndBuildsPage(t *testing.T) {
	f := newFixture(t)
	e, err := entities.NewExample("A", "", 0, 0)
	require.NoError(t, err)

	f.repo.EXPECT().List(gomock.Any(), pagination.Params{Page: 1, Size: pagination.MaxSize}).
		Return([]*entities.Example{e}, int64(250), nil)

	page, err := f.uc.List(context.Background(), dtos.ListExamplesInput{Params: pagination.Params{Page: 0, Size: 1000}})

	require.NoError(t, err)
	assert.Len(t, page.Items, 1)
	assert.Equal(t, int64(3), page.TotalPages)
	assert.Equal(t, pagination.MaxSize, page.Size)
}
