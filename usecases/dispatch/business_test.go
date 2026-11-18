package dispatch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/domain/services"
)

type capturedBusiness struct {
	services.NoBusinessEvents
	transitions []services.RequestTransition
}

func (c *capturedBusiness) RequestTransitioned(_ context.Context, e services.RequestTransition) {
	c.transitions = append(c.transitions, e)
}

func TestAcceptReportsTheTransition(t *testing.T) {
	w := newWorld(t, taxi())
	business := &capturedBusiness{}
	w.usecase.Business = business
	provider := w.addLiveProvider(0.001, nil)
	w.advance()
	offer := w.onlyOffer()
	business.transitions = nil

	require.NoError(t, acceptIgnoringView(w, provider, offer.ID, w.request.Version))

	require.Len(t, business.transitions, 1)
	got := business.transitions[0]
	assert.Equal(t, w.request.ID, got.RequestID)
	assert.Equal(t, "taxi", got.Category)
	assert.Equal(t, "offered", got.From)
	assert.Equal(t, "accepted", got.To)
	assert.Equal(t, "provider", got.ActorType)
	assert.False(t, got.Scheduled)
	assert.Equal(t, w.request.CreatedAt, got.CreatedAt)
}

func TestRejectedAcceptReportsNothing(t *testing.T) {
	w := newWorld(t, taxi())
	business := &capturedBusiness{}
	w.usecase.Business = business
	provider := w.addLiveProvider(0.001, nil)
	w.advance()
	offer := w.onlyOffer()
	business.transitions = nil

	require.ErrorIs(t, acceptIgnoringView(w, provider, offer.ID, w.request.Version+5), ErrRequestConflict)
	assert.Empty(t, business.transitions)
}
