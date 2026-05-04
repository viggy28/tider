package draft

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
)

// fakeProvider returns a canned response and records the request it saw.
type fakeProvider struct {
	name     string
	response string
	err      error
	delay    time.Duration
	calls    int32
	gotReq   llm.Request
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	atomic.AddInt32(&f.calls, 1)
	f.gotReq = req
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.response, InputTokens: 100, OutputTokens: 200}, nil
}

const cannedDraftJSON = `{
  "risk": "low",
  "angles": [
    {
      "id": 1,
      "premise": "war story about WAL ordering",
      "hook": "what surprised me about logical replication",
      "titles": [
        {"id": "1.1", "text": "WAL ordering bit me; here's what I missed"},
        {"id": "1.2", "text": "Logical replication: the gotcha I wish I'd known"},
        {"id": "1.3", "text": "Three weeks lost to one WAL ordering assumption"}
      ],
      "bodies": [
        {"id": "1.1", "text": "Spent two sprints assuming...", "tags": ["opener:war-story", "close:invite-stories"]},
        {"id": "1.2", "text": "Quick one for folks running logical replication...", "tags": ["opener:question"]}
      ]
    },
    {
      "id": 2,
      "premise": "technical comparison vs Debezium",
      "hook": "what we got from going single-binary",
      "titles": [
        {"id": "2.1", "text": "Single-binary CDC: tradeoffs vs Debezium"},
        {"id": "2.2", "text": "Why we built our own CDC instead of using Debezium"},
        {"id": "2.3", "text": "Debezium served us well; here's why we replaced it"}
      ],
      "bodies": [
        {"id": "2.1", "text": "We ran Debezium for 18 months...", "tags": ["opener:context", "hook:contrarian"]},
        {"id": "2.2", "text": "Cards on the table — Debezium does X well...", "tags": ["opener:disclosure"]}
      ]
    }
  ],
  "recommendation": {
    "angle_id": 1,
    "title_id": "1.3",
    "body_id": "1.1",
    "reasoning": "war stories outperform feature pitches in this sub"
  },
  "flair": {"required": true, "suggested": "discussion"},
  "suggested_window": "Tue-Thu 9-11am ET",
  "media_recommendation": ""
}`

func sampleBrief() types.Brief {
	return types.Brief{
		Source:     types.BriefSource{Mode: "url", Value: "https://example.com"},
		Title:      "Streambed",
		Summary:    "WAL-native CDC for Postgres.",
		Highlights: []string{"Single binary", "Postgres wire protocol"},
		Audience:   "Postgres teams",
		CreatedAt:  time.Now().UTC(),
	}
}

func sampleResearch() types.Research {
	return types.Research{
		Sub:   types.Subreddit{Name: "golang", Subscribers: 250000},
		Notes: &types.SubNotes{Name: "golang", Tone: "terse, hype-allergic", SelfPromoTolerance: "low"},
		Rules: []types.Rule{{ShortName: "No spam", Description: "do not spam"}},
		TopWeek: []types.Post{
			{Title: "Some discussion post", Score: 500, NumComments: 80, LinkFlairText: "discussion"},
		},
		Hot: []types.Post{
			{Title: "What's hot now", Score: 200, NumComments: 30},
		},
		Generated: time.Now().UTC(),
	}
}

// sampleInput wraps the sample Brief/Research with default opts. Tests
// override fields (Opts.AuthorContext, OperatorNote, Contexts, etc.)
// after constructing.
func sampleInput() Input {
	return Input{
		Brief:    sampleBrief(),
		Research: sampleResearch(),
		Opts:     Default(),
	}
}

func TestRenderPromptIncludesContextAndCounts(t *testing.T) {
	prompt, err := RenderPrompt(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	wantSubstrings := []string{
		"r/golang",               // sub header
		"Some discussion post",   // top post leaked in
		"terse, hype-allergic",   // curated notes leaked in
		"Streambed",              // brief title
		"Postgres wire protocol", // highlight
		"2 *distinct* angles",    // variant count
		"3 candidate titles",     // titles per angle
		"2 candidate bodies",     // bodies per angle
		"Anti-tells",             // anti-tells section present
		"\"risk\":",              // output schema present
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestRenderPromptIncludesAuthorContextWhenSet(t *testing.T) {
	in := sampleInput()
	in.Opts.AuthorContext = "Five years at Cloudflare leading the Postgres team. Currently building Streambed."
	prompt, err := RenderPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Who you're writing as") {
		t.Error("author context section heading missing")
	}
	if !strings.Contains(prompt, "Five years at Cloudflare") {
		t.Error("author context body not embedded")
	}
	if !strings.Contains(prompt, "ground them in this background") {
		t.Error("author context guidance to LLM missing")
	}
}

func TestRenderPromptOmitsAuthorContextWhenEmpty(t *testing.T) {
	prompt, err := RenderPrompt(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "Who you're writing as") {
		t.Errorf("author context section should not render when AuthorContext empty")
	}
}

func TestRenderPromptIncludesOperatorNoteAndContexts(t *testing.T) {
	in := sampleInput()
	in.OperatorNote = "Ask sellers about AI listing images. Don't pitch Kova."
	in.Contexts = []types.LoadedReplyContext{
		{ID: "kova", Source: "bank", Body: "Kova is a CDC tool for Postgres."},
		{ID: "personal", Source: "path", Path: "/home/user/.tider/personal.md", Body: "Five years building infra at Cloudflare."},
	}

	prompt, err := RenderPrompt(in)
	if err != nil {
		t.Fatal(err)
	}
	wantSubstrings := []string{
		"Operator intent",
		"Ask sellers about AI listing images",
		"Background context",
		"context: kova",
		"Kova is a CDC tool",
		"context: personal",
		"/home/user/.tider/personal.md",
		"Five years building infra",
		"Precedence",
		"Operator intent",
		"Background context",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q", s)
		}
	}
}

func TestRenderPromptOmitsOperatorAndContextSectionsWhenAbsent(t *testing.T) {
	prompt, err := RenderPrompt(sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	// The H1 section headers should not render when the inputs are
	// empty. (The precedence list still mentions them inline by name —
	// that's intentional, it's the rule.)
	for _, header := range []string{"# Operator intent", "# Background context"} {
		if strings.Contains(prompt, header) {
			t.Errorf("prompt should not contain section header %q when input absent", header)
		}
	}
	if !strings.Contains(prompt, "# Precedence") {
		t.Error("Precedence section should always render")
	}
}

func TestRenderPromptRejectsZeroCounts(t *testing.T) {
	in := sampleInput()
	in.Opts = Options{AngleCount: 0, TitlesPerAngle: 3, BodiesPerAngle: 2}
	_, err := RenderPrompt(in)
	if err == nil {
		t.Fatal("expected error for zero AngleCount")
	}
}

func TestGenerateFanOutAcrossProviders(t *testing.T) {
	a := &fakeProvider{name: "anthropic", response: cannedDraftJSON, delay: 50 * time.Millisecond}
	b := &fakeProvider{name: "openai", response: cannedDraftJSON, delay: 50 * time.Millisecond}

	start := time.Now()
	bundle, err := Generate(context.Background(), []ProviderRef{
		{Provider: a, Model: "claude-sonnet-4-7"},
		{Provider: b, Model: "gpt-5"},
	}, sampleInput())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	// Sequential would be >= 100ms. Concurrent should land closer to 50ms.
	// Allow some slack for test machine variability but require it's clearly under sequential.
	if elapsed > 90*time.Millisecond {
		t.Errorf("fan-out not concurrent: elapsed = %v (want < 90ms)", elapsed)
	}

	if bundle.Sub != "golang" {
		t.Errorf("sub = %q", bundle.Sub)
	}
	if len(bundle.Drafts) != 2 {
		t.Fatalf("drafts len = %d", len(bundle.Drafts))
	}
	gotProviders := map[string]string{}
	for _, d := range bundle.Drafts {
		gotProviders[d.Provider] = d.Model
		if d.Error != "" {
			t.Errorf("draft from %s errored: %s", d.Provider, d.Error)
		}
		if d.Risk != "low" {
			t.Errorf("risk = %q (parse failed?)", d.Risk)
		}
		if len(d.Angles) != 2 {
			t.Errorf("angles = %d", len(d.Angles))
		}
	}
	if gotProviders["anthropic"] != "claude-sonnet-4-7" {
		t.Errorf("anthropic model not preserved: %q", gotProviders["anthropic"])
	}
	if gotProviders["openai"] != "gpt-5" {
		t.Errorf("openai model not preserved: %q", gotProviders["openai"])
	}
}

func TestGenerateRequiresAtLeastOneProvider(t *testing.T) {
	_, err := Generate(context.Background(), nil, sampleInput())
	if err == nil {
		t.Fatal("expected error for empty providers")
	}
}

func TestGenerateRecordsPerProviderError(t *testing.T) {
	good := &fakeProvider{name: "anthropic", response: cannedDraftJSON}
	bad := &fakeProvider{name: "openai", err: errors.New("rate limited")}

	bundle, err := Generate(context.Background(), []ProviderRef{
		{Provider: good, Model: "claude-sonnet-4-7"},
		{Provider: bad, Model: "gpt-5"},
	}, sampleInput())
	if err != nil {
		t.Fatalf("bundle should succeed even when one provider fails: %v", err)
	}
	if len(bundle.Drafts) != 2 {
		t.Fatalf("drafts len = %d", len(bundle.Drafts))
	}
	var goodDraft, badDraft types.Draft
	for _, d := range bundle.Drafts {
		switch d.Provider {
		case "anthropic":
			goodDraft = d
		case "openai":
			badDraft = d
		}
	}
	if goodDraft.Error != "" {
		t.Errorf("good provider got error: %s", goodDraft.Error)
	}
	if badDraft.Error == "" || !strings.Contains(badDraft.Error, "rate limited") {
		t.Errorf("bad provider error not recorded: %q", badDraft.Error)
	}
	if len(badDraft.Angles) != 0 {
		t.Errorf("failed draft should not have angles")
	}
}

func TestGenerateBadJSONRecordedAsError(t *testing.T) {
	p := &fakeProvider{name: "anthropic", response: "not json at all"}
	bundle, err := Generate(context.Background(), []ProviderRef{{Provider: p, Model: "x"}}, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]
	if d.Error == "" || !strings.Contains(d.Error, "parse draft json") {
		t.Errorf("expected parse error, got %q", d.Error)
	}
}

func TestGenerateRefusePath(t *testing.T) {
	const refuseJSON = `{"risk":"refuse","risk_reason":"this sub rejects launch posts","angles":[]}`
	p := &fakeProvider{name: "anthropic", response: refuseJSON}
	bundle, err := Generate(context.Background(), []ProviderRef{{Provider: p, Model: "x"}}, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]
	if d.Risk != types.RiskRefuse {
		t.Errorf("risk = %q", d.Risk)
	}
	if d.RiskReason != "this sub rejects launch posts" {
		t.Errorf("risk reason = %q", d.RiskReason)
	}
	if len(d.Angles) != 0 {
		t.Errorf("refuse should have no angles, got %d", len(d.Angles))
	}
}

func TestGenerateMissingRiskFieldErrors(t *testing.T) {
	p := &fakeProvider{name: "anthropic", response: `{"angles":[]}`}
	bundle, err := Generate(context.Background(), []ProviderRef{{Provider: p, Model: "x"}}, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]
	if d.Error == "" || !strings.Contains(d.Error, "risk") {
		t.Errorf("expected missing-risk error, got %q", d.Error)
	}
}

func TestGenerateSendsJSONModeRequest(t *testing.T) {
	p := &fakeProvider{name: "anthropic", response: cannedDraftJSON}
	_, err := Generate(context.Background(), []ProviderRef{{Provider: p, Model: "claude-sonnet-4-7"}}, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	if !p.gotReq.JSONMode {
		t.Error("JSONMode should be true for draft generation")
	}
	if p.gotReq.MaxTokens != Default().MaxTokens {
		t.Errorf("MaxTokens = %d, want %d", p.gotReq.MaxTokens, Default().MaxTokens)
	}
	if p.gotReq.Model != "claude-sonnet-4-7" {
		t.Errorf("Model = %q (should pass through ref.Model)", p.gotReq.Model)
	}
	if len(p.gotReq.Messages) != 1 || p.gotReq.Messages[0].Role != llm.RoleUser {
		t.Errorf("messages = %+v", p.gotReq.Messages)
	}
}

func TestTokenUsageRecorded(t *testing.T) {
	p := &fakeProvider{name: "anthropic", response: cannedDraftJSON}
	bundle, err := Generate(context.Background(), []ProviderRef{{Provider: p, Model: "x"}}, sampleInput())
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]
	if d.InputTokens != 100 || d.OutputTokens != 200 {
		t.Errorf("token usage not recorded: in=%d out=%d", d.InputTokens, d.OutputTokens)
	}
}
