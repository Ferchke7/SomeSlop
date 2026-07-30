package migrations

import "embed"

// Files contains the versioned SQL migrations used both by Goose and the
// self-contained development server.
//
//go:embed *.sql
var Files embed.FS
