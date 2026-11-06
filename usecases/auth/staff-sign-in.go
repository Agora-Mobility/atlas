package usecases

import (
	"context"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared/tenant"
	"github.com/Agora-Mobility/atlas/usecases/auth/dtos"
)

// StaffSignIn resolves the membership of a staff account and starts the
// session bound to it: the token carries the organization, the scope and the
// permission version of the oldest active membership of the client's scope.
type StaffSignIn struct {
	userRepo       repository.IUserRepository
	membershipRepo repository.IMembershipRepository
	sessions       *SessionIssuer
}

func NewStaffSignIn(
	userRepo repository.IUserRepository,
	membershipRepo repository.IMembershipRepository,
	sessions *SessionIssuer,
) *StaffSignIn {
	return &StaffSignIn{
		userRepo:       userRepo,
		membershipRepo: membershipRepo,
		sessions:       sessions,
	}
}

func (s *StaffSignIn) Begin(ctx context.Context, user *entities.User, client *entities.Client, ip, userAgent string) (*dtos.SessionOutput, error) {
	scope, ok := client.StaffScope()
	if !ok {
		return nil, ErrClientNotAllowed
	}
	ctx = tenant.WithUser(ctx, user.ID)
	memberships, err := s.membershipRepo.ListActiveForSignIn(ctx, user.ID, scope)
	if err != nil {
		return nil, err
	}
	if len(memberships) == 0 {
		return nil, ErrInvalidCredentials
	}
	// The list is ordered by age, so a person in several organizations of this
	// scope always enters the oldest one. The token then carries the choice.
	membership := memberships[0]
	ctx = tenant.WithOrganization(ctx, membership.OrganizationID)
	access, err := s.membershipRepo.Access(ctx, membership.ID)
	if err != nil {
		return nil, err
	}
	if access == nil || !access.IsActive() {
		return nil, ErrInvalidCredentials
	}
	return s.sessions.Start(ctx, user, client, access, ip, userAgent)
}
