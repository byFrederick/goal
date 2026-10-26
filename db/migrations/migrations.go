// Package migrations embeds the forward-only SQL migrations that internal/adapters/store/postgres
// applies with goose.
package migrations

import "embed"

// FS holds every migration file.
//
//go:embed *.sql
var FS embed.FS
