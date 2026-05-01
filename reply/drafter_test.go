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
    {"id":"best","label":"best","text":"Best draft text.","reasoning":"concise + actionable"},
    {"id":"short","label":"short","text":"Short text.","reasoning":"shortest viable"},
    {"id":"detailed","label":"detailed","text":"Detailed text.","reasoning":"more depth"},
    {"id":"question-first","label":"question-first","text":"What kind of cart?","reasoning":"need more info"}
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
	if len(bundle.Drafts) != 4 {
		t.Errorf("drafts len = %d", len(bundle.Drafts))
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
		"best plugins for performance?",
		"Looking for plugin recs",
		"alice", "Try X", // top comment leaked in
		"Who you're writing as",
		"Five years running Postgres",
		"Context (project material",
		"From bank (kova)",
		"From path",
		"kova body content",
		"notes body",
		"Do not name or pitch the project",
		"Anti-tells",
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", s, prompt)
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
