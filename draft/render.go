package draft

import (
	"fmt"
	"strings"

	"github.com/viggy28/tider/internal/types"
)

// RenderMarkdown turns a DraftBundle into reviewable markdown, organized
// to make per-provider comparison easy: same sections in the same order
// for every provider, recommendation called out at the top of each.
func RenderMarkdown(b *types.DraftBundle) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Drafts for r/%s\n\n", b.Sub)
	fmt.Fprintf(&sb, "**Brief:** %s\n\n", b.Brief.Title)
	if b.Brief.Summary != "" {
		fmt.Fprintf(&sb, "%s\n\n", b.Brief.Summary)
	}
	fmt.Fprintf(&sb, "Generated: %s · %d provider(s)\n\n", b.Generated.Format("2006-01-02 15:04 UTC"), len(b.Drafts))
	sb.WriteString("---\n\n")

	for _, d := range b.Drafts {
		renderDraft(&sb, d)
		sb.WriteString("\n---\n\n")
	}
	return sb.String()
}

func renderDraft(sb *strings.Builder, d types.Draft) {
	fmt.Fprintf(sb, "## %s · %s\n\n", d.Provider, d.Model)

	if d.Error != "" {
		fmt.Fprintf(sb, "**Error:** %s\n\n", d.Error)
		return
	}

	fmt.Fprintf(sb, "**Risk:** %s", d.Risk)
	if d.RiskReason != "" {
		fmt.Fprintf(sb, " — %s", d.RiskReason)
	}
	sb.WriteString("\n\n")

	if d.Risk == types.RiskRefuse {
		// Nothing else to render — refuse means no angles by design.
		return
	}

	if d.Recommendation.AngleID != 0 || d.Recommendation.TitleID != "" {
		fmt.Fprintf(sb, "**Recommendation:** angle %d, title %s, body %s",
			d.Recommendation.AngleID, d.Recommendation.TitleID, d.Recommendation.BodyID)
		if d.Recommendation.Reasoning != "" {
			fmt.Fprintf(sb, " — %s", d.Recommendation.Reasoning)
		}
		sb.WriteString("\n\n")
	}

	if d.Flair.Suggested != "" {
		marker := "optional"
		if d.Flair.Required {
			marker = "required"
		}
		fmt.Fprintf(sb, "**Flair:** %s (%s)\n\n", d.Flair.Suggested, marker)
	}
	if d.SuggestedWindow != "" {
		fmt.Fprintf(sb, "**Suggested window:** %s\n\n", d.SuggestedWindow)
	}
	if d.MediaRecommendation != "" {
		fmt.Fprintf(sb, "**Media:** %s\n\n", d.MediaRecommendation)
	}

	for _, a := range d.Angles {
		fmt.Fprintf(sb, "### Angle %d: %s\n\n", a.ID, a.Premise)
		if a.Hook != "" {
			fmt.Fprintf(sb, "*Hook:* %s\n\n", a.Hook)
		}
		if len(a.Titles) > 0 {
			sb.WriteString("**Titles**\n\n")
			for _, t := range a.Titles {
				fmt.Fprintf(sb, "- `%s` %s\n", t.ID, t.Text)
			}
			sb.WriteString("\n")
		}
		if len(a.Bodies) > 0 {
			sb.WriteString("**Bodies**\n\n")
			for _, body := range a.Bodies {
				tagSuffix := ""
				if len(body.Tags) > 0 {
					tagSuffix = " — _" + strings.Join(body.Tags, ", ") + "_"
				}
				fmt.Fprintf(sb, "**`%s`**%s\n\n", body.ID, tagSuffix)
				fmt.Fprintf(sb, "%s\n\n", body.Text)
			}
		}
	}

	if d.InputTokens+d.OutputTokens > 0 {
		fmt.Fprintf(sb, "_tokens: in=%d out=%d_\n", d.InputTokens, d.OutputTokens)
	}
}
