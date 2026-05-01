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
			{ID: "shorter", Label: "shorter", Text: "Shorter text.", Reasoning: "shortest viable"},
			{ID: "counterpoint", Label: "counterpoint", Text: "Engages the batching pushback.", Reasoning: "top comment counterpoint"},
			{ID: "warmer-personal", Label: "warmer-personal", Text: "Story-shaped reply.", Reasoning: "uses one-person handmade shop story from personal.md"},
			{ID: "question", Label: "question", Text: "What's your stack?", Reasoning: "need more info"},
		},
		PickID:    "best",
		Generated: time.Now(),
	}
}

func TestRenderMarkdownBestPickLeads(t *testing.T) {
	md := RenderMarkdown(sampleBundle(), "best plugins for performance?", "/tmp/sessions/replies/x")

	pickIdx := strings.Index(md, "## Best Pick")
	altIdx := strings.Index(md, "## Alternative Picks")
	if pickIdx == -1 || altIdx == -1 {
		t.Fatalf("missing section headers\n--- output ---\n%s", md)
	}
	if pickIdx > altIdx {
		t.Errorf("Best Pick should come before Alternative Picks")
	}

	checks := []string{
		"# Reply drafts for r/shopify",
		"Thread: best plugins for performance?",
		"Mode: reply",
		"Session: /tmp/sessions/replies/x",
		"## Best Pick",                             // header per spec
		"Best reply text here.",                    // pick text in full
		"## Alternative Picks",                     // renamed from "Alternatives" per v2 spec
		"### Shorter",                              // v2 label, replaces "Short"
		"Shorter text.",
		"### Counterpoint",                         // v2 label, replaces "Thread-Aware"
		"### Warmer / Personal",                    // v2 label with explicit spacing per spec
		"### Question",                             // v2 label, replaces "Question First"
		"What's your stack?",
	}
	for _, c := range checks {
		if !strings.Contains(md, c) {
			t.Errorf("missing %q\n--- output ---\n%s", c, md)
		}
	}
}

// Per SPEC_REPLY_REFINEMENT.md (v2) "Output rendering", the rendered
// markdown does NOT include per-variant reasoning. Reasoning stays in
// drafts.json for audit/debug only — surfacing it here would turn the
// output into a report about the comments rather than the comments
// themselves.
func TestRenderMarkdownDoesNotIncludeReasoning(t *testing.T) {
	md := RenderMarkdown(sampleBundle(), "title", "/x")
	bannedReasoningStrings := []string{
		"> concise + fits sub",      // old blockquote on best pick
		"*shortest viable*",         // old italic on alternative
		"*top comment counterpoint*",
		"*need more info*",
		"concise + fits sub",        // even un-decorated reasoning shouldn't appear
		"shortest viable",
		"top comment counterpoint",
		"uses one-person handmade shop story from personal.md",
		"need more info",
		// Forbidden user-facing labels from older drafts:
		"Why this works",
		"Editing Notes",
	}
	for _, banned := range bannedReasoningStrings {
		if strings.Contains(md, banned) {
			t.Errorf("rendered markdown should not contain reasoning text %q\n--- output ---\n%s", banned, md)
		}
	}
}

// Old "## Alternatives" section header was renamed to "## Alternative Picks"
// in v2. This test catches the regression of the old name slipping back in.
func TestRenderMarkdownUsesAlternativePicks(t *testing.T) {
	md := RenderMarkdown(sampleBundle(), "title", "/x")
	if strings.Contains(md, "## Alternatives\n") {
		t.Errorf("rendered markdown should not contain old '## Alternatives' header\n--- output ---\n%s", md)
	}
	if !strings.Contains(md, "## Alternative Picks") {
		t.Errorf("rendered markdown must contain '## Alternative Picks' header per v2 spec\n--- output ---\n%s", md)
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
	// renders, but Alternative Picks still lists everything.
	b := sampleBundle()
	b.PickID = "nope"
	md := RenderMarkdown(b, "title", "")
	if strings.Contains(md, "## Best Pick") {
		t.Error("missing pick should suppress Best Pick header")
	}
	if !strings.Contains(md, "## Alternative Picks") {
		t.Error("Alternative Picks should still render")
	}
	for _, want := range []string{"### Best", "### Shorter", "### Counterpoint", "### Warmer / Personal", "### Question"} {
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
		// v2 reply-mode labels (SPEC_REPLY_REFINEMENT.md "Output rendering").
		{"best", "Best"},
		{"shorter", "Shorter"},
		{"counterpoint", "Counterpoint"},
		{"warmer-personal", "Warmer / Personal"}, // explicit spacing per spec
		{"question", "Question"},
		// Review-mode-specific (SPEC_REVIEW_DRAFT_REFINEMENT.md). Renders
		// with a space, not a hyphen — the spec calls it a noun phrase.
		{"structured-review", "Structured Review"},
		// Legacy ids preserved in the map for old session re-renders.
		{"short", "Short"},
		{"thread-aware", "Thread-Aware"},
		{"personal-story", "Personal Story"},
		{"question-first", "Question First"},
		{"detailed", "Detailed"},
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
