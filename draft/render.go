package draft

import (
	"fmt"
	"strings"

	"github.com/viggy28/tider/internal/types"
)

// RenderMarkdown turns a DraftBundle into reviewable markdown. The
// recommendation leads with full title + body so the user can decide
// in 30 seconds; alternatives are listed below in compressed form so a
// disagreement with the pick still has somewhere to go without scrolling
// through every variant's full text.
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
	fmt.Fprintf(sb, "## %s · %s · risk: %s\n\n", d.Provider, d.Model, d.Risk)

	if d.Error != "" {
		fmt.Fprintf(sb, "**Error:** %s\n\n", d.Error)
		return
	}
	if d.Risk == types.RiskRefuse {
		fmt.Fprintf(sb, "**Refused:** %s\n\n", d.RiskReason)
		return
	}

	renderPick(sb, d)
	renderMeta(sb, d)
	renderAlternatives(sb, d)

	if d.InputTokens+d.OutputTokens > 0 {
		fmt.Fprintf(sb, "_tokens: in=%d out=%d_\n", d.InputTokens, d.OutputTokens)
	}
}

func renderPick(sb *strings.Builder, d types.Draft) {
	a := findAngle(d.Angles, d.Recommendation.AngleID)
	t := findTitle(a, d.Recommendation.TitleID)
	body := findBody(a, d.Recommendation.BodyID)
	if a == nil || t == nil || body == nil {
		sb.WriteString("**Pick:** none (recommendation references missing ids)\n\n")
		return
	}
	fmt.Fprintf(sb, "### Pick: angle %d / title %s / body %s\n\n",
		d.Recommendation.AngleID, d.Recommendation.TitleID, d.Recommendation.BodyID)
	if d.Recommendation.Reasoning != "" {
		fmt.Fprintf(sb, "> %s\n\n", d.Recommendation.Reasoning)
	}
	fmt.Fprintf(sb, "**Title:** %s\n\n", t.Text)
	sb.WriteString("**Body:**\n\n")
	fmt.Fprintf(sb, "%s\n\n", strings.TrimSpace(body.Text))
}

func renderMeta(sb *strings.Builder, d types.Draft) {
	wrote := false
	if d.Flair.Suggested != "" {
		marker := "optional"
		if d.Flair.Required {
			marker = "required"
		}
		fmt.Fprintf(sb, "**Flair:** %s (%s)\n", d.Flair.Suggested, marker)
		wrote = true
	}
	if d.SuggestedWindow != "" {
		fmt.Fprintf(sb, "**Suggested window:** %s\n", d.SuggestedWindow)
		wrote = true
	}
	if d.MediaRecommendation != "" {
		fmt.Fprintf(sb, "**Media:** %s\n", d.MediaRecommendation)
		wrote = true
	}
	if wrote {
		sb.WriteString("\n")
	}
}

func renderAlternatives(sb *strings.Builder, d types.Draft) {
	if len(d.Angles) == 0 {
		return
	}
	sb.WriteString("### Alternatives\n\n")
	for _, a := range d.Angles {
		isRecAngle := a.ID == d.Recommendation.AngleID

		header := fmt.Sprintf("**Angle %d** — %s", a.ID, a.Premise)
		if isRecAngle {
			header += " *(recommended angle)*"
		}
		fmt.Fprintf(sb, "%s\n", header)
		if a.Hook != "" {
			fmt.Fprintf(sb, "*Hook:* %s\n", a.Hook)
		}
		sb.WriteString("\n")

		if len(a.Titles) > 0 {
			sb.WriteString("Titles:\n")
			for _, t := range a.Titles {
				marker := ""
				if isRecAngle && t.ID == d.Recommendation.TitleID {
					marker = " ← picked"
				}
				fmt.Fprintf(sb, "- `%s` %s%s\n", t.ID, t.Text, marker)
			}
			sb.WriteString("\n")
		}

		// Skip the picked body (already shown in full above); list the rest as
		// metadata only so the user can scan and request a regen if needed.
		var remaining []types.Body
		for _, body := range a.Bodies {
			if isRecAngle && body.ID == d.Recommendation.BodyID {
				continue
			}
			remaining = append(remaining, body)
		}
		if len(remaining) > 0 {
			label := "Bodies:"
			if isRecAngle {
				label = "Other bodies:"
			}
			sb.WriteString(label + "\n")
			for _, body := range remaining {
				tagSuffix := ""
				if len(body.Tags) > 0 {
					tagSuffix = " — _" + strings.Join(body.Tags, ", ") + "_"
				}
				fmt.Fprintf(sb, "- `%s` (~%d words)%s\n", body.ID, wordCount(body.Text), tagSuffix)
			}
			sb.WriteString("\n")
		}
	}
}

func findAngle(angles []types.Angle, id int) *types.Angle {
	for i := range angles {
		if angles[i].ID == id {
			return &angles[i]
		}
	}
	return nil
}

func findTitle(a *types.Angle, id string) *types.Title {
	if a == nil {
		return nil
	}
	for i := range a.Titles {
		if a.Titles[i].ID == id {
			return &a.Titles[i]
		}
	}
	return nil
}

func findBody(a *types.Angle, id string) *types.Body {
	if a == nil {
		return nil
	}
	for i := range a.Bodies {
		if a.Bodies[i].ID == id {
			return &a.Bodies[i]
		}
	}
	return nil
}

func wordCount(s string) int { return len(strings.Fields(s)) }
