// Package prompts holds versioned prompt templates as embedded files. The
// templates themselves are part of tider's IP and are intended to be
// iterated on — diffing them against generated draft quality is the point.
package prompts

import "embed"

//go:embed *.tmpl
var FS embed.FS
