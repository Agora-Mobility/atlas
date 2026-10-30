package notifications_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/events"
	repomocks "github.com/Agora-Mobility/atlas/domain/repository/mocks"
	"github.com/Agora-Mobility/atlas/domain/services"
	servicemocks "github.com/Agora-Mobility/atlas/domain/services/mocks"
	"github.com/Agora-Mobility/atlas/shared/enums"
	"github.com/Agora-Mobility/atlas/usecases/notifications"
)

type emailFixture struct {
	handler *notifications.EmailEventsHandler
	users   *repomocks.MockIUserRepository
	orgs    *repomocks.MockIOrganizationRepository
	email   *servicemocks.MockIEmailSender
}

func newEmailFixture(t *testing.T) emailFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := emailFixture{
		users: repomocks.NewMockIUserRepository(ctrl),
		orgs:  repomocks.NewMockIOrganizationRepository(ctrl),
		email: servicemocks.NewMockIEmailSender(ctrl),
	}
	f.handler = notifications.NewEmailEventsHandler(f.users, f.orgs, f.email)
	return f
}

func staffWithEmail(t *testing.T, email string) *entities.User {
	t.Helper()
	user, err := entities.NewUser("Ana Domingos", email, "+244923000000", "$argon2id$hash", enums.RoleStaff)
	require.NoError(t, err)
	return user
}

func envelope(t *testing.T, evt events.Event) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(evt, "corr")
	require.NoError(t, err)
	return env
}

func TestEmailEvents_MemberNoticesNameTheOrganization(t *testing.T) {
	org, err := entities.NewPartnerOrganization("Transportes Kianda", "")
	require.NoError(t, err)
	user := staffWithEmail(t, "ana@agora.ao")
	change := events.NewMemberChange(uuid.New(), org.ID, user.ID, nil, 2)

	cases := map[string]struct {
		event   events.Event
		subject string
	}{
		"activated":     {events.MemberActivatedV1{MemberChange: change}, "A sua conta em Transportes Kianda está activa"},
		"roles changed": {events.MemberRolesChangedV1{MemberChange: change}, "Os seus acessos em Transportes Kianda foram alterados"},
		"suspended":     {events.MemberSuspendedV1{MemberChange: change}, "O seu acesso a Transportes Kianda foi suspenso"},
		"reactivated":   {events.MemberReactivatedV1{MemberChange: change}, "O seu acesso a Transportes Kianda foi reactivado"},
		"deactivated":   {events.MemberDeactivatedV1{MemberChange: change}, "O seu acesso a Transportes Kianda foi desactivado"},
		"mfa reset":     {events.MemberMFAResetV1{MemberChange: change}, "A verificação em dois passos da sua conta foi reposta"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newEmailFixture(t)
			env := envelope(t, tc.event)
			f.orgs.EXPECT().FindByID(gomock.Any(), org.ID).Return(org, nil)
			f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
			f.email.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, email services.Email) error {
				assert.Equal(t, "ana@agora.ao", email.To)
				assert.Equal(t, tc.subject, email.Subject)
				assert.Equal(t, env.EventType+"/"+env.EventID.String(), email.IdempotencyKey, "a redelivered event is not sent twice")
				return nil
			})

			require.NoError(t, f.handler.OnEvent(context.Background(), env))
		})
	}
}

func TestEmailEvents_AccountNotices(t *testing.T) {
	user, err := entities.NewUser("Bruno", "bruno@agora.ao", "+244923000001", "$argon2id$hash", enums.RoleCustomer)
	require.NoError(t, err)
	for name, evt := range map[string]events.Event{
		"blocked":   events.UserBlockedV1{ID: user.ID, Role: user.Role},
		"unblocked": events.UserUnblockedV1{ID: user.ID, Role: user.Role},
	} {
		t.Run(name, func(t *testing.T) {
			f := newEmailFixture(t)
			f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
			f.email.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, email services.Email) error {
				assert.Equal(t, "bruno@agora.ao", email.To)
				assert.Contains(t, email.Text, "Olá Bruno,")
				return nil
			})

			require.NoError(t, f.handler.OnEvent(context.Background(), envelope(t, evt)))
		})
	}
}

func TestEmailEvents_UserWithoutEmailIsSkipped(t *testing.T) {
	f := newEmailFixture(t)
	user := staffWithEmail(t, "")
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)

	require.NoError(t, f.handler.OnEvent(context.Background(), envelope(t, events.UserBlockedV1{ID: user.ID})))
}

func TestEmailEvents_ProviderFailureIsRetried(t *testing.T) {
	f := newEmailFixture(t)
	user := staffWithEmail(t, "ana@agora.ao")
	f.users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	f.email.EXPECT().Send(gomock.Any(), gomock.Any()).Return(services.ErrEmailUnavailable)

	err := f.handler.OnEvent(context.Background(), envelope(t, events.UserUnblockedV1{ID: user.ID}))

	require.ErrorIs(t, err, services.ErrEmailUnavailable)
}

func TestEmailEvents_EveryBoundEventHasAMessage(t *testing.T) {
	assert.ElementsMatch(t, []string{
		events.MemberActivated, events.MemberRolesChanged, events.MemberSuspended, events.MemberReactivated,
		events.MemberDeactivated, events.MemberMFAReset, events.UserBlocked, events.UserUnblocked,
	}, notifications.EmailEventTypes())
}
