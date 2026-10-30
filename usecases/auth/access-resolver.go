package usecases

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

type AccessResolver struct {
	membershipRepo repository.IMembershipRepository
	cache          services.IAccessCache
}

var _ services.IAccessResolver = (*AccessResolver)(nil)

func NewAccessResolver(membershipRepo repository.IMembershipRepository, cache services.IAccessCache) *AccessResolver {
	return &AccessResolver{membershipRepo: membershipRepo, cache: cache}
}

func (r *AccessResolver) Resolve(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error) {
	cached, err := r.cache.Get(ctx, membershipID)
	if err != nil {
		logger.FromContext(ctx).Warn("access cache unavailable, reading from the database", zap.Error(err))
	}
	if cached != nil {
		return cached, nil
	}

	access, err := r.membershipRepo.Access(ctx, membershipID)
	if err != nil || access == nil {
		return access, err
	}
	if err := r.cache.Set(ctx, access); err != nil {
		logger.FromContext(ctx).Warn("access not cached", zap.Error(err))
	}
	return access, nil
}

func (r *AccessResolver) Fresh(ctx context.Context, membershipID uuid.UUID) (*entities.Access, error) {
	return r.membershipRepo.Access(ctx, membershipID)
}

func (r *AccessResolver) Invalidate(ctx context.Context, membershipIDs ...uuid.UUID) {
	if err := r.cache.Delete(ctx, membershipIDs...); err != nil {
		logger.FromContext(ctx).Error("cached access not invalidated", zap.Error(err))
	}
}
