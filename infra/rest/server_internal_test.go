package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/Agora-Mobility/atlas/config"
	"github.com/Agora-Mobility/atlas/shared/enums"
)

func TestAPIDocsOnlyInDevelopment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := map[enums.Environment]int{
		enums.EnvDevelopment: http.StatusOK,
		enums.EnvStaging:     http.StatusNotFound,
		enums.EnvTest:        http.StatusNotFound,
		enums.EnvProduction:  http.StatusNotFound,
	}
	for env, want := range cases {
		t.Run(env.String(), func(t *testing.T) {
			r := gin.New()
			registerAPIDocs(r, &config.Config{App: config.AppConfig{Env: env}})

			for _, path := range []string{"/docs", "/docs/openapi.json", "/docs/scalar.js"} {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))
				assert.Equal(t, want, rec.Code, path)
			}
		})
	}
}
