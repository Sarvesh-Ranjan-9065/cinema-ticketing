// Package migrations embeds versioned SQL migration scripts for database schema evolution.
package migrations

import "embed"

// FS embeds all migration SQL files.
//
//go:embed *.sql
var FS embed.FS
