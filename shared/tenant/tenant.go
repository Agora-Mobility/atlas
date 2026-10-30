package tenant

import (
	"context"

	"github.com/google/uuid"
)

type scopeKey struct{}

type Scope struct {
	OrganizationID *uuid.UUID
	UserID         *uuid.UUID
}

func WithOrganization(ctx context.Context, organizationID uuid.UUID) context.Context {
	scope := From(ctx)
	scope.OrganizationID = &organizationID
	return context.WithValue(ctx, scopeKey{}, scope)
}

func WithUser(ctx context.Context, userID uuid.UUID) context.Context {
	scope := From(ctx)
	scope.UserID = &userID
	return context.WithValue(ctx, scopeKey{}, scope)
}

func From(ctx context.Context) Scope {
	scope, _ := ctx.Value(scopeKey{}).(Scope)
	return scope
}

func (s Scope) Settings() (organizationID, userID string) {
	if s.OrganizationID != nil {
		organizationID = s.OrganizationID.String()
	}
	if s.UserID != nil {
		userID = s.UserID.String()
	}
	return organizationID, userID
}
