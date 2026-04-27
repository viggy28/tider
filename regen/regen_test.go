package regen

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
	return &llm.Response{Content: f.response, InputTokens: 50, OutputTokens: 100}, nil
}

func sampleSnapshot() *types.Snapshot {
	return &types.Snapshot{
		Brief: types.Brief{Title: "Streambed", Summary: "WAL CDC for Postgres."},
		Research: types.Research{
			Sub:   types.Subreddit{Name: "databases", Subscribers: 100000},
			Notes: &types.SubNotes{Tone: "engineering-focused", SelfPromoTolerance: "medium"},
			TopWeek: []types.Post{
				{Title: "Discussion of CDC tools", Score: 200, NumComments: 50},
			},
		},
		Bundle: types.DraftBundle{
			Sub: "databases",
			Drafts: []types.Draft{
				{
					Provider: "openai", Model: "gpt-5", Risk: "low",
					Angles: []types.Angle{
						{
							ID: 1, Premise: "war story", Hook: "what surprised me",
							Titles: []types.Title{
								{ID: "1.1", Text: "OLD title 1"},
								{ID: "1.2", Text: "OLD title 2"},
								{ID: "1.3", Text: "OLD title 3"},
							},
							Bodies: []types.Body{
								{ID: "1.1", Text: "OLD body content one.", Tags: []string{"opener:war-story"}},
								{ID: "1.2", Text: "OLD body content two.", Tags: []string{"opener:question"}},
							},
						},
						{
							ID: 2, Premise: "technical deep-dive", Hook: "the COW gotcha",
							Titles: []types.Title{
								{ID: "2.1", Text: "OLD title 2.1"},
							},
							Bodies: []types.Body{
								{ID: "2.1", Text: "OLD body 2.1.", Tags: []string{"opener:context"}},
							},
						},
					},
					Recommendation: types.Recommendation{AngleID: 1, TitleID: "1.2", BodyID: "1.1"},
					Flair:          types.FlairRec{Suggested: "discussion"},
				},
			},
		},
	}
}

func newTitlesResponse() string {
	return `{
  "titles": [
    {"id": "1.1", "text": "NEW title 1"},
    {"id": "1.2", "text": "NEW title 2"},
    {"id": "1.3", "text": "NEW title 3"}
  ]
}`
}

func TestTitlesReplacesOnlyTargetAngle(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newTitlesResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	bundle, err := Titles(context.Background(), refs, sampleSnapshot(), 1, "punchier")
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]

	// Angle 1 titles should be NEW.
	a1 := d.Angles[0]
	if a1.ID != 1 {
		t.Fatalf("angle order disturbed")
	}
	if len(a1.Titles) != 3 {
		t.Fatalf("titles count wrong: %d", len(a1.Titles))
	}
	for _, ti := range a1.Titles {
		if !strings.HasPrefix(ti.Text, "NEW") {
			t.Errorf("angle 1 title not regenerated: %q", ti.Text)
		}
	}

	// Angle 1 bodies should be UNCHANGED.
	if a1.Bodies[0].Text != "OLD body content one." {
		t.Errorf("angle 1 bodies wrongly mutated: %q", a1.Bodies[0].Text)
	}

	// Angle 2 titles should be UNCHANGED.
	a2 := d.Angles[1]
	if a2.Titles[0].Text != "OLD title 2.1" {
		t.Errorf("angle 2 titles wrongly mutated: %q", a2.Titles[0].Text)
	}

	// Recommendation should not be silently changed.
	if d.Recommendation.AngleID != 1 || d.Recommendation.TitleID != "1.2" {
		t.Errorf("recommendation disturbed: %+v", d.Recommendation)
	}
}

func TestTitlesPromptIncludesContextAndNote(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newTitlesResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	_, err := Titles(context.Background(), refs, sampleSnapshot(), 1, "make them punchier")
	if err != nil {
		t.Fatal(err)
	}
	prompt := p.gotReq.Messages[0].Content
	wantSubstrings := []string{
		"r/databases",            // sub
		"engineering-focused",    // curated notes
		"war story",              // angle premise carried forward
		"what surprised me",      // angle hook carried forward
		"OLD title 1",            // existing titles shown so model knows what to replace
		"make them punchier",     // user's note
		"3 candidate titles",     // count derived from existing
		"\"id\": \"1.1\"",        // output schema with correct angle ID
		"Anti-tells",             // anti-tells preserved
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("regen titles prompt missing %q", s)
		}
	}
	if !p.gotReq.JSONMode {
		t.Error("JSONMode should be true")
	}
}

func TestTitlesSkipsRefusedDrafts(t *testing.T) {
	snap := sampleSnapshot()
	snap.Bundle.Drafts[0].Risk = types.RiskRefuse
	snap.Bundle.Drafts[0].RiskReason = "sub rejects"
	snap.Bundle.Drafts[0].Angles = nil

	p := &fakeProvider{name: "openai", response: newTitlesResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	bundle, err := Titles(context.Background(), refs, snap, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&p.calls) != 0 {
		t.Errorf("refused drafts should not call the provider, got %d calls", p.calls)
	}
	if bundle.Drafts[0].Risk != types.RiskRefuse {
		t.Errorf("refused state lost: %+v", bundle.Drafts[0])
	}
}

func TestTitlesRecordsErrorWhenAngleMissing(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newTitlesResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	bundle, err := Titles(context.Background(), refs, sampleSnapshot(), 99, "")
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]
	if d.Error == "" || !strings.Contains(d.Error, "angle 99 not in this draft") {
		t.Errorf("expected missing-angle error, got %q", d.Error)
	}
	if atomic.LoadInt32(&p.calls) != 0 {
		t.Errorf("provider should not be called on missing angle, got %d", p.calls)
	}
}

func TestTitlesRecordsErrorWhenProviderNotInRefs(t *testing.T) {
	p := &fakeProvider{name: "anthropic", response: newTitlesResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "claude-sonnet-4-7"}}
	// snapshot has openai draft; refs only has anthropic — provider mismatch.
	bundle, err := Titles(context.Background(), refs, sampleSnapshot(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.Drafts[0].Error, "provider \"openai\" not in --providers list") {
		t.Errorf("expected provider-not-in-refs error, got %q", bundle.Drafts[0].Error)
	}
}

func TestTitlesPropagatesProviderError(t *testing.T) {
	p := &fakeProvider{name: "openai", err: errors.New("rate limited")}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	bundle, err := Titles(context.Background(), refs, sampleSnapshot(), 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.Drafts[0].Error, "rate limited") {
		t.Errorf("provider error not recorded: %q", bundle.Drafts[0].Error)
	}
	// Original titles should still be in place — failed regen doesn't wipe them.
	if bundle.Drafts[0].Angles[0].Titles[0].Text != "OLD title 1" {
		t.Errorf("failed regen mutated titles: %q", bundle.Drafts[0].Angles[0].Titles[0].Text)
	}
}

func TestTitlesRequiresProviders(t *testing.T) {
	_, err := Titles(context.Background(), nil, sampleSnapshot(), 1, "")
	if err == nil {
		t.Fatal("expected error for empty providers")
	}
}

func newBodyResponse() string {
	return `{
  "body": {
    "id": "1.1",
    "text": "NEW body content rewritten with the user's guidance applied.",
    "tags": ["opener:war-story", "close:invite-stories"]
  }
}`
}

func TestBodyReplacesOnlyTargetBody(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newBodyResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	bundle, err := Body(context.Background(), refs, sampleSnapshot(), "1.1", "more provocative", 200)
	if err != nil {
		t.Fatal(err)
	}
	d := bundle.Drafts[0]

	// Body 1.1 should be NEW
	a1 := d.Angles[0]
	if a1.Bodies[0].ID != "1.1" {
		t.Fatalf("body order disturbed")
	}
	if !strings.HasPrefix(a1.Bodies[0].Text, "NEW") {
		t.Errorf("body 1.1 not regenerated: %q", a1.Bodies[0].Text)
	}
	if len(a1.Bodies[0].Tags) != 2 || a1.Bodies[0].Tags[0] != "opener:war-story" {
		t.Errorf("body tags not updated: %+v", a1.Bodies[0].Tags)
	}

	// Body 1.2 should be UNCHANGED
	if a1.Bodies[1].Text != "OLD body content two." {
		t.Errorf("body 1.2 wrongly mutated: %q", a1.Bodies[1].Text)
	}

	// Titles for angle 1 should be UNCHANGED
	if a1.Titles[0].Text != "OLD title 1" {
		t.Errorf("angle 1 titles wrongly mutated")
	}

	// Angle 2 entirely UNCHANGED
	if d.Angles[1].Bodies[0].Text != "OLD body 2.1." {
		t.Errorf("angle 2 wrongly mutated: %q", d.Angles[1].Bodies[0].Text)
	}
}

func TestBodyForcesIDIntoResponse(t *testing.T) {
	// Model returns a different id ("9.9") — regen should force it back to "1.1".
	resp := `{"body":{"id":"9.9","text":"NEW body","tags":[]}}`
	p := &fakeProvider{name: "openai", response: resp}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	bundle, err := Body(context.Background(), refs, sampleSnapshot(), "1.1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	body := bundle.Drafts[0].Angles[0].Bodies[0]
	if body.ID != "1.1" {
		t.Errorf("expected ID forced to 1.1, got %q", body.ID)
	}
	if !strings.HasPrefix(body.Text, "NEW") {
		t.Errorf("text not updated: %q", body.Text)
	}
}

func TestBodyPromptIncludesGuidanceAndLength(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newBodyResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	_, err := Body(context.Background(), refs, sampleSnapshot(), "1.1", "lighter on tradeoffs", 150)
	if err != nil {
		t.Fatal(err)
	}
	prompt := p.gotReq.Messages[0].Content
	wantSubstrings := []string{
		"r/databases",
		"war story",                  // angle premise
		"what surprised me",          // angle hook
		"OLD title 1",                // title carried forward
		"OLD body content one.",      // existing body shown for replacement
		"opener:war-story",           // existing tag carried forward
		"lighter on tradeoffs",       // user's note
		"~150 words",                 // length hint
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("regen body prompt missing %q", s)
		}
	}
}

func TestBodyOmitsLengthWhenZero(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newBodyResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}
	_, err := Body(context.Background(), refs, sampleSnapshot(), "1.1", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	prompt := p.gotReq.Messages[0].Content
	if strings.Contains(prompt, "Target length") {
		t.Errorf("length hint should be omitted when 0")
	}
}

func TestBodyRecordsErrorWhenBodyMissing(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newBodyResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}
	bundle, err := Body(context.Background(), refs, sampleSnapshot(), "1.99", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(bundle.Drafts[0].Error, "body 1.99 not in this draft") {
		t.Errorf("expected missing-body error, got %q", bundle.Drafts[0].Error)
	}
}

func TestBodyMalformedVariantID(t *testing.T) {
	p := &fakeProvider{name: "openai", response: newBodyResponse()}
	refs := []llm.ProviderRef{{Provider: p, Model: "gpt-5"}}

	cases := []string{"", "abc", "1.", ".1", "noformat"}
	for _, id := range cases {
		_, err := Body(context.Background(), refs, sampleSnapshot(), id, "", 0)
		if err == nil || !strings.Contains(err.Error(), "bad") {
			t.Errorf("expected error for variant id %q, got %v", id, err)
		}
	}
}

func TestSplitVariantID(t *testing.T) {
	cases := []struct {
		in        string
		wantAngle int
		wantOK    bool
	}{
		{"1.1", 1, true},
		{"2.3", 2, true},
		{"10.1", 10, true},
		{"1.", 0, false},
		{".1", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		a, _, err := splitVariantID(c.in)
		ok := err == nil
		if ok != c.wantOK {
			t.Errorf("splitVariantID(%q) ok = %v, want %v (err: %v)", c.in, ok, c.wantOK, err)
		}
		if ok && a != c.wantAngle {
			t.Errorf("splitVariantID(%q) angle = %d, want %d", c.in, a, c.wantAngle)
		}
	}
}

func TestTitlesFanOutConcurrent(t *testing.T) {
	a := &fakeProvider{name: "anthropic", response: newTitlesResponse(), delay: 50 * time.Millisecond}
	o := &fakeProvider{name: "openai", response: newTitlesResponse(), delay: 50 * time.Millisecond}
	refs := []llm.ProviderRef{
		{Provider: a, Model: "claude-sonnet-4-7"},
		{Provider: o, Model: "gpt-5"},
	}
	snap := sampleSnapshot()
	// Add a second draft so we have one per provider.
	snap.Bundle.Drafts = append(snap.Bundle.Drafts, types.Draft{
		Provider: "anthropic", Model: "claude-sonnet-4-7", Risk: "low",
		Angles: []types.Angle{
			{ID: 1, Premise: "p", Hook: "h", Titles: []types.Title{{ID: "1.1", Text: "x"}}, Bodies: []types.Body{{ID: "1.1", Text: "y"}}},
		},
	})

	start := time.Now()
	_, err := Titles(context.Background(), refs, snap, 1, "")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 90*time.Millisecond {
		t.Errorf("fan-out not concurrent: elapsed = %v (want < 90ms; sequential would be >= 100ms)", elapsed)
	}
}
