package usecases_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	domainrepository "github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
	usecases "github.com/Agora-Mobility/atlas/usecases/auth"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

func TestUpdatePartner_ChangesTheDetailsWithAuditAndEvent(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	partner, err := entities.NewPartnerOrganization("Transportes Kianda", "5417123456")
	require.NoError(t, err)
	f.orgs.EXPECT().FindByID(gomock.Any(), partner.ID).Return(partner, nil)
	f.orgs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, o *entities.Organization) error {
		assert.Equal(t, "Kianda, Lda", o.Name)
		assert.Equal(t, "5417123456", o.NIF, "fields not sent stay")
		return nil
	})
	f.expectOutbox(t, events.PartnerUpdated)
	f.audit.EXPECT().Record(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *entities.AuditEntry) error {
		assert.Equal(t, "partner.update", e.Action)
		assert.Equal(t, partner.ID, *e.OrganizationID)
		assert.Contains(t, string(e.Before), "Transportes Kianda")
		assert.Contains(t, string(e.After), "Kianda, Lda")
		return nil
	})

	out, err := f.organizations.UpdatePartner(context.Background(), actorWith(r.platform, r.superAdmin), partner.ID, dtos.UpdatePartnerInput{Name: ptr("Kianda, Lda")})

	require.NoError(t, err)
	assert.Equal(t, "Kianda, Lda", out.Name)
}

func TestUpdatePartner_NoChangeWritesNothing(t *testing.T) {
	r := newRBAC()
	f := newFixture(t)
	partner, err := entities.NewPartnerOrganization("Transportes Kianda", "")
	require.NoError(t, err)
	f.orgs.EXPECT().FindByID(gomock.Any(), partner.ID).Return(partner, nil)

	out, err := f.organizations.UpdatePartner(context.Background(), actorWith(r.platform, r.superAdmin), partner.ID, dtos.UpdatePartnerInput{Name: ptr(" Transportes Kianda ")})

	require.NoError(t, err)
	assert.Equal(t, "Transportes Kianda", out.Name)
}

func TestUpdatePartner_Rejections(t *testing.T) {
	r := newRBAC()

	t.Run("platform organization", func(t *testing.T) {
		f := newFixture(t)
		f.orgs.EXPECT().FindByID(gomock.Any(), r.platform.ID).Return(r.platform, nil)

		_, err := f.organizations.UpdatePartner(context.Background(), actorWith(r.platform, r.superAdmin), r.platform.ID, dtos.UpdatePartnerInput{Name: ptr("Outra")})

		require.ErrorIs(t, err, usecases.ErrOrganizationNotFound)
	})

	t.Run("invalid nif", func(t *testing.T) {
		f := newFixture(t)
		partner, err := entities.NewPartnerOrganization("Kianda", "")
		require.NoError(t, err)
		f.orgs.EXPECT().FindByID(gomock.Any(), partner.ID).Return(partner, nil)

		_, err = f.organizations.UpdatePartner(context.Background(), actorWith(r.platform, r.superAdmin), partner.ID, dtos.UpdatePartnerInput{NIF: ptr("123")})

		_, isValidation := customerrors.AsValidation(err)
		assert.True(t, isValidation)
	})

	t.Run("nif of another partner", func(t *testing.T) {
		f := newFixture(t)
		partner, err := entities.NewPartnerOrganization("Kianda", "")
		require.NoError(t, err)
		f.orgs.EXPECT().FindByID(gomock.Any(), partner.ID).Return(partner, nil)
		f.orgs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(domainrepository.ErrNIFAlreadyExists)

		_, err = f.organizations.UpdatePartner(context.Background(), actorWith(r.platform, r.superAdmin), partner.ID, dtos.UpdatePartnerInput{NIF: ptr("5417123456")})

		require.ErrorIs(t, err, usecases.ErrNIFAlreadyExists)
	})
}

func TestGetPartner_ReturnsThePartner(t *testing.T) {
	f := newFixture(t)
	partner, err := entities.NewPartnerOrganization("Transportes Kianda", "5417123456")
	require.NoError(t, err)
	f.orgs.EXPECT().FindByID(gomock.Any(), partner.ID).Return(partner, nil)

	out, err := f.organizations.GetPartner(context.Background(), partner.ID)

	require.NoError(t, err)
	assert.Equal(t, partner.ID, out.ID)
	assert.Equal(t, "Transportes Kianda", out.Name)
	assert.Equal(t, "5417123456", out.NIF)
}

func TestGetPartner_OnlyPartners(t *testing.T) {
	r := newRBAC()

	t.Run("platform organization", func(t *testing.T) {
		f := newFixture(t)
		f.orgs.EXPECT().FindByID(gomock.Any(), r.platform.ID).Return(r.platform, nil)

		_, err := f.organizations.GetPartner(context.Background(), r.platform.ID)

		require.ErrorIs(t, err, usecases.ErrOrganizationNotFound)
	})

	t.Run("unknown id", func(t *testing.T) {
		f := newFixture(t)
		id := uuid.New()
		f.orgs.EXPECT().FindByID(gomock.Any(), id).Return(nil, nil)

		_, err := f.organizations.GetPartner(context.Background(), id)

		require.ErrorIs(t, err, usecases.ErrOrganizationNotFound)
	})
}
