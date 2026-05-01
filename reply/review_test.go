package reply

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/types"
)

func sampleInspection() *types.Inspection {
	return &types.Inspection{
		URL:             "https://my-shop.example.com",
		Status:          200,
		Title:           "My Etsy Shop",
		MetaDescription: "Wheel-thrown ceramics by a single artisan.",
		OGTitle:         "My Etsy Shop",
		OGDescription:   "OG description",
		Headings: []types.Heading{
			{Level: 1, Text: "Welcome to my shop"},
			{Level: 2, Text: "Latest pieces"},
		},
		Snippets: []string{
			"Each piece is handmade in my home studio.",
			"Mug — $40, glazed celadon.",
		},
	}
}

const sampleNotesResp = `{
  "strengths": ["Clear meta description names what you sell", "h1 is welcoming"],
  "weaknesses": ["No clear pricing on the landing page", "h1 doesn't say WHAT you sell upfront"],
  "suggestions": ["Lead h1 with 'Handmade ceramics' instead of 'Welcome'", "Add a featured-products section"],
  "open_questions": ["Are photos high-resolution?", "Do you have customer reviews?"]
}`

func TestBuildReviewNotesHappy(t *testing.T) {
	p := &fakeProvider{name: "fake", response: sampleNotesResp}
	notes, err := BuildReviewNotes(context.Background(), p, "gpt-5", sampleInspection(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if notes.TargetURL != "https://my-shop.example.com" {
		t.Errorf("target URL: %q", notes.TargetURL)
	}
	if len(notes.Strengths) != 2 || len(notes.Weaknesses) != 2 || len(notes.Suggestions) != 2 || len(notes.OpenQuestions) != 2 {
		t.Errorf("category counts off: %+v", notes)
	}
	if !p.gotReq.JSONMode {
		t.Error("notes call should be JSONMode")
	}
}

func TestBuildReviewNotesNilInspectionErrors(t *testing.T) {
	p := &fakeProvider{name: "fake", response: sampleNotesResp}
	_, err := BuildReviewNotes(context.Background(), p, "x", nil, 0)
	if err == nil || !strings.Contains(err.Error(), "nil inspection") {
		t.Errorf("expected nil-inspection error, got %v", err)
	}
}

func TestBuildReviewNotesBadJSON(t *testing.T) {
	p := &fakeProvider{name: "fake", response: "not json"}
	_, err := BuildReviewNotes(context.Background(), p, "x", sampleInspection(), 0)
	if err == nil || !strings.Contains(err.Error(), "parse json") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestBuildReviewNotesProviderError(t *testing.T) {
	p := &fakeProvider{name: "fake", err: errors.New("rate limited")}
	_, err := BuildReviewNotes(context.Background(), p, "x", sampleInspection(), 0)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected provider error, got %v", err)
	}
}

func TestRenderReviewNotesPromptIncludesInspectionFields(t *testing.T) {
	prompt, err := RenderReviewNotesPrompt(sampleInspection())
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"https://my-shop.example.com",
		"Title: My Etsy Shop",
		"Wheel-thrown ceramics",       // meta desc
		"OG title: My Etsy Shop",
		"(h1) Welcome to my shop",     // headings rendered with level
		"(h2) Latest pieces",
		"Each piece is handmade",      // snippet
		"Mug — $40",
		"GROUNDED",                    // anti-invention rule
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("notes prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

const sampleReviewDrafts = `{
  "drafts": [
    {"id":"best","label":"best","text":"Specific review reply citing the meta description and h1.","reasoning":"balances praise + actionable critique"},
    {"id":"short","label":"short","text":"Short review reply.","reasoning":"shortest viable"},
    {"id":"detailed","label":"detailed","text":"Detailed multi-section review.","reasoning":"thread has substance"},
    {"id":"question-first","label":"question-first","text":"Ask about pricing first.","reasoning":"open question is load-bearing"}
  ],
  "pick_id": "best"
}`

func sampleReviewInput() *ReviewDraftInput {
	return &ReviewDraftInput{
		Thread: &types.Thread{
			Subreddit: "EtsySellers",
			Title:     "Looking for feedback on my Etsy shop",
			Body:      "Here's the link...",
			URL:       "https://www.reddit.com/r/EtsySellers/comments/abc/feedback/",
		},
		Mode: &types.ReplyModeResult{Mode: types.ReplyModeReview, TargetURLs: []string{"https://my-shop.example.com"}},
		Notes: &types.ReviewNotes{
			TargetURL:     "https://my-shop.example.com",
			Strengths:     []string{"clear meta description"},
			Weaknesses:    []string{"h1 doesn't say what you sell"},
			Suggestions:   []string{"Lead h1 with 'Handmade ceramics'"},
			OpenQuestions: []string{"Are photos hi-res?"},
		},
	}
}

func TestGenerateReviewReplyHappy(t *testing.T) {
	p := &fakeProvider{name: "fake", response: sampleReviewDrafts}
	bundle, err := GenerateReviewReply(context.Background(), p, "gpt-5", sampleReviewInput(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Mode != types.ReplyModeReview {
		t.Errorf("mode: %q, want review", bundle.Mode)
	}
	if len(bundle.Drafts) != 4 {
		t.Errorf("drafts: %d", len(bundle.Drafts))
	}
	if bundle.PickID != "best" {
		t.Errorf("pick: %q", bundle.PickID)
	}
}

func TestGenerateReviewReplyNilInputErrors(t *testing.T) {
	p := &fakeProvider{name: "fake", response: sampleReviewDrafts}
	_, err := GenerateReviewReply(context.Background(), p, "x", nil, 0)
	if err == nil {
		t.Error("nil input should error")
	}
	in := sampleReviewInput()
	in.Notes = nil
	_, err = GenerateReviewReply(context.Background(), p, "x", in, 0)
	if err == nil {
		t.Error("nil notes should error")
	}
}

func TestRenderReviewPromptCitesNotesAndOmitsEmpty(t *testing.T) {
	in := sampleReviewInput()
	in.AuthorContext = "Five years at Cloudflare."
	in.Contexts = []types.LoadedReplyContext{{ID: "kova", Source: "bank", Body: "kova body"}}

	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"r/EtsySellers",
		"Target URL: https://my-shop.example.com",
		"## Strengths",
		"clear meta description",
		"## Weaknesses",
		"h1 doesn't say what you sell",
		"## Suggestions",
		"Lead h1 with 'Handmade ceramics'",
		"## Open questions",
		"Are photos hi-res?",
		"# Who you're writing as",
		"Five years at Cloudflare",
		"# Context (project material",
		"kova body",
		"Do not name or pitch the project",
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

func TestRenderReviewPromptOmitsEmptyCategories(t *testing.T) {
	in := sampleReviewInput()
	in.Notes = &types.ReviewNotes{TargetURL: "https://x.example.com"} // no strengths/weaknesses/etc.
	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"## Strengths", "## Weaknesses", "## Suggestions", "## Open questions"} {
		if strings.Contains(prompt, s) {
			t.Errorf("category section %q should be omitted when empty", s)
		}
	}
}
