package middleware

import (
	"context"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

type authContextKey string

const (
	clientContextKey    authContextKey = "client"
	userIDContextKey    authContextKey = "user_id"
	sessionIDContextKey authContextKey = "session_id"
	roleContextKey      authContextKey = "role"
)

func WithClient(ctx context.Context, client *entities.Client) context.Context {
	return context.WithValue(ctx, clientContextKey, client)
}

func ClientFromContext(ctx context.Context) (*entities.Client, bool) {
	client, ok := ctx.Value(clientContextKey).(*entities.Client)
	return client, ok && client != nil
}

func WithAuthenticatedUser(ctx context.Context, userID, sessionID uuid.UUID, role enums.Role) context.Context {
	ctx = context.WithValue(ctx, userIDContextKey, userID)
	ctx = context.WithValue(ctx, sessionIDContextKey, sessionID)
	return context.WithValue(ctx, roleContextKey, role)
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(userIDContextKey).(uuid.UUID)
	return userID, ok
}

func SessionIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	sessionID, ok := ctx.Value(sessionIDContextKey).(uuid.UUID)
	return sessionID, ok
}

func RoleFromContext(ctx context.Context) (enums.Role, bool) {
	role, ok := ctx.Value(roleContextKey).(enums.Role)
	return role, ok
}
