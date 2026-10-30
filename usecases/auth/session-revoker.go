package usecases

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/domain/services"
)

// clockSkew widens the window of sessions denied by RevokeUser, so tokens
// issued by an instance with a slightly different clock are still covered.
const clockSkew = time.Minute

// SessionRevoker ends sessions at once: the refresh tokens stop working and
// the access tokens already issued are denied until they expire.
type SessionRevoker struct {
	refreshTokenRepo repository.IRefreshTokenRepository
	store            services.ISessionRevocationStore
	accessTTL        time.Duration
}

func NewSessionRevoker(refreshTokenRepo repository.IRefreshTokenRepository, store services.ISessionRevocationStore, accessTTL time.Duration) *SessionRevoker {
	return &SessionRevoker{refreshTokenRepo: refreshTokenRepo, store: store, accessTTL: accessTTL}
}

func (r *SessionRevoker) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	if err := r.refreshTokenRepo.RevokeFamily(ctx, sessionID, time.Now().UTC()); err != nil {
		return err
	}
	return r.store.RevokeSessions(ctx, sessionID)
}

// RevokeUser ends every session of the user. Call it inside the transaction
// of the change that requires it, so a failure to deny the access tokens
// rolls the change back instead of leaving live tokens behind.
func (r *SessionRevoker) RevokeUser(ctx context.Context, userID uuid.UUID) error {
	now := time.Now().UTC()
	if err := r.refreshTokenRepo.RevokeAllForUser(ctx, userID, now); err != nil {
		return err
	}
	sessions, err := r.refreshTokenRepo.ListSessionsIssuedSince(ctx, userID, now.Add(-r.accessTTL-clockSkew))
	if err != nil {
		return err
	}
	return r.store.RevokeSessions(ctx, sessions...)
}
