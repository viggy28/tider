package draft

import (
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func TestRenderMarkdownHappyPath(t *testing.T) {
	bundle := &types.DraftBundle{
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
						Titles: []types.Title{{ID: "1.1", Text: "Title A"}, {ID: "1.2", Text: "Title B"}},
						Bodies: []types.Body{{ID: "1.1", Text: "Body content here.", Tags: []string{"opener:war-story"}}},
					},
				},
				Recommendation: types.Recommendation{AngleID: 1, TitleID: "1.2", BodyID: "1.1", Reasoning: "fits the sub"},
				Flair:          types.FlairRec{Required: true, Suggested: "discussion"},
				SuggestedWindow: "Tue-Thu 9-11am ET",
				InputTokens:    1500, OutputTokens: 800,
				Generated: time.Now(),
			},
			{
				Sub:      "golang",
				Provider: "openai",
				Model:    "gpt-5",
				Risk:     "medium",
				Angles: []types.Angle{
					{
						ID: 1, Premise: "technical pitch",
						Titles: []types.Title{{ID: "1.1", Text: "Different angle title"}},
						Bodies: []types.Body{{ID: "1.1", Text: "Different body content."}},
					},
				},
				Recommendation: types.Recommendation{AngleID: 1, TitleID: "1.1", BodyID: "1.1"},
				Flair:          types.FlairRec{Required: false, Suggested: "show & tell"},
				Generated: time.Now(),
			},
		},
	}

	md := RenderMarkdown(bundle)

	checks := []string{
		"# Drafts for r/golang",
		"**Brief:** Streambed",
		"WAL-native CDC.",
		"2 provider(s)",
		"## anthropic · claude-sonnet-4-7",
		"## openai · gpt-5",
		"**Risk:** low",
		"**Risk:** medium",
		"**Recommendation:** angle 1, title 1.2, body 1.1",
		"fits the sub",
		"### Angle 1: war story",
		"*Hook:* what surprised me",
		"`1.1` Title A",
		"`1.2` Title B",
		"opener:war-story",
		"Body content here.",
		"**Flair:** discussion (required)",
		"**Flair:** show & tell (optional)",
		"**Suggested window:** Tue-Thu 9-11am ET",
		"tokens: in=1500 out=800",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Errorf("markdown missing %q\n--- output ---\n%s", c, md)
		}
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
	if !strings.Contains(md, "**Risk:** refuse — r/golang rejects bare promotion") {
		t.Errorf("refuse rendering wrong:\n%s", md)
	}
	if strings.Contains(md, "### Angle") {
		t.Error("refuse should not render any angles")
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
}
