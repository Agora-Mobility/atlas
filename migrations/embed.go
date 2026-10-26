// Package migrations embeds the SQL migrations into the binary so the
// distroless image can run `atlas migrate` without files on disk.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
