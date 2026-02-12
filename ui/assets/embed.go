package assets

import "embed"

// FS contains all static assets served by the HTTP server.
//
//go:embed css js img
var FS embed.FS
