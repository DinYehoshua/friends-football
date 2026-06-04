// Package frontend provides embedded static files for the web UI.
package frontend

import "embed"

// StaticFiles contains the embedded static directory.
//
//go:embed all:static
var StaticFiles embed.FS
