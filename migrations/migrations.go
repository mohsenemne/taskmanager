// Package migrations embeds the SQL schema migrations so the binary can apply
// them without shipping separate files.
package migrations

import "embed"

// FS holds the migration files. golang-migrate applies them in version order.
//
//go:embed *.sql
var FS embed.FS
