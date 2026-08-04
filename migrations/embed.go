package migrations

import "embed"

// Files contains all versioned PostgreSQL migrations.
//
//go:embed *.sql
var Files embed.FS
