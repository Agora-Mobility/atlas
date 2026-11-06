package middleware

import (
	"context"
	"time"

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
	membershipKey       authContextKey = "membership"
	accessKey           authContextKey = "access"
	expiryContextKey    authContextKey = "session_expiry"
)

type TokenMembership struct {
	MembershipID   uuid.UUID
	OrganizationID uuid.UUID
	Scope          enums.OrganizationType
}

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

func WithTokenMembership(ctx context.Context, membership TokenMembership) context.Context {
	return context.WithValue(ctx, membershipKey, membership)
}

func TokenMembershipFromContext(ctx context.Context) (TokenMembership, bool) {
	membership, ok := ctx.Value(membershipKey).(TokenMembership)
	return membership, ok
}

func WithAccess(ctx context.Context, access *entities.Access) context.Context {
	return context.WithValue(ctx, accessKey, access)
}

func AccessFromContext(ctx context.Context) (*entities.Access, bool) {
	access, ok := ctx.Value(accessKey).(*entities.Access)
	return access, ok && access != nil
}

func WithTokenExpiry(ctx context.Context, expiresAt time.Time) context.Context {
	return context.WithValue(ctx, expiryContextKey, expiresAt)
}

func TokenExpiryFromContext(ctx context.Context) time.Time {
	expiresAt, _ := ctx.Value(expiryContextKey).(time.Time)
	return expiresAt
}
