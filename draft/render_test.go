package draft

import (
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func sampleBundle() *types.DraftBundle {
	return &types.DraftBundle{
		Sub:       "golang",
		Brief:     types.Brief{Title: "Streambed", Summary: "WAL-native CDC."},
		Generated: time.Date(2026, 4, 27, 10, 0, 0, 0, time.UTC),
		Drafts: []types.Draft{
			{
				Sub:      "golang",
				Provider: "anthropic",
				Model:    "claude-sonnet-4-7",
				Risk:     "low",
				Angles: []types.Angle{
					{
						ID: 1, Premise: "war story", Hook: "what surprised me",
						Titles: []types.Title{
							{ID: "1.1", Text: "Title A1"},
							{ID: "1.2", Text: "Title A2"},
						},
						Bodies: []types.Body{
							{ID: "1.1", Text: "Body A1 content one two three.", Tags: []string{"opener:war-story"}},
							{ID: "1.2", Text: "Body A2 content here with a few more words for counting.", Tags: []string{"opener:question"}},
						},
					},
					{
						ID: 2, Premise: "technical pitch", Hook: "what we got",
						Titles: []types.Title{
							{ID: "2.1", Text: "Title B1"},
						},
						Bodies: []types.Body{
							{ID: "2.1", Text: "Body B1 content with some words.", Tags: []string{"opener:context"}},
						},
					},
				},
				Recommendation: types.Recommendation{
					AngleID:   1,
					TitleID:   "1.2",
					BodyID:    "1.1",
					Reasoning: "fits the sub better than a pitch",
				},
				Flair:           types.FlairRec{Required: true, Suggested: "discussion"},
				SuggestedWindow: "Tue-Thu 9-11am ET",
				InputTokens:     1500, OutputTokens: 800,
				Generated: time.Now(),
			},
		},
	}
}

func TestRenderMarkdownPickLeadsWithFullContent(t *testing.T) {
	md := RenderMarkdown(sampleBundle())

	pickIdx := strings.Index(md, "### Pick: angle 1 / title 1.2 / body 1.1")
	if pickIdx == -1 {
		t.Fatalf("Pick header missing\n%s", md)
	}
	altIdx := strings.Index(md, "### Alternatives")
	if altIdx == -1 {
		t.Fatalf("Alternatives header missing\n%s", md)
	}
	if pickIdx > altIdx {
		t.Errorf("Pick should appear before Alternatives (pick=%d, alt=%d)", pickIdx, altIdx)
	}

	// Reasoning rendered as a blockquote
	if !strings.Contains(md, "> fits the sub better than a pitch") {
		t.Error("reasoning blockquote missing")
	}
	// Recommended title text rendered in full
	if !strings.Contains(md, "**Title:** Title A2") {
		t.Errorf("recommended title text not rendered in full")
	}
	// Recommended body text rendered in full (not just metadata)
	if !strings.Contains(md, "Body A1 content one two three.") {
		t.Errorf("recommended body text not rendered in full")
	}
}

func TestRenderMarkdownAlternativesAreCompressed(t *testing.T) {
	md := RenderMarkdown(sampleBundle())

	// Picked body must NOT appear a second time as full content under Alternatives.
	pickedBodyText := "Body A1 content one two three."
	if strings.Count(md, pickedBodyText) != 1 {
		t.Errorf("picked body should appear exactly once (in Pick), got %d occurrences", strings.Count(md, pickedBodyText))
	}

	// Picked title should be marked in the alternatives Titles list
	if !strings.Contains(md, "`1.2` Title A2 ← picked") {
		t.Errorf("picked title not marked in alternatives\n%s", md)
	}

	// Recommended angle should be marked
	if !strings.Contains(md, "**Angle 1** — war story *(recommended angle)*") {
		t.Errorf("recommended angle not marked")
	}
	// Non-recommended angle should NOT have the marker
	if strings.Contains(md, "**Angle 2** — technical pitch *(recommended angle)*") {
		t.Errorf("non-recommended angle wrongly marked as recommended")
	}

	// Other bodies in the recommended angle: appear as metadata with word count + tags
	if !strings.Contains(md, "Other bodies:") {
		t.Error("'Other bodies:' label missing for recommended angle")
	}
	if !strings.Contains(md, "`1.2` (~11 words)") {
		t.Errorf("body 1.2 word count metadata wrong\n%s", md)
	}
	if !strings.Contains(md, "_opener:question_") {
		t.Errorf("body 1.2 tag missing")
	}

	// Body 2.1 is NOT the picked body, but is the only body in non-recommended angle 2.
	// Should appear under "Bodies:" (not "Other bodies:") with metadata only.
	if !strings.Contains(md, "Bodies:\n- `2.1`") {
		t.Errorf("non-recommended angle bodies section wrong\n%s", md)
	}
	// Body 2.1's full text should NOT be rendered.
	if strings.Contains(md, "Body B1 content with some words.") {
		t.Error("non-recommended body text should not be rendered in full")
	}
}

func TestRenderMarkdownMetaSection(t *testing.T) {
	md := RenderMarkdown(sampleBundle())
	if !strings.Contains(md, "**Flair:** discussion (required)") {
		t.Error("flair not rendered")
	}
	if !strings.Contains(md, "**Suggested window:** Tue-Thu 9-11am ET") {
		t.Error("suggested window not rendered")
	}
	if !strings.Contains(md, "_tokens: in=1500 out=800_") {
		t.Error("token usage not rendered")
	}
}

func TestRenderMarkdownProviderHeader(t *testing.T) {
	md := RenderMarkdown(sampleBundle())
	if !strings.Contains(md, "## anthropic · claude-sonnet-4-7 · risk: low") {
		t.Errorf("provider header format wrong\n%s", md)
	}
}

func TestRenderMarkdownRefusePath(t *testing.T) {
	bundle := &types.DraftBundle{
		Sub:       "golang",
		Brief:     types.Brief{Title: "Marketing post"},
		Generated: time.Now(),
		Drafts: []types.Draft{
			{
				Provider:   "anthropic",
				Model:      "claude-sonnet-4-7",
				Risk:       types.RiskRefuse,
				RiskReason: "r/golang rejects bare promotion",
				Generated:  time.Now(),
			},
		},
	}
	md := RenderMarkdown(bundle)
	if !strings.Contains(md, "## anthropic · claude-sonnet-4-7 · risk: refuse") {
		t.Errorf("refuse provider header wrong:\n%s", md)
	}
	if !strings.Contains(md, "**Refused:** r/golang rejects bare promotion") {
		t.Errorf("refuse reason wrong:\n%s", md)
	}
	if strings.Contains(md, "### Pick:") || strings.Contains(md, "### Alternatives") {
		t.Error("refuse should not render Pick or Alternatives")
	}
}

func TestRenderMarkdownErrorPath(t *testing.T) {
	bundle := &types.DraftBundle{
		Sub:       "golang",
		Generated: time.Now(),
		Drafts: []types.Draft{
			{
				Provider:  "openai",
				Model:     "gpt-5",
				Error:     "rate limited",
				Generated: time.Now(),
			},
		},
	}
	md := RenderMarkdown(bundle)
	if !strings.Contains(md, "**Error:** rate limited") {
		t.Errorf("error rendering wrong:\n%s", md)
	}
	if strings.Contains(md, "### Pick:") || strings.Contains(md, "### Alternatives") {
		t.Error("error path should not render Pick or Alternatives")
	}
}

func TestRenderMarkdownMissingRecommendationIDs(t *testing.T) {
	bundle := &types.DraftBundle{
		Sub:       "golang",
		Brief:     types.Brief{Title: "X"},
		Generated: time.Now(),
		Drafts: []types.Draft{
			{
				Provider: "anthropic", Model: "x", Risk: "low",
				Angles: []types.Angle{
					{ID: 1, Titles: []types.Title{{ID: "1.1", Text: "T"}}, Bodies: []types.Body{{ID: "1.1", Text: "B"}}},
				},
				// Recommendation points at non-existent angle 99
				Recommendation: types.Recommendation{AngleID: 99, TitleID: "9.9", BodyID: "9.9"},
				Generated:      time.Now(),
			},
		},
	}
	md := RenderMarkdown(bundle)
	if !strings.Contains(md, "**Pick:** none (recommendation references missing ids)") {
		t.Errorf("missing-id fallback not rendered:\n%s", md)
	}
	// Alternatives should still render so the user has something to work with.
	if !strings.Contains(md, "### Alternatives") {
		t.Errorf("alternatives should still render when recommendation is broken")
	}
}

func TestWordCount(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"one", 1},
		{"one two three", 3},
		{"  spaced   out  words  ", 3},
		{"newlines\nbetween\nwords", 3},
	}
	for _, c := range cases {
		if got := wordCount(c.in); got != c.want {
			t.Errorf("wordCount(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}
