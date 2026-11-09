//go:build desktop

package main

import "embed"

// frontend_dist is a copy of frontend/dist made by `make app`.
//
//go:embed all:frontend_dist
var assets embed.FS
