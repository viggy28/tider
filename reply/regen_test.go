package reply

import (
	"context"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/types"
)

const goodRegenResp = `{
  "drafts": [
    {"id":"best","label":"best","text":"Sharper pass guided by note","reasoning":"engages the buffyferry comment"},
    {"id":"shorter","label":"shorter","text":"Shorter version","reasoning":"tightens best"},
    {"id":"question","label":"question","text":"Quick clarifier question?","reasoning":"unblocks better advice"}
  ],
  "pick_id": "best"
}`

func sampleRegenInput() *RegenInput {
	return &RegenInput{
		Thread:        sampleThread(),
		Mode:          &types.ReplyModeResult{Mode: types.ReplyModeReply},
		Contexts:      []types.LoadedReplyContext{{ID: "kova", Source: "bank", Body: "kova thesis snippet"}},
		AuthorContext: "experienced founder voice",
		PreviousDrafts: []types.ReplyDraft{
			{ID: "best", Label: "best", Text: "the original best draft", Reasoning: "original angle"},
			{ID: "shorter", Label: "shorter", Text: "the original shorter", Reasoning: "tighten"},
		},
		Note: "shorter, no kova mention",
	}
}

func TestGenerateReplyRegenSuccess(t *testing.T) {
	p := &fakeProvider{name: "fake", response: goodRegenResp}

	bundle, err := GenerateReplyRegen(context.Background(), p, "gpt-5", sampleRegenInput(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(bundle.Drafts) != 3 {
		t.Errorf("expected 3 drafts, got %d", len(bundle.Drafts))
	}
	if bundle.PickID != "best" {
		t.Errorf("pick_id = %q, want best", bundle.PickID)
	}
	if bundle.Mode != types.ReplyModeReply {
		t.Errorf("mode = %s, want reply", bundle.Mode)
	}
	wantIDs := map[string]bool{"best": false, "shorter": false, "question": false}
	for _, d := range bundle.Drafts {
		if _, ok := wantIDs[d.ID]; !ok {
			t.Errorf("unexpected draft id %q (regen v1 spec is best/shorter/question)", d.ID)
			continue
		}
		wantIDs[d.ID] = true
	}
	for id, seen := range wantIDs {
		if !seen {
			t.Errorf("missing required draft id %q in regen output", id)
		}
	}
}

func TestGenerateReplyRegenRejectsEmptyNote(t *testing.T) {
	p := &fakeProvider{name: "fake", response: goodRegenResp}

	in := sampleRegenInput()
	in.Note = ""
	_, err := GenerateReplyRegen(context.Background(), p, "gpt-5", in, 0)
	if err == nil {
		t.Fatal("expected error for empty note")
	}
	if !strings.Contains(err.Error(), "--note is required") {
		t.Errorf("expected --note required error, got %v", err)
	}

	in.Note = "   \t\n  "
	_, err = GenerateReplyRegen(context.Background(), p, "gpt-5", in, 0)
	if err == nil {
		t.Fatal("expected error for whitespace-only note")
	}
}

func TestGenerateReplyRegenRejectsReviewMode(t *testing.T) {
	// Review-mode sessions are explicitly out of v1 — different input
	// shape (visual notes/inspection), separate code path. Surface the
	// sentinel error so the CLI can format the user-facing message
	// from the issue spec.
	p := &fakeProvider{name: "fake", response: goodRegenResp}
	in := sampleRegenInput()
	in.Mode = &types.ReplyModeResult{Mode: types.ReplyModeReview}

	_, err := GenerateReplyRegen(context.Background(), p, "gpt-5", in, 0)
	if err == nil {
		t.Fatal("expected error for review-mode input")
	}
	if err != ErrRegenReviewModeUnsupported {
		t.Errorf("expected ErrRegenReviewModeUnsupported, got %v", err)
	}
}

func TestRenderRegenPromptIncludesNoteAndPrecedence(t *testing.T) {
	in := sampleRegenInput()
	in.Note = "Reply to Buffyferry's last comment, mention Kova transparently"

	got, err := RenderRegenPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	// The operator note must appear verbatim and prominently — it's
	// the highest product-level priority signal.
	if !strings.Contains(got, in.Note) {
		t.Errorf("rendered prompt missing operator note %q", in.Note)
	}

	// The precedence statement is the load-bearing instruction that
	// tells the model the note overrides defaults including no-pitch.
	for _, want := range []string{
		"highest product-level priority",
		"override",
		"impersonation",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing precedence cue %q", want)
		}
	}

	// Three-variant contract is explicit so the model can't drop one.
	for _, id := range []string{"best", "shorter", "question"} {
		if !strings.Contains(got, "`"+id+"`") {
			t.Errorf("rendered prompt missing variant id %q", id)
		}
	}
}

func TestRenderRegenPromptIncludesPreviousDrafts(t *testing.T) {
	in := sampleRegenInput()

	got, err := RenderRegenPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	// Both prior drafts should be visible to the model — the regen
	// runs against the original drafts.json, not against prior regens
	// (per issue spec C). Verify both texts appear.
	for _, want := range []string{"the original best draft", "the original shorter"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered prompt missing previous draft text %q", want)
		}
	}
}

func TestRenderRegenPromptKovaPermissionVisible(t *testing.T) {
	// When the operator note explicitly asks to mention Kova, the
	// rendered prompt must carry permission language strong enough to
	// override the default no-pitch behavior. Verifies the template's
	// "if --note explicitly says mention Kova..." sentence is present
	// regardless of whether THIS run's note mentions Kova.
	got, err := RenderRegenPrompt(sampleRegenInput())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Kova") {
		t.Errorf("rendered prompt should reference Kova permission cue")
	}
	if !strings.Contains(strings.ToLower(got), "transparently") {
		t.Errorf("rendered prompt should require transparent mention when note asks")
	}
}

func TestRenderRegenPromptOverridesNoPitch(t *testing.T) {
	// Test plan item: --note="mention Kova" must appear in the prompt
	// AND the precedence model must say it overrides default no-pitch
	// behavior. Verify both halves explicitly so the template can't
	// silently lose the override sentence.
	in := sampleRegenInput()
	in.Note = "mention Kova"

	got, err := RenderRegenPrompt(in)
	if err != nil {
		t.Fatal(err)
	}

	// 1. The operator's literal note text reaches the model.
	if !strings.Contains(got, "mention Kova") {
		t.Errorf("rendered prompt missing literal note text")
	}

	// 2. The precedence statement explicitly says the operator note
	//    overrides default no-pitch behavior, not just generic style.
	//    "no-pitch" is the load-bearing phrase the issue spec calls
	//    out — verify it survived template edits.
	low := strings.ToLower(got)
	if !strings.Contains(low, "no-pitch") {
		t.Errorf("rendered prompt missing 'no-pitch' override language")
	}
	if !strings.Contains(low, "override") {
		t.Errorf("rendered prompt missing 'override' precedence verb")
	}
}

func TestGenerateReplyRegenJSONParseError(t *testing.T) {
	p := &fakeProvider{name: "fake", response: "not json at all"}

	_, err := GenerateReplyRegen(context.Background(), p, "gpt-5", sampleRegenInput(), 0)
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
	if !strings.Contains(err.Error(), "parse json") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestGenerateReplyRegenEmptyDrafts(t *testing.T) {
	p := &fakeProvider{name: "fake", response: `{"drafts":[],"pick_id":""}`}

	_, err := GenerateReplyRegen(context.Background(), p, "gpt-5", sampleRegenInput(), 0)
	if err == nil {
		t.Fatal("expected error for empty drafts")
	}
	if !strings.Contains(err.Error(), "no drafts returned") {
		t.Errorf("expected no-drafts error, got %v", err)
	}
}

func TestGenerateReplyRegenSentRequestShape(t *testing.T) {
	// Regen must use JSON mode and the operator note must reach the
	// model. Belt-and-suspenders for the wiring.
	p := &fakeProvider{name: "fake", response: goodRegenResp}
	in := sampleRegenInput()
	in.Note = "make it warmer and ask one specific question"

	if _, err := GenerateReplyRegen(context.Background(), p, "gpt-5", in, 0); err != nil {
		t.Fatal(err)
	}
	if !p.gotReq.JSONMode {
		t.Error("regen request should set JSONMode=true")
	}
	if len(p.gotReq.Messages) == 0 || !strings.Contains(p.gotReq.Messages[0].Content, in.Note) {
		t.Errorf("regen prompt did not include operator note")
	}
}
