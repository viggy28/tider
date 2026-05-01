package reply

import (
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func sampleBundle() *types.ReplyBundle {
	return &types.ReplyBundle{
		ThreadURL: "https://www.reddit.com/r/shopify/comments/abc/best/",
		Subreddit: "shopify",
		Mode:      types.ReplyModeReply,
		Drafts: []types.ReplyDraft{
			{ID: "best", Label: "best", Text: "Best reply text here.", Reasoning: "concise + fits sub"},
			{ID: "short", Label: "short", Text: "Short text.", Reasoning: "shortest viable"},
			{ID: "thread-aware", Label: "thread-aware", Text: "Engages the batching pushback.", Reasoning: "top comment counterpoint"},
			{ID: "personal-story", Label: "personal-story", Text: "Story-shaped reply.", Reasoning: "uses one-person handmade shop story from personal.md"},
			{ID: "question-first", Label: "question-first", Text: "What's your stack?", Reasoning: "need more info"},
		},
		PickID:    "best",
		Generated: time.Now(),
	}
}

func TestRenderMarkdownBestPickLeads(t *testing.T) {
	md := RenderMarkdown(sampleBundle(), "best plugins for performance?", "/tmp/sessions/replies/x")

	pickIdx := strings.Index(md, "## Best Pick")
	altIdx := strings.Index(md, "## Alternatives")
	if pickIdx == -1 || altIdx == -1 {
		t.Fatalf("missing section headers\n--- output ---\n%s", md)
	}
	if pickIdx > altIdx {
		t.Errorf("Best Pick should come before Alternatives")
	}

	checks := []string{
		"# Reply drafts for r/shopify",
		"Thread: best plugins for performance?",
		"Mode: reply",
		"Session: /tmp/sessions/replies/x",
		"## Best Pick",                         // header per spec
		"> concise + fits sub",                 // pick reasoning as blockquote
		"Best reply text here.",                // pick text in full
		"## Alternatives",
		"### Short",
		"*shortest viable*",                    // alt reasoning as italic
		"Short text.",
		"### Thread-Aware",                     // compound modifier — hyphen retained per spec
		"### Personal Story",                   // noun phrase — space, not hyphen, per spec
		"### Question First",                   // noun phrase — space, not hyphen, per spec
		"What's your stack?",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Errorf("missing %q\n--- output ---\n%s", c, md)
		}
	}
}

func TestRenderMarkdownWithoutSessionPath(t *testing.T) {
	md := RenderMarkdown(sampleBundle(), "title", "")
	if strings.Contains(md, "Session:") {
		t.Errorf("empty session path should omit the line, got:\n%s", md)
	}
	if !strings.Contains(md, "Thread: title") {
		t.Errorf("title should still render: %s", md)
	}
}

func TestRenderMarkdownWithoutThreadTitle(t *testing.T) {
	md := RenderMarkdown(sampleBundle(), "", "/x")
	if strings.Contains(md, "Thread:") {
		t.Errorf("empty title should omit the Thread: line")
	}
}

func TestRenderMarkdownPickIDNotInDrafts(t *testing.T) {
	// Defensive: if PickID points at a missing draft, no Best Pick section
	// renders, but Alternatives still lists everything.
	b := sampleBundle()
	b.PickID = "nope"
	md := RenderMarkdown(b, "title", "")
	if strings.Contains(md, "## Best Pick") {
		t.Error("missing pick should suppress Best Pick header")
	}
	if !strings.Contains(md, "## Alternatives") {
		t.Error("Alternatives should still render")
	}
	for _, want := range []string{"### Best", "### Short", "### Thread-Aware", "### Personal Story", "### Question First"} {
		if !strings.Contains(md, want) {
			t.Errorf("alternative %q should still render", want)
		}
	}
}

func TestRenderMarkdownNilBundle(t *testing.T) {
	if got := RenderMarkdown(nil, "x", "y"); got != "" {
		t.Errorf("nil bundle should render empty, got %q", got)
	}
}

// Review-mode renders an inspection-depth header showing what was
// inspected. Reply-mode bundles (Inspection nil) render the previous
// shape unchanged. Per SPEC_REVIEW_VISUAL_FIRECRAWL.md "Output rendering".
func TestRenderMarkdownReviewModeShowsInspectionHeader(t *testing.T) {
	b := sampleBundle()
	b.Mode = types.ReplyModeReview
	b.Inspection = &types.InspectionSummary{
		Source:         "firecrawl",
		ScreenshotPath: "/sessions/abc/screenshots/homepage.png",
		ImagesAnalyzed: 4,
		ShopType:       "handmade",
		Limitations:    []string{"homepage only", "checkout not inspected"},
	}
	md := RenderMarkdown(b, "review my shop?", "/sessions/abc")
	checks := []string{
		"Mode: review",
		"Inspection: Firecrawl visual",
		"Screenshot: saved",
		"Images analyzed: 4",
		"Shop type: handmade",
		"Limitations: homepage only; checkout not inspected",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Errorf("missing %q\n--- output ---\n%s", c, md)
		}
	}
}

// Reply-mode bundles (no Inspection) render the original shape — no
// stray Inspection / Screenshot / Limitations lines.
func TestRenderMarkdownReplyModeOmitsInspectionHeader(t *testing.T) {
	b := sampleBundle()
	if b.Inspection != nil {
		t.Fatal("sampleBundle should not pre-populate Inspection")
	}
	md := RenderMarkdown(b, "title", "/x")
	for _, banned := range []string{"Inspection:", "Screenshot:", "Images analyzed:", "Shop type:", "Limitations:"} {
		if strings.Contains(md, banned) {
			t.Errorf("reply-mode rendering should omit %q\n--- output ---\n%s", banned, md)
		}
	}
}

func TestTitleCaseLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		// Spec-mandated display forms (SPEC_REPLY_REFINEMENT.md "Output rendering").
		{"best", "Best"},
		{"short", "Short"},
		{"thread-aware", "Thread-Aware"},          // hyphen retained — compound modifier
		{"personal-story", "Personal Story"},      // space — noun phrase
		{"question-first", "Question First"},      // space — noun phrase
		{"detailed", "Detailed"},
		{"structured-review", "Structured-Review"}, // hyphen retained — compound modifier (review-mode variant)
		// Unknown labels fall back to kebab→title-case-with-hyphens so future
		// variant names render reasonably without a code change.
		{"long-multi-part", "Long-Multi-Part"},
		{"", ""},
		{"a", "A"},
	}
	for _, c := range cases {
		if got := titleCaseLabel(c.in); got != c.want {
			t.Errorf("titleCaseLabel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
