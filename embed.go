// Package seds embeds the tuned configuration profiles into the server binary,
// so a missing or mis-copied config/ directory can never break a deploy.
package seds

import "embed"

// ConfigFS holds config/*.json.
//
//go:embed config/*.json
var ConfigFS embed.FS
