package notifications_test

import (
	"context"
	"testing"
	"time"

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

func phoneVerified(t *testing.T, user *entities.User) events.Envelope {
	t.Helper()
	env, err := events.NewEnvelope(events.UserPhoneVerifiedV1{ID: user.ID, VerifiedAt: time.Now().UTC()}, "corr")
	require.NoError(t, err)
	return env
}

func newHandler(t *testing.T) (*notifications.NotificationEventsHandler, *repomocks.MockIUserRepository, *servicemocks.MockISMSSender) {
	t.Helper()
	ctrl := gomock.NewController(t)
	users := repomocks.NewMockIUserRepository(ctrl)
	sms := servicemocks.NewMockISMSSender(ctrl)
	return notifications.NewNotificationEventsHandler(users, sms), users, sms
}

func newUser(t *testing.T) *entities.User {
	t.Helper()
	user, err := entities.NewUser("Ana Domingos", "", "+244923000000", "$argon2id$hash", enums.RoleCustomer)
	require.NoError(t, err)
	return user
}

func TestOnPhoneVerified_SendsWelcomeSMSWithFirstName(t *testing.T) {
	handler, users, sms := newHandler(t)
	user := newUser(t)
	users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	sms.EXPECT().Send(gomock.Any(), "+244923000000",
		"AGORA: bem-vindo(a), Ana! A sua conta foi confirmada e já pode usar a aplicação.").Return(nil)

	require.NoError(t, handler.OnPhoneVerified(context.Background(), phoneVerified(t, user)))
}

func TestOnPhoneVerified_SMSFailureIsRetried(t *testing.T) {
	handler, users, sms := newHandler(t)
	user := newUser(t)
	users.EXPECT().FindByID(gomock.Any(), user.ID).Return(user, nil)
	sms.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).Return(services.ErrSMSUnavailable)

	err := handler.OnPhoneVerified(context.Background(), phoneVerified(t, user))

	require.ErrorIs(t, err, services.ErrSMSUnavailable)
}

func TestOnPhoneVerified_DeletedUserIsSkipped(t *testing.T) {
	handler, users, _ := newHandler(t)
	user := newUser(t)
	users.EXPECT().FindByID(gomock.Any(), user.ID).Return(nil, nil)

	require.NoError(t, handler.OnPhoneVerified(context.Background(), phoneVerified(t, user)))
}

func TestOnPhoneVerified_MalformedPayloadFails(t *testing.T) {
	handler, _, _ := newHandler(t)
	env := events.Envelope{EventType: events.UserPhoneVerified, Payload: []byte(`{"id":"not-a-uuid"}`)}

	require.Error(t, handler.OnPhoneVerified(context.Background(), env))
}
