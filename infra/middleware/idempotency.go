package middleware

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	goredis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/Agora-Mobility/atlas/config/logger"
	"github.com/Agora-Mobility/atlas/shared"
)

const (
	HeaderIdempotencyKey = "Idempotency-Key"
	maxIdempotencyKey    = 128
	maxReplayBodyBytes   = 256 << 10
	inProgressTTL        = 30 * time.Second
	statePending         = "pending"
	stateDone            = "done"
)

type idempotentRecord struct {
	State       string `json:"state"`
	Fingerprint string `json:"fingerprint"`
	Status      int    `json:"status,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

type Idempotency struct {
	rdb *goredis.Client
	ttl time.Duration
}

func NewIdempotency(rdb *goredis.Client, ttl time.Duration) *Idempotency {
	return &Idempotency{rdb: rdb, ttl: ttl}
}

type capturingWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *capturingWriter) Write(data []byte) (int, error) {
	if w.body.Len()+len(data) <= maxReplayBodyBytes {
		w.body.Write(data)
	}
	return w.ResponseWriter.Write(data)
}

func (i *Idempotency) Optional() gin.HandlerFunc {
	return i.handler(false)
}

func (i *Idempotency) Required() gin.HandlerFunc {
	return i.handler(true)
}

func (i *Idempotency) handler(required bool) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		key := ctx.GetHeader(HeaderIdempotencyKey)
		if key == "" {
			if required {
				shared.BadRequest(ctx, "IDEMPOTENCY_KEY_REQUIRED", "The Idempotency-Key header is required.")
				return
			}
			ctx.Next()
			return
		}
		if len(key) > maxIdempotencyKey {
			shared.BadRequest(ctx, "INVALID_IDEMPOTENCY_KEY", "The Idempotency-Key header is too long.")
			return
		}
		i.run(ctx, key)
	}
}

func (i *Idempotency) run(ctx *gin.Context, key string) {
	reqCtx := ctx.Request.Context()
	body, err := io.ReadAll(ctx.Request.Body)
	if err != nil {
		shared.BadRequest(ctx, shared.CodeInvalidRequest, "Invalid request body.")
		return
	}
	ctx.Request.Body = io.NopCloser(bytes.NewReader(body))

	userID, _ := UserIDFromContext(reqCtx)
	storageKey := fmt.Sprintf("idem:%s:%s:%s", userID, ctx.FullPath(), key)
	fingerprint := fingerprintOf(ctx.Request.URL.Path, body)

	claimed, err := i.claim(reqCtx, storageKey, fingerprint)
	if err != nil {
		logger.FromContext(reqCtx).Warn("idempotency store unavailable, running without replay", zap.Error(err))
		ctx.Next()
		return
	}
	if !claimed {
		i.replay(ctx, storageKey, fingerprint)
		return
	}

	writer := &capturingWriter{ResponseWriter: ctx.Writer}
	ctx.Writer = writer
	ctx.Next()
	i.finish(context.WithoutCancel(reqCtx), storageKey, fingerprint, writer)
}

func (i *Idempotency) claim(ctx context.Context, key, fingerprint string) (bool, error) {
	pending, err := json.Marshal(idempotentRecord{State: statePending, Fingerprint: fingerprint})
	if err != nil {
		return false, err
	}
	err = i.rdb.SetArgs(ctx, key, pending, goredis.SetArgs{Mode: "NX", TTL: inProgressTTL}).Err()
	if errors.Is(err, goredis.Nil) {
		return false, nil
	}
	return err == nil, err
}

func (i *Idempotency) finish(ctx context.Context, key, fingerprint string, writer *capturingWriter) {
	status := writer.Status()
	if status >= http.StatusInternalServerError || writer.body.Len() >= maxReplayBodyBytes {
		if err := i.rdb.Del(ctx, key).Err(); err != nil {
			logger.FromContext(ctx).Warn("idempotency claim not released", zap.Error(err))
		}
		return
	}
	record, err := json.Marshal(idempotentRecord{
		State: stateDone, Fingerprint: fingerprint, Status: status, ContentType: writer.Header().Get("Content-Type"), Body: writer.body.Bytes(),
	})
	if err == nil {
		err = i.rdb.Set(ctx, key, record, i.ttl).Err()
	}
	if err != nil {
		logger.FromContext(ctx).Warn("idempotent response not stored", zap.Error(err))
	}
}

func (i *Idempotency) replay(ctx *gin.Context, key, fingerprint string) {
	raw, err := i.rdb.Get(ctx.Request.Context(), key).Bytes()
	if err != nil {
		shared.Conflict(ctx, "IDEMPOTENCY_IN_PROGRESS", "This action is still being processed. Try again in a moment.")
		return
	}
	var record idempotentRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		shared.Conflict(ctx, "IDEMPOTENCY_IN_PROGRESS", "This action is still being processed. Try again in a moment.")
		return
	}
	if record.Fingerprint != fingerprint {
		shared.RespondError(ctx, http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED", "This Idempotency-Key was used with a different request.")
		return
	}
	if record.State != stateDone {
		shared.Conflict(ctx, "IDEMPOTENCY_IN_PROGRESS", "This action is still being processed. Try again in a moment.")
		return
	}
	ctx.Header("Idempotent-Replayed", "true")
	ctx.Data(record.Status, record.ContentType, record.Body)
	ctx.Abort()
}

func fingerprintOf(path string, body []byte) string {
	hash := sha256.New()
	hash.Write([]byte(path))
	hash.Write(body)
	return hex.EncodeToString(hash.Sum(nil))
}
