package assets

import "embed"

// FS contains all static assets served by the HTTP server.
//
//go:embed css js
var FS embed.FS
