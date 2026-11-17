package usecases_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/repository"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

type auditReaderStub struct {
	filter  repository.AuditFilter
	records []repository.AuditRecord
}

func (s *auditReaderStub) List(_ context.Context, filter repository.AuditFilter) ([]repository.AuditRecord, int64, error) {
	s.filter = filter
	return s.records, int64(len(s.records)), nil
}

func TestTheAuditLogHidesSecrets(t *testing.T) {
	actor := uuid.New()
	reader := &auditReaderStub{records: []repository.AuditRecord{
		{ID: uuid.New(), ActorID: &actor, ActorRole: "staff", ActorName: "Ana", Action: "user.reset-password", EntityType: "user", EntityID: "u1",
			After: json.RawMessage(`{"name":"Rui","password_hash":"x","nested":{"refresh_token":"y","keys":[{"api_key":"z","label":"ok"}]}}`)},
		{ID: uuid.New(), Action: "request.expire", EntityType: "service_request", EntityID: "r1"},
	}}
	page, err := usecases.NewAuditUsecase(reader).List(context.Background(), dtos.AuditListQuery{From: "2026-11-01", Size: 10})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)

	assert.JSONEq(t, `{"name":"Rui","password_hash":"***","nested":{"refresh_token":"***","keys":[{"api_key":"***","label":"ok"}]}}`, string(page.Items[0].After))
	assert.Equal(t, "staff", page.Items[0].Actor.Type)
	assert.Equal(t, "Ana", page.Items[0].Actor.Name)
	assert.Equal(t, "system", page.Items[1].Actor.Type, "no actor is the system")
	assert.Nil(t, page.Items[1].Before)
	assert.Equal(t, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), *reader.filter.From)
	assert.Equal(t, 10, reader.filter.Limit)
}

func TestTheAuditFiltersAreChecked(t *testing.T) {
	uc := usecases.NewAuditUsecase(&auditReaderStub{})
	_, err := uc.List(context.Background(), dtos.AuditListQuery{From: "ontem"})
	assert.ErrorIs(t, err, usecases.ErrAuditFilter)
}
