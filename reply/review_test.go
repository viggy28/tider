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
    {"id":"shorter","label":"shorter","text":"Shorter review reply.","reasoning":"shortest viable"},
    {"id":"structured-review","label":"structured-review","text":"What works:\nWhat I would fix:\nBiggest priority:","reasoning":"thread has substance and OP asked for critique"},
    {"id":"question","label":"question","text":"Ask about pricing first.","reasoning":"open question is load-bearing"}
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
		"name or pitch the project", // bold markup may surround "not"; assert the substring that survives either form
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

// VisualNotes propagation: when ReviewDraftInput.VisualNotes is set,
// the review prompt MUST include the visual section so the drafter can
// cite specific visual observations. When VisualNotes is nil, the
// section MUST be omitted (no "Visual review notes" header) so the
// drafter doesn't hallucinate visual feedback for a thread that didn't
// have it.
func TestRenderReviewPromptIncludesVisualNotes(t *testing.T) {
	in := sampleReviewInput()
	in.VisualNotes = &types.VisualReviewNotes{
		ShopType: "handmade",
		Summary:  "warm one-person handmade studio with weak product proof",
		Observations: []types.VisualObservation{
			{
				Area:           "product_images",
				Finding:        "no in-hand or scale shot in the first 3 images",
				Evidence:       "all top-down crops",
				Severity:       "high",
				Recommendation: "add a hand-held photo to slot 1 or 2",
			},
		},
		KovaSignals: []string{"texture invisible at the photo crops shown"},
	}

	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"# Visual review notes",
		"ShopType: handmade",
		"warm one-person handmade studio",
		"product_images",
		"no in-hand or scale shot",
		"## Kova signals",
		"texture invisible at the photo crops",
		// Per SPEC_REVIEW_DRAFT_REFINEMENT.md the Kova lens is reshaped per
		// shop type — handmade/boutique gets the maker-process framing,
		// other shop types get reshaped advice (real-use imagery, etc.)
		// without that framing.
		"\"show your maker process\"", // appears in both the handmade-allowed and B2B-forbidden contexts
		"NOT \"show your maker process\"", // explicit reshape rule for B2B/SaaS/etc.
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

func TestRenderReviewPromptOmitsVisualSectionWhenNil(t *testing.T) {
	in := sampleReviewInput()
	in.VisualNotes = nil

	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "# Visual review notes") {
		t.Error("Visual review notes section should be omitted when VisualNotes is nil")
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

// SPEC_REVIEW_DRAFT_REFINEMENT.md tightens review-mode drafting after
// the PND fixture run produced an audit-shaped Best Pick with 5 fixes.
// The prompt must now enforce: 2-3 fixes (hard cap), severity-based
// ranking, mobile-claim handling for desktop-only captures, B2B
// pricing/policy guardrails with concrete allowed/forbidden phrases,
// reshaped Kova lens for non-handmade shop types, "Structured Review"
// display label (space, not hyphen), and dropping visual-notes
// questions[] from Best Pick / Shorter.
func TestRenderReviewPromptIncludesV2RefinementRules(t *testing.T) {
	in := sampleReviewInput()
	in.Contexts = []types.LoadedReplyContext{{ID: "kova", Source: "bank", Body: "kova body content"}}

	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	// Hard cap on fixes + tighter word budget for Best Pick.
	hardCapChecks := []string{
		"MAX 3 fixes",                 // explicit hard cap on Best Pick fix list
		"150-260 words",               // tighter word budget than the old 150-300
		"2 entries by default",        // drafts array default (was 2-3 before this PR)
		"emitting a full slate makes", // failure-mode note guarding the cap
	}
	for _, s := range hardCapChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing hard-cap rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Severity-based ranking: only high/medium make Best Pick.
	severityChecks := []string{
		"Severity-based ranking",
		"`severity` of `high` or `medium`",
		"`severity=low`",       // demoted to structured-review only
		"2 strong fixes beat 3 fixes where one is filler",
	}
	for _, s := range severityChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing severity rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Mobile-claim handling — defensive skip when limitations contradict.
	mobileChecks := []string{
		"Mobile-claim handling",
		"Skip any `mobile_risk` observation",
		"desktop-only inspection",
	}
	for _, s := range mobileChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing mobile rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// B2B pricing/policy rules with concrete allowed/forbidden phrases.
	pricingChecks := []string{
		"B2B / quote-driven pricing & policy guidance",
		"If this is quote-only, make that explicit",
		"Add MOQ / lead-time / spec-sheet expectations",
		"Move the quote CTA to the product/category decision point",
		"Forbidden phrasings",
		"Publish prices.",
		"Pricing is the biggest issue.",
	}
	for _, s := range pricingChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing pricing/policy rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Reshaped Kova lens — non-handmade shops still get visual-proof
	// guidance, just without the maker-process framing.
	kovaChecks := []string{
		"Kova lens",
		"reshaped",
		"real-use imagery",
		"workers wearing PPE",            // concrete B2B example
		"organic-content reuse",          // channel-reuse angle (renamed from "reusable organic content")
	}
	for _, s := range kovaChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing Kova-lens reshape rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Visual-notes questions[] handling — drop from Best Pick / Shorter.
	if !strings.Contains(prompt, "Visual-notes `questions[]` handling") {
		t.Errorf("review prompt missing visual-notes questions[] handling rule\n--- prompt ---\n%s", prompt)
	}
	if !strings.Contains(prompt, "no \"Open Qs:\" tail blocks") {
		t.Errorf("review prompt missing the explicit ban on Open Qs: tail blocks\n--- prompt ---\n%s", prompt)
	}

	// Display label rename — Structured Review (space) per spec.
	if !strings.Contains(prompt, "displayed as **Structured Review**") {
		t.Errorf("review prompt should reference 'Structured Review' (space, not hyphen)\n--- prompt ---\n%s", prompt)
	}
	if strings.Contains(prompt, "displayed as **Structured-Review**") {
		t.Errorf("review prompt still references the legacy 'Structured-Review' (hyphen) form\n--- prompt ---\n%s", prompt)
	}
}

// SPEC: prose-tightening PR — three new prompt rules. The drafter must
// (1) refuse the checklist wrapper template in Best Pick, (2) gate
// Structured Review behind an explicit OP request for structure, and
// (3) require a channel-reuse line whenever Best Pick recommends visual
// proof. These tests assert the prompt actually carries those rules.

// (1) Best Pick must read as a Reddit comment, not a structured review.
// The "Top fixes by [impact|leverage]" wrapper + numbered fix list is
// the exact pattern the prior runs produced and the spec wants killed.
func TestRenderReviewPromptForbidsChecklistWrapper(t *testing.T) {
	in := sampleReviewInput()
	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	// The prompt should explicitly forbid the checklist wrapper template.
	wrapperBans := []string{
		"FORBIDDEN",                          // strong signal that templates follow
		"Top fixes by impact:",               // exact wrapper phrasing observed in prior runs
		"Top fixes by leverage:",             // and its sibling
		"If you do one thing today:",         // the closer pattern observed
		"numbered fix lists",                 // explicit ban
		"`1) ... 2) ... 3) ...`",             // concrete pattern
		"Reddit comments meander",            // tone instruction reinforcing prose-not-list
		"You are writing a Reddit comment",   // explicit framing
	}
	for _, s := range wrapperBans {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing checklist-wrapper ban %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Anti-tells should also call out the wrapper patterns explicitly so
	// the model sees the rule from two directions.
	antiTellChecks := []string{
		"Numbered fix lists in `Best Pick`",
		"Section-header wrappers in `Best Pick`",
		"\"Top fixes by impact:\"",
		"\"Quick win:\"",
	}
	for _, s := range antiTellChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing anti-tell %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

// (2) Structured Review trigger must be tightened. Currently the slot
// fires almost every review run. The new rule: only fire when OP
// explicitly asks for a STRUCTURED format. Bare "review my site"
// requests don't count.
func TestRenderReviewPromptTightensStructuredReviewTrigger(t *testing.T) {
	in := sampleReviewInput()
	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	triggerChecks := []string{
		"Structured Review trigger",   // dedicated section
		"off by default",              // strong default signal
		"Pros and cons?",              // example trigger phrase
		"Looking for review/criticism", // example NON-trigger
		"Roast my site",               // example NON-trigger
		"2 drafts",                    // default count: best + shorter only
		"second audit",                // failure-mode framing
	}
	for _, s := range triggerChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing structured-review trigger rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Anti-tell coverage too.
	if !strings.Contains(prompt, "Generating `structured-review` when OP didn't explicitly ask for structure") {
		t.Errorf("review prompt missing structured-review anti-tell\n--- prompt ---\n%s", prompt)
	}
}

// (3) When Best Pick recommends visual proof, the comment MUST include
// one explicit channel-reuse sentence. Promotes the rule from soft
// bias (current behavior in two recent runs both missed it) to a
// mandatory line tied to the shop type.
func TestRenderReviewPromptMandatesChannelReuseLine(t *testing.T) {
	in := sampleReviewInput()
	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	channelReuseChecks := []string{
		"Channel-reuse line — MANDATORY",   // dedicated section header
		"non-optional when visual proof is in the fix list",
		"those same visuals double as content for", // the canonical phrasing pattern
		"LinkedIn / case-study posts / sales follow-up", // B2B channel example
		"IG / TikTok",                                   // handmade/boutique channel example
		"Forbidden phrasings",                           // forbidden non-channel phrasings listed
		"That doubles as proof and helps buyers",        // the exact phrasing from a prior run that didn't satisfy the rule
	}
	for _, s := range channelReuseChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing channel-reuse rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Anti-tell coverage.
	if !strings.Contains(prompt, "Recommending visual proof without the channel-reuse line") {
		t.Errorf("review prompt missing channel-reuse anti-tell\n--- prompt ---\n%s", prompt)
	}
}

// First-paragraph placement rule: when --context=kova + non-handmade
// shop type + medium+ visual finding, the visual-proof + channel-reuse
// angle MUST appear in the first paragraph of Best Pick. The earlier
// drafts buried it at paragraph 2 or 3. This is a softer version of
// "must lead as fix #1" — the rule allows the angle to co-lead with
// another fix in the same paragraph.
func TestRenderReviewPromptFirstParagraphPlacementRule(t *testing.T) {
	in := sampleReviewInput()
	prompt, err := RenderReviewPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	placementChecks := []string{
		"First-paragraph placement",                         // section title
		"non-handmade",                                      // shop type condition
		"medium`+ severity finding",                         // severity condition
		"any area",                                          // intentional non-mechanical (not tied to product_images/trust/generic_risk)
		"first paragraph** of `Best Pick`",                  // explicit MUST target
		"Not paragraph 2. Not paragraph 3. Paragraph 1.",    // emphatic placement language
		"pair it with another fix",                          // explicit allowance for co-lead
		"dual-purpose framing",                              // links to channel-reuse rule
	}
	for _, s := range placementChecks {
		if !strings.Contains(prompt, s) {
			t.Errorf("review prompt missing first-paragraph rule %q\n--- prompt ---\n%s", s, prompt)
		}
	}

	// Anti-tell coverage.
	if !strings.Contains(prompt, "Burying the visual-proof + channel-reuse angle in paragraph 2 or later") {
		t.Errorf("review prompt missing first-paragraph anti-tell\n--- prompt ---\n%s", prompt)
	}
}
