package middleware

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Agora-Mobility/atlas/domain/entities"
	"github.com/Agora-Mobility/atlas/domain/repository"
	"github.com/Agora-Mobility/atlas/shared"
)

const (
	HeaderClientID     = "X-Client-Id"
	maxClientIDLength  = 64
	defaultClientCache = 5 * time.Minute
)

type cachedClient struct {
	client    *entities.Client
	expiresAt time.Time
}

type ClientAuthorizer struct {
	clients  repository.IClientRepository
	cacheTTL time.Duration
	mu       sync.RWMutex
	cache    map[string]cachedClient
}

func NewClientAuthorizer(clients repository.IClientRepository) *ClientAuthorizer {
	return &ClientAuthorizer{
		clients:  clients,
		cacheTTL: defaultClientCache,
		cache:    map[string]cachedClient{},
	}
}

func (a *ClientAuthorizer) RequireClient() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		clientID := strings.TrimSpace(ctx.GetHeader(HeaderClientID))
		if clientID == "" || len(clientID) > maxClientIDLength {
			shared.BadRequest(ctx, "MISSING_CLIENT", "The X-Client-Id header is required.")
			return
		}

		client, err := a.resolve(ctx.Request.Context(), clientID)
		if err != nil {
			shared.InternalServerError(ctx, shared.CodeInternalError, "An internal error occurred.", err)
			return
		}
		if client == nil {
			shared.Unauthorized(ctx, "UNKNOWN_CLIENT", "Missing or unknown client.")
			return
		}

		ctx.Request = ctx.Request.WithContext(WithClient(ctx.Request.Context(), client))
		ctx.Next()
	}
}

func (a *ClientAuthorizer) resolve(ctx context.Context, clientID string) (*entities.Client, error) {
	if client, ok := a.cached(clientID); ok {
		return client, nil
	}

	client, err := a.clients.FindActiveByClientID(ctx, clientID)
	if err != nil || client == nil {
		return nil, err
	}

	a.mu.Lock()
	a.cache[clientID] = cachedClient{client: client, expiresAt: time.Now().Add(a.cacheTTL)}
	a.mu.Unlock()
	return client, nil
}

func (a *ClientAuthorizer) cached(clientID string) (*entities.Client, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	entry, ok := a.cache[clientID]
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.client, true
}
