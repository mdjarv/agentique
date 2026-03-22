package server

import "embed"

//go:embed all:frontend_dist
var frontendFS embed.FS
