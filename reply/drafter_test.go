package reply

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/types"
)

func sampleDraftInput() *DraftInput {
	return &DraftInput{
		Thread: &types.Thread{
			Subreddit: "shopify",
			Flair:     "Marketing",
			Title:     "best plugins for performance?",
			Body:      "Looking for plugin recs to speed up the cart.",
			URL:       "https://www.reddit.com/r/shopify/comments/abc/best/",
			Comments: []types.Comment{
				{ID: "c1", Author: "alice", Body: "Try X", Score: 50},
			},
		},
		Mode: &types.ReplyModeResult{Mode: types.ReplyModeReply},
	}
}

const goodDrafterResp = `{
  "drafts": [
    {"id":"best","label":"best","text":"Best draft text.","reasoning":"one sharp frame for solo store owner"},
    {"id":"short","label":"short","text":"Short text.","reasoning":"shortest viable answer"},
    {"id":"thread-aware","label":"thread-aware","text":"Engaging the batching pushback.","reasoning":"top comment's batching critique deserves a response"}
  ],
  "pick_id": "best"
}`

func TestGenerateReplyHappyPath(t *testing.T) {
	p := &fakeProvider{name: "fake", response: goodDrafterResp}
	bundle, err := GenerateReply(context.Background(), p, "gpt-5", sampleDraftInput(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Subreddit != "shopify" || bundle.Mode != types.ReplyModeReply {
		t.Errorf("bundle metadata: %+v", bundle)
	}
	if len(bundle.Drafts) != 3 {
		t.Errorf("drafts len = %d (expected 3 for new variant model)", len(bundle.Drafts))
	}
	gotLabels := map[string]bool{}
	for _, d := range bundle.Drafts {
		gotLabels[d.Label] = true
	}
	for _, want := range []string{"best", "short", "thread-aware"} {
		if !gotLabels[want] {
			t.Errorf("missing variant %q in bundle", want)
		}
	}
	if bundle.PickID != "best" {
		t.Errorf("pick = %q", bundle.PickID)
	}
	if !p.gotReq.JSONMode {
		t.Error("drafter should set JSONMode")
	}
}

func TestGenerateReplyDefaultsPickWhenModelOmits(t *testing.T) {
	const respNoPick = `{"drafts":[
		{"id":"best","label":"best","text":"text","reasoning":"r"}
	]}`
	p := &fakeProvider{name: "fake", response: respNoPick}
	bundle, err := GenerateReply(context.Background(), p, "x", sampleDraftInput(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.PickID != "best" {
		t.Errorf("expected pick to default to first draft id, got %q", bundle.PickID)
	}
}

func TestGenerateReplyEmptyDraftsErrors(t *testing.T) {
	const respEmpty = `{"drafts":[],"pick_id":""}`
	p := &fakeProvider{name: "fake", response: respEmpty}
	_, err := GenerateReply(context.Background(), p, "x", sampleDraftInput(), 0)
	if err == nil || !strings.Contains(err.Error(), "no drafts returned") {
		t.Errorf("expected no-drafts error, got %v", err)
	}
}

func TestGenerateReplyBadJSONErrors(t *testing.T) {
	p := &fakeProvider{name: "fake", response: "not json"}
	_, err := GenerateReply(context.Background(), p, "x", sampleDraftInput(), 0)
	if err == nil || !strings.Contains(err.Error(), "parse json") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestGenerateReplyProviderErrorPropagated(t *testing.T) {
	p := &fakeProvider{name: "fake", err: errors.New("rate limited")}
	_, err := GenerateReply(context.Background(), p, "x", sampleDraftInput(), 0)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected provider error, got %v", err)
	}
}

func TestGenerateReplyNilInputErrors(t *testing.T) {
	p := &fakeProvider{name: "fake", response: goodDrafterResp}
	_, err := GenerateReply(context.Background(), p, "x", nil, 0)
	if err == nil {
		t.Error("nil input should error")
	}
}

func TestRenderReplyPromptIncludesContextAndAuthor(t *testing.T) {
	input := sampleDraftInput()
	input.AuthorContext = "Five years running Postgres at Cloudflare."
	input.Contexts = []types.LoadedReplyContext{
		{ID: "kova", Source: "bank", Path: "/x/kova.md", Body: "kova body content"},
		{Source: "path", Path: "/y/notes.md", Body: "notes body"},
	}

	prompt, err := RenderReplyPrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"r/shopify",
		"Flair: Marketing",                   // flair threaded through (new)
		"best plugins for performance?",
		"Looking for plugin recs",
		"alice", "Try X",                     // top comment leaked in
		"Who you're writing as",
		"Five years running Postgres",
		"voice and judgment only",            // author_context redefined as voice-only
		"Context (project material",
		"From bank (kova)",
		"From path",
		"kova body content",
		"notes body",
		"Do not name or pitch the project",
		"lens, not a topic",                  // context-as-lens guidance
		"Sub-category inference",             // abstract category guidance, not named-sub list
		"thread-aware",                       // new variant slot
		"personal-story",                     // new variant slot
		"Fabricate first-person experience",  // explicit ban on fake autobiography
		"Repeat the consensus",               // engage-don't-duplicate rule
		"Anti-tells",
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

// Flair is conditional: it must appear in the rendered prompt only when
// Thread.Flair is non-empty. Empty flair should not produce a stray
// "Flair: " line.
func TestRenderReplyPromptOmitsFlairLineWhenEmpty(t *testing.T) {
	input := sampleDraftInput()
	input.Thread.Flair = ""

	prompt, err := RenderReplyPrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Flair:") {
		t.Errorf("empty Flair should not render a Flair: line\n--- prompt ---\n%s", prompt)
	}
	// Subreddit line still renders.
	if !strings.Contains(prompt, "Subreddit: r/shopify") {
		t.Error("Subreddit line should still render even when flair is empty")
	}
}

// The new prompt forbids hardcoded named-subreddit category lookups.
// Sub-category inference comes from Subreddit + Flair + body, described
// abstractly. Catching the regression of a "named subs" list slipping
// back in: assert that example sub names from the spec body do NOT
// appear in the rendered prompt.
func TestRenderReplyPromptDoesNotEnumerateNamedSubs(t *testing.T) {
	input := sampleDraftInput()
	prompt, err := RenderReplyPrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	bannedExamples := []string{
		"r/EtsySellers",
		"r/ecommerce",
		"r/Entrepreneur",
		"r/marketing",
		"r/PostgreSQL",
		"r/kubernetes",
		"r/sysadmin",
	}
	for _, b := range bannedExamples {
		if strings.Contains(prompt, b) {
			t.Errorf("prompt should not enumerate named subs (%q found) — sub-category inference must come from category descriptions", b)
		}
	}
}

func TestRenderReplyPromptOmitsEmptySections(t *testing.T) {
	input := &DraftInput{
		Thread: &types.Thread{Subreddit: "x", Title: "t", Body: "", Comments: nil},
		Mode:   &types.ReplyModeResult{Mode: types.ReplyModeReply},
		// No author context, no contexts
	}
	prompt, err := RenderReplyPrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	// Match the SECTION HEADER, not the phrase "Who you're writing as"
	// which the prompt's anti-tells section references regardless.
	if strings.Contains(prompt, "# Who you're writing as") {
		t.Error("author section header should be omitted when AuthorContext empty")
	}
	if strings.Contains(prompt, "# Context (project material") {
		t.Error("context section header should be omitted when no contexts")
	}
	if strings.Contains(prompt, "Top comments") {
		t.Error("comments section should be omitted when empty")
	}
}
