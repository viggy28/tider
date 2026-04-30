package research

import (
	"fmt"
	"strings"

	"github.com/viggy28/tider/internal/types"
)

// RenderMarkdown turns ResearchInsights into a concise human report. It avoids
// raw post bodies by design; the raw bundle is available through JSON output.
func RenderMarkdown(i *types.ResearchInsights) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# r/%s Research\n\n", i.Subreddit)

	if i.Takeaway != "" {
		sb.WriteString("## Takeaway\n\n")
		fmt.Fprintf(&sb, "%s\n\n", i.Takeaway)
	}

	if len(i.PainPoints) > 0 {
		sb.WriteString("## Strongest Pain Points\n\n")
		for n, p := range i.PainPoints {
			confidence := p.Confidence
			if confidence == "" {
				confidence = "unknown"
			}
			fmt.Fprintf(&sb, "%d. **%s** (%s confidence)\n", n+1, p.Name, confidence)
			if p.Summary != "" {
				fmt.Fprintf(&sb, "   %s\n", p.Summary)
			}
			sb.WriteString("\n")
		}
	}

	if len(i.SpecificFriction) > 0 {
		sb.WriteString("## Specific Friction Seen\n\n")
		for _, f := range i.SpecificFriction {
			confidence := f.Confidence
			if confidence == "" {
				confidence = "unknown"
			}
			fmt.Fprintf(&sb, "- **%s** (%s confidence)", f.Name, confidence)
			if f.Summary != "" {
				fmt.Fprintf(&sb, ": %s", f.Summary)
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	if len(i.RepeatedAsks) > 0 {
		sb.WriteString("## Repeated Asks\n\n")
		for _, ask := range i.RepeatedAsks {
			fmt.Fprintf(&sb, "- %s\n", ask)
		}
		sb.WriteString("\n")
	}

	if len(i.Opportunity) > 0 {
		sb.WriteString("## Opportunity Areas\n\n")
		for _, note := range i.Opportunity {
			fmt.Fprintf(&sb, "- %s\n", note)
		}
		sb.WriteString("\n")
	}

	if len(i.Language) > 0 {
		sb.WriteString("## Language\n\n")
		fmt.Fprintf(&sb, "%s\n\n", strings.Join(i.Language, ", "))
	}

	if len(i.Evidence) > 0 {
		sb.WriteString("## Evidence Posts\n\n")
		for _, e := range i.Evidence {
			fmt.Fprintf(&sb, "- %s\n", formatEvidence(e))
		}
		sb.WriteString("\n")
	}

	if len(i.Limitations) > 0 {
		sb.WriteString("## Limitations\n\n")
		for _, l := range i.Limitations {
			fmt.Fprintf(&sb, "- %s\n", l)
		}
		sb.WriteString("\n")
	}

	if i.InputTokens+i.OutputTokens > 0 {
		fmt.Fprintf(&sb, "_tokens: in=%d out=%d_\n", i.InputTokens, i.OutputTokens)
	}
	return sb.String()
}

func formatEvidence(e types.ResearchEvidence) string {
	title := strings.TrimSpace(e.Title)
	if title == "" {
		title = "(untitled)"
	}
	stats := fmt.Sprintf("%d pts, %d comments", e.Score, e.Comments)
	if e.Source != "" {
		stats += ", " + e.Source
	}
	if e.Permalink != "" {
		return fmt.Sprintf("%q - %s - %s", title, stats, e.Permalink)
	}
	return fmt.Sprintf("%q - %s", title, stats)
}
