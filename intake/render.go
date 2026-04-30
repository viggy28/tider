package intake

import (
	"fmt"
	"strings"

	"github.com/viggy28/tider/internal/types"
)

// RenderMarkdown turns a Brief into a scannable human report. raw_content
// is intentionally omitted — it's verbose and meant for machine consumption
// (it round-trips through brief.json into draft). For the markdown view,
// we want what a human cares about: title, summary, audience, the things
// the LLM actually surfaced as Brief-shape signal.
func RenderMarkdown(b *types.Brief) string {
	if b == nil {
		return ""
	}
	var sb strings.Builder

	title := b.Title
	if title == "" {
		title = "(untitled brief)"
	}
	fmt.Fprintf(&sb, "# %s\n\n", title)

	if b.Summary != "" {
		fmt.Fprintf(&sb, "%s\n\n", b.Summary)
	}
	if b.Audience != "" {
		fmt.Fprintf(&sb, "**Audience:** %s\n\n", b.Audience)
	}

	if len(b.Highlights) > 0 {
		sb.WriteString("## Highlights\n\n")
		for _, h := range b.Highlights {
			fmt.Fprintf(&sb, "- %s\n", h)
		}
		sb.WriteString("\n")
	}

	if len(b.Links) > 0 {
		sb.WriteString("## Links\n\n")
		for _, l := range b.Links {
			fmt.Fprintf(&sb, "- %s\n", l)
		}
		sb.WriteString("\n")
	}

	if b.Source.Mode != "" || b.Source.Value != "" {
		sb.WriteString("---\n\n")
		if b.Source.Value != "" {
			fmt.Fprintf(&sb, "*Source: %s — %s*\n", b.Source.Mode, b.Source.Value)
		} else {
			fmt.Fprintf(&sb, "*Source: %s*\n", b.Source.Mode)
		}
	}

	return sb.String()
}
