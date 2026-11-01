// Package web embeds the built Vue admin UI.
package web

import "embed"

// Dist holds the output of `npm --prefix web run build`.
//
//go:embed all:dist
var Dist embed.FS
