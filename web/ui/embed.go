package ui

import "embed"

// Assets holds the built SPA under dist/.
//
//go:embed all:dist
var Assets embed.FS
