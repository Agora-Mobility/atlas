package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/shared/customerrors"
)

// RateLimiter is a fixed-window limiter per client IP backed by Redis, so the
// limit holds across API instances. If Redis fails the request is allowed:
// availability wins over strict limiting.
func RateLimiter(client *goredis.Client, limit int, window time.Duration, skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := skip[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		ctx := c.Request.Context()
		now := time.Now()
		windowStart := now.Truncate(window)
		key := fmt.Sprintf("atlas:ratelimit:%s:%d", c.ClientIP(), windowStart.Unix())

		count, err := incrWindow(ctx, client, key, window)
		if err != nil {
			logger.FromContext(ctx).Warn("rate limiter unavailable, allowing request", zap.Error(err))
			c.Next()
			return
		}

		remaining := max(limit-int(count), 0)
		c.Header("X-RateLimit-Limit", strconv.Itoa(limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if int(count) > limit {
			retryAfter := int(windowStart.Add(window).Sub(now).Seconds()) + 1
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			appErr := customerrors.New(customerrors.CodeTooManyRequests, "too many requests", nil)
			c.AbortWithStatusJSON(http.StatusTooManyRequests,
				customerrors.NewResponse(appErr, logger.CorrelationID(ctx)))
			return
		}
		c.Next()
	}
}

func incrWindow(ctx context.Context, client *goredis.Client, key string, window time.Duration) (int64, error) {
	pipe := client.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window+time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return incr.Val(), nil
}
