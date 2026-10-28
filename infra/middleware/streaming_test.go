package middleware_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Agora-Mobility/atlas/infra/middleware"
)

const writeTimeout = 100 * time.Millisecond

func streamEvents(ctx *gin.Context) {
	ctx.Header("Content-Type", "text/event-stream")
	for i := range 3 {
		if i > 0 {
			time.Sleep(writeTimeout)
		}
		_, _ = ctx.Writer.WriteString("data: ping\n\n")
		ctx.Writer.Flush()
	}
}

func startServer(t *testing.T) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/stream", middleware.Streaming(), streamEvents)
	r.GET("/regular", streamEvents)

	server := httptest.NewUnstartedServer(r)
	server.Config.WriteTimeout = writeTimeout
	server.Start()
	t.Cleanup(server.Close)
	return server.URL
}

func countEvents(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	events := 0
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if scanner.Text() == "data: ping" {
			events++
		}
	}
	return events
}

func TestStreamingRoutesOutliveTheWriteTimeout(t *testing.T) {
	url := startServer(t)

	assert.Equal(t, 3, countEvents(t, url+"/stream"))
	assert.Less(t, countEvents(t, url+"/regular"), 3)
}
