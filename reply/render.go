package reply

import (
	"fmt"
	"strings"

	"github.com/viggy28/tider/internal/types"
)

// RenderMarkdown turns a ReplyBundle into a scannable human report,
// matching the shape in SPEC_REPLY.md. Pick leads with full content;
// other variants follow under "Alternatives". TTY-aware ANSI styling is
// applied at the CLI layer (cmd/tider/term.go), not here.
//
// threadTitle is rendered in the header for context — the bundle alone
// doesn't carry it. sessionPath is the absolute on-disk session
// directory; passing "" omits the line.
func RenderMarkdown(b *types.ReplyBundle, threadTitle string, sessionPath string) string {
	if b == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Reply drafts for r/%s\n\n", b.Subreddit)
	if threadTitle != "" {
		fmt.Fprintf(&sb, "Thread: %s\n", threadTitle)
	}
	fmt.Fprintf(&sb, "Mode: %s\n", b.Mode)
	if sessionPath != "" {
		fmt.Fprintf(&sb, "Session: %s\n", sessionPath)
	}
	sb.WriteString("\n")

	pick := findDraft(b.Drafts, b.PickID)
	if pick != nil {
		sb.WriteString("## Best Pick\n\n")
		if pick.Reasoning != "" {
			fmt.Fprintf(&sb, "> %s\n\n", pick.Reasoning)
		}
		fmt.Fprintf(&sb, "%s\n\n", strings.TrimSpace(pick.Text))
	}

	others := alternatives(b.Drafts, b.PickID)
	if len(others) > 0 {
		sb.WriteString("## Alternatives\n\n")
		for _, d := range others {
			fmt.Fprintf(&sb, "### %s\n\n", titleCaseLabel(d.Label))
			if d.Reasoning != "" {
				fmt.Fprintf(&sb, "*%s*\n\n", d.Reasoning)
			}
			fmt.Fprintf(&sb, "%s\n\n", strings.TrimSpace(d.Text))
		}
	}

	return sb.String()
}

func findDraft(drafts []types.ReplyDraft, id string) *types.ReplyDraft {
	for i := range drafts {
		if drafts[i].ID == id {
			return &drafts[i]
		}
	}
	return nil
}

func alternatives(drafts []types.ReplyDraft, pickID string) []types.ReplyDraft {
	var out []types.ReplyDraft
	for _, d := range drafts {
		if d.ID != pickID {
			out = append(out, d)
		}
	}
	return out
}

// titleCaseLabel turns "question-first" → "Question-First", "short" →
// "Short", etc. Cobra-style title casing for short tokens. Avoids
// strings.Title (deprecated) and the cases package dep.
func titleCaseLabel(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "-")
}
