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
    {"id":"shorter","label":"shorter","text":"Shorter text.","reasoning":"shortest viable answer"},
    {"id":"counterpoint","label":"counterpoint","text":"Counterpoint engaging the batching pushback.","reasoning":"top comment's batching critique deserves a response"}
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
	for _, want := range []string{"best", "shorter", "counterpoint"} {
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
		"Flair: Marketing",                   // flair threaded through (PR #19)
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
		"name or pitch the project",          // bold markup may surround "not"
		"lens, not a topic",                  // context-as-lens guidance
		"Sub-category inference",             // abstract category guidance, not named-sub list
		// v2 variant slots — these are the labels the prompt asks the model to emit.
		"`shorter`",
		"`counterpoint`",
		"`warmer-personal`",
		"`question`",
		// Distinct-frame rule (new in v2)
		"Distinct-frame rule",
		// Bullet rule (new in v2)
		"Bullet rule",
		// First-person ban + consensus-repeat ban (carried over)
		"Fabricate first-person experience",
		"Repeat the consensus",
		"Anti-tells",
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
	// V2 removes `detailed` entirely and renames `short` → `shorter`,
	// `thread-aware` → `counterpoint`, `personal-story` → `warmer-personal`,
	// `question-first` → `question`. The regression-guard checks for the
	// slot-definition format used in the prompt's task list ( **`label`** )
	// rather than any mention of the legacy name — the prompt itself is
	// allowed to say "there is no `detailed` variant" as a model-facing
	// instruction.
	bannedSlotDefinitions := []string{
		"**`detailed`**",
		"**`thread-aware`**",
		"**`personal-story`**",
		"**`question-first`**",
		"**`short`**",
	}
	for _, b := range bannedSlotDefinitions {
		if strings.Contains(prompt, b) {
			t.Errorf("prompt should not define legacy/removed variant slot %q (v2 spec)\n--- prompt ---\n%s", b, prompt)
		}
	}

	// Drafts-array cap regression guard. SPEC_REPLY_REFINEMENT.md is
	// emphatic: "Two to four strong drafts are better than a full menu"
	// and "Keep default output to 2-4 drafts total." The variant set has
	// 5 slots (best/shorter/counterpoint/warmer-personal/question), so
	// any "2-5" wording in the Output section would license the full
	// menu the spec calls a failure. The Output section sits last in the
	// prompt and tends to anchor numerical behavior — keep it at 2-4.
	bannedCountWording := []string{
		"2-5 entries",
		"2 to 5 entries",
		"2-5 drafts",
		"2 to 5 drafts",
	}
	for _, b := range bannedCountWording {
		if strings.Contains(prompt, b) {
			t.Errorf("prompt licenses the full 5-slot menu (%q); spec caps at 2-4\n--- prompt ---\n%s", b, prompt)
		}
	}
	if !strings.Contains(prompt, "2-4 entries") && !strings.Contains(prompt, "2 to 4") {
		t.Errorf("prompt should specify the 2-4 drafts cap explicitly\n--- prompt ---\n%s", prompt)
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

// Three structural rules added to mirror PR #28's review-mode tightening:
// (1) ban the workshop-curriculum Best Pick template, (2) require thesis
// in the first paragraph, (3) require explicit consensus engagement when
// the thread has a clear repeated pattern.
func TestRenderReplyPromptStructuralRules(t *testing.T) {
	in := sampleDraftInput()
	prompt, err := RenderReplyPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	// (1) workshop-curriculum ban — concrete forbidden template + explicit
	// failure mode language so the model has both positive and negative
	// signals. Scoped to non-technical threads (technical/troubleshooting
	// threads legitimately need bullets/numbered steps for code/config/
	// diagnostics).
	curriculumBans := []string{
		"workshop curriculum",                        // section title
		"FORBIDDEN",                                  // strong signal
		"Pick one [thing]. Then [verb] [N] minutes",  // template fragment
		"funnel-speak chains",                        // explicit anti-pattern
		"maintenance mode folder",                    // jargon called out by name
		"performance + lifecycle stack",              // jargon called out by name
		"stack of consultant patterns",               // failure-mode framing (the thing the rule actually targets)
		"3+ of the patterns above",                   // explicit counting rule
	}
	for _, s := range curriculumBans {
		if !strings.Contains(prompt, s) {
			t.Errorf("reply prompt missing curriculum-ban rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Technical-thread exemption — the ban must NOT over-fire on
	// legitimate technical/troubleshooting answers where bulleted steps,
	// numerics, and config snippets are correct Reddit voice.
	technicalExemptionChecks := []string{
		"technical / troubleshooting / engineering",
		"this rule is RELAXED",
		"work_mem`",                                  // concrete technical example showing what's allowed
		"upvoted answer in a Postgres-tuning thread", // explicit "this is correct voice for those threads" (generic, no named sub)
		"not concrete-steps technical answers",       // explicit clarification of what the rule does NOT target
	}
	for _, s := range technicalExemptionChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("reply prompt missing technical-thread exemption %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// (2) first-paragraph thesis rule — covers the "setup paragraph then
	// pivot" failure mode observed in the Shopify run.
	thesisChecks := []string{
		"First-paragraph thesis rule",
		"first paragraph of `Best Pick` MUST carry the thesis frame",
		"Opening with a strength acknowledgment as a separate beat",
		"Opening with operational specifics",
		"a Reddit reader would screenshot and quote",
	}
	for _, s := range thesisChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("reply prompt missing thesis-rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// (3) consensus engagement — softer wording per spec discussion ("when
	// the thread has a clear repeated advice pattern", NOT "3+ commenters
	// echoing"). Implicit engagement is explicitly insufficient.
	consensusChecks := []string{
		"Consensus engagement rule",
		"thread has a clear repeated advice pattern",
		"Agree-and-extend",
		"Push back",
		"Implicit engagement (writing advice that happens to align with the consensus without naming it)",
	}
	for _, s := range consensusChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("reply prompt missing consensus-rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Anti-tells coverage too — the rules should be reinforced from both
	// the rule-section AND the anti-tell direction.
	antiTellChecks := []string{
		"Walk past consensus without naming it",
		"Workshop-curriculum `Best Pick` on non-technical threads", // scoping explicit in the anti-tell too
		"Setup paragraph before the thesis",
	}
	for _, s := range antiTellChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("reply prompt missing anti-tell %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Word-count language deliberately NOT tightened (per spec discussion
	// — structural rules carry the load, word counts stay soft). Confirm
	// the existing soft-cap line is still there and we didn't accidentally
	// tighten it.
	if !strings.Contains(prompt, "Word counts are guidance, not hard limits") {
		t.Errorf("reply prompt should preserve soft word-count guidance — structural rules do the work\n--- prompt ---\n%s", prompt)
	}
}
