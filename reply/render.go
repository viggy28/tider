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
	// Inspection-depth header — populated only in review mode so the
	// reader can see what was inspected before reading the draft. Per
	// SPEC_REVIEW_VISUAL_FIRECRAWL.md "Output rendering".
	if b.Inspection != nil {
		fmt.Fprintf(&sb, "Inspection: %s\n", inspectionDescription(b.Inspection))
		if b.Inspection.ScreenshotPath != "" {
			sb.WriteString("Screenshot: saved\n")
		}
		fmt.Fprintf(&sb, "Images analyzed: %d\n", b.Inspection.ImagesAnalyzed)
		if b.Inspection.ShopType != "" {
			fmt.Fprintf(&sb, "Shop type: %s\n", b.Inspection.ShopType)
		}
		if len(b.Inspection.Limitations) > 0 {
			fmt.Fprintf(&sb, "Limitations: %s\n", strings.Join(b.Inspection.Limitations, "; "))
		}
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

// inspectionDescription turns an InspectionSummary into a one-line
// description for the rendered header. "Firecrawl visual" when the
// firecrawl backend ran with a screenshot; "Firecrawl (no screenshot)"
// is a defensive case but should not occur in practice — the review-
// mode invariant requires a screenshot. Fallback to Source verbatim
// for unknown values so future backends don't render as empty.
func inspectionDescription(s *types.InspectionSummary) string {
	switch s.Source {
	case "firecrawl":
		if s.ScreenshotPath != "" {
			return "Firecrawl visual"
		}
		return "Firecrawl (no screenshot)"
	case "":
		return "unknown"
	default:
		return s.Source
	}
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

// displayLabel maps known variant ids to their spec-mandated display
// form. SPEC_REPLY_REFINEMENT.md "Output rendering" defines the reply-
// mode forms; SPEC_REVIEW_VISUAL_FIRECRAWL.md adds review-mode-specific
// variants. Hyphen retention is per-label and intentional: compound
// modifiers like "thread-aware" / "structured-review" keep the hyphen;
// noun phrases like "personal-story" / "question-first" use spaces.
var displayLabel = map[string]string{
	"best":              "Best",
	"short":             "Short",
	"thread-aware":      "Thread-Aware",
	"personal-story":    "Personal Story",
	"question-first":    "Question First",
	"detailed":          "Detailed",
	"structured-review": "Structured-Review",
}

// titleCaseLabel returns the display form of a draft label. Known labels
// from the spec use the explicit map above; unknown labels fall back to
// kebab-to-title-case-with-hyphens so future variant names render
// reasonably without a code change.
func titleCaseLabel(s string) string {
	if s == "" {
		return ""
	}
	if d, ok := displayLabel[s]; ok {
		return d
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
