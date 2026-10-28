package apidocs

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	swagger "github.com/Agora-Mobility/atlas/docs/swagger"
)

const (
	scalarVersion   = "1.73.1"
	scalarScriptURL = "https://cdn.jsdelivr.net/npm/@scalar/api-reference@" + scalarVersion + "/dist/browser/standalone.js"
	scalarIntegrity = "sha384-kYDGzV91Jnn3TbHINV3nt54riK2uMJDfN5Al8dAkz4FssELTBWbD8rgw32sTKfOi"

	contentSecurityPolicy = "default-src 'none'; " +
		"script-src 'self' https://cdn.jsdelivr.net; " +
		"style-src 'self' 'unsafe-inline'; " +
		"img-src 'self' data:; " +
		"font-src 'self' data:; " +
		"connect-src 'self'; " +
		"worker-src 'self' blob:; " +
		"base-uri 'none'; form-action 'none'; frame-ancestors 'none'"
)

var (
	//go:embed page.html
	pageTemplate string

	//go:embed scalar.js
	scalarConfig []byte
)

type pageData struct {
	ScriptURL string
	Integrity string
}

func Register(r gin.IRoutes) {
	page := renderPage()
	r.GET("/docs", func(ctx *gin.Context) {
		ctx.Header("Content-Security-Policy", contentSecurityPolicy)
		ctx.Data(http.StatusOK, "text/html; charset=utf-8", page)
	})
	r.GET("/docs/scalar.js", func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "text/javascript; charset=utf-8", scalarConfig)
	})
	r.GET("/docs/openapi.json", func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "application/json; charset=utf-8", []byte(swagger.SwaggerInfo.ReadDoc()))
	})
}

func renderPage() []byte {
	var page bytes.Buffer
	tmpl := template.Must(template.New("docs").Parse(pageTemplate))
	if err := tmpl.Execute(&page, pageData{ScriptURL: scalarScriptURL, Integrity: scalarIntegrity}); err != nil {
		panic("render api docs page: " + err.Error())
	}
	return page.Bytes()
}
