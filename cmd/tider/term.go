package main

import (
	"os"
	"regexp"
	"strings"
)

// Tiny ANSI renderer for the specific markdown subset that
// draft.RenderMarkdown emits. Not a general renderer — handles only the
// constructs we actually produce. Trade-off: zero deps, full control,
// behavior is obvious. Anything more elaborate (tables, fenced code,
// nested lists) will need a real library.

const (
	ansiReset     = "\x1b[0m"
	ansiBold      = "\x1b[1m"
	ansiDim       = "\x1b[2m"
	ansiItalic    = "\x1b[3m"
	ansiCyan      = "\x1b[36m"
	ansiGreen     = "\x1b[32m"
	ansiMagenta   = "\x1b[35m"
)

var (
	// Order of regex application matters in applyInline (bold before
	// italic so ** doesn't get eaten as two single *).
	reBold        = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reCode        = regexp.MustCompile("`([^`]+)`")
	reItalicUnder = regexp.MustCompile(`(^|[\s(])_([^_]+)_($|[\s).,;:!?])`)
	reItalicStar  = regexp.MustCompile(`(^|[\s(])\*([^*]+)\*($|[\s).,;:!?])`)
)

// renderTerminal converts the markdown produced by draft.RenderMarkdown
// into ANSI-styled text suitable for a terminal.
func renderTerminal(md string) string {
	var out strings.Builder
	for line := range strings.SplitSeq(md, "\n") {
		out.WriteString(renderLine(line))
		out.WriteByte('\n')
	}
	return out.String()
}

func renderLine(line string) string {
	switch {
	case line == "---":
		return ansiDim + strings.Repeat("─", 60) + ansiReset
	case strings.HasPrefix(line, "### "):
		return ansiBold + ansiCyan + strings.TrimPrefix(line, "### ") + ansiReset
	case strings.HasPrefix(line, "## "):
		return ansiBold + ansiGreen + strings.TrimPrefix(line, "## ") + ansiReset
	case strings.HasPrefix(line, "# "):
		return ansiBold + ansiMagenta + strings.TrimPrefix(line, "# ") + ansiReset
	case strings.HasPrefix(line, "> "):
		return ansiDim + "│ " + applyInline(strings.TrimPrefix(line, "> ")) + ansiReset
	case strings.HasPrefix(line, "- "):
		return "  • " + applyInline(strings.TrimPrefix(line, "- "))
	default:
		return applyInline(line)
	}
}

func applyInline(s string) string {
	s = reBold.ReplaceAllString(s, ansiBold+"$1"+ansiReset)
	s = reCode.ReplaceAllString(s, ansiCyan+"$1"+ansiReset)
	s = reItalicUnder.ReplaceAllString(s, "$1"+ansiItalic+"$2"+ansiReset+"$3")
	s = reItalicStar.ReplaceAllString(s, "$1"+ansiItalic+"$2"+ansiReset+"$3")
	return s
}

// isTerminal reports whether f is connected to an interactive terminal.
// Used to decide whether to ANSI-render markdown output: TTY → rendered,
// pipe/redirect → raw markdown so downstream tooling sees clean text.
// NO_COLOR (https://no-color.org) forces raw even in a TTY.
func isTerminal(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
