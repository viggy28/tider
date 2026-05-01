package reply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
)

// fakeProvider is shared with drafter_test.go via package compilation.
// Reuse here for symmetry. If the shared fake ever moves, this comment
// catches the dependency.

func sampleVisualInput(t *testing.T) *VisualInput {
	t.Helper()
	tmp := t.TempDir()
	shotPath := filepath.Join(tmp, "shot.png")
	if err := os.WriteFile(shotPath, []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &VisualInput{
		Inspection: &types.Inspection{
			URL:             "https://shop.example/",
			Title:           "Handmade Ceramic Bowls",
			MetaDescription: "Wheel-thrown ceramics from a one-person studio.",
			ScreenshotPath:  shotPath,
			Headings: []types.Heading{
				{Level: 1, Text: "Welcome to the studio"},
				{Level: 2, Text: "Latest pieces"},
			},
		},
		ImageURLs: []string{
			"https://shop.example/img/bowl-1.jpg",
			"https://shop.example/img/bowl-2.jpg",
		},
	}
}

const handmadeVisualResp = `{
  "shop_type": "handmade",
  "summary": "Solo ceramicist; warm but generic photography.",
  "observations": [
    {"area":"product_images","finding":"hero shot lacks scale reference","evidence":"first three images are top-down crops with no hand or object for scale","severity":"medium","recommendation":"add a hand-held shot to one of the first 2 photos"}
  ],
  "kova_signals": ["no in-hand or scale shot in the first 2 product images","texture is invisible at the photo crops shown"],
  "questions": ["are these all single pieces or a series?"],
  "limitations": ["screenshot only covers homepage; no product detail page inspected"]
}`

func TestAnalyzeVisualHappyPath(t *testing.T) {
	p := &fakeProvider{name: "fake", response: handmadeVisualResp}
	notes, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if notes.ShopType != "handmade" {
		t.Errorf("ShopType = %q", notes.ShopType)
	}
	if len(notes.KovaSignals) != 2 {
		t.Errorf("expected 2 kova signals (handmade gates allow them), got %d", len(notes.KovaSignals))
	}
	if len(notes.Observations) != 1 {
		t.Errorf("Observations = %d", len(notes.Observations))
	}
	if !p.gotReq.JSONMode {
		t.Error("visual analyzer should set JSONMode")
	}
	// Screenshot must have been attached as a local Path image.
	if len(p.gotReq.Images) < 1 {
		t.Fatal("expected at least one image in the request")
	}
	if p.gotReq.Images[0].Path == "" {
		t.Error("first image (screenshot) should be passed by Path, not URL")
	}
	// Product images attach as URL refs.
	urlCount := 0
	for _, img := range p.gotReq.Images[1:] {
		if img.URL != "" && img.Path == "" {
			urlCount++
		}
	}
	if urlCount != 2 {
		t.Errorf("expected 2 URL-referenced product images, got %d", urlCount)
	}
}

// KovaSignals must be filtered out when the analyzer returns a shop_type
// outside {handmade, boutique}, regardless of what the model says — the
// Go side is belt-and-suspenders against prompt-rule violations. The
// PND Industrial Suppliers regression target.
func TestAnalyzeVisualGatesKovaSignalsForB2B(t *testing.T) {
	const b2bResp = `{
  "shop_type": "b2b_industrial",
  "summary": "industrial supplier, quote-driven",
  "observations": [],
  "kova_signals": ["model violated the prompt by populating this anyway"],
  "questions": [],
  "limitations": []
}`
	p := &fakeProvider{name: "fake", response: b2bResp}
	notes, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if notes.ShopType != "b2b_industrial" {
		t.Errorf("ShopType = %q", notes.ShopType)
	}
	if len(notes.KovaSignals) != 0 {
		t.Errorf("KovaSignals must be empty for b2b_industrial (regression target), got %v", notes.KovaSignals)
	}
}

// Same for SaaS, dropship, services, portfolio, unclear — none should
// have KovaSignals leak through.
func TestAnalyzeVisualGatesKovaSignalsForAllNonHandmadeTypes(t *testing.T) {
	for _, st := range []string{"dropship", "saas", "services", "portfolio", "unclear"} {
		t.Run(st, func(t *testing.T) {
			resp := `{"shop_type":"` + st + `","summary":"x","observations":[],"kova_signals":["leak"],"questions":[],"limitations":[]}`
			p := &fakeProvider{name: "fake", response: resp}
			notes, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(notes.KovaSignals) != 0 {
				t.Errorf("ShopType=%s: KovaSignals must be empty, got %v", st, notes.KovaSignals)
			}
		})
	}
}

// Boutique counts as Kova-adjacent — KovaSignals should pass through.
func TestAnalyzeVisualKeepsKovaSignalsForBoutique(t *testing.T) {
	const resp = `{"shop_type":"boutique","summary":"x","observations":[],"kova_signals":["keep this"],"questions":[],"limitations":[]}`
	p := &fakeProvider{name: "fake", response: resp}
	notes, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.KovaSignals) != 1 {
		t.Errorf("boutique should keep kova_signals, got %v", notes.KovaSignals)
	}
}

func TestAnalyzeVisualMissingShopTypeErrors(t *testing.T) {
	const resp = `{"shop_type":"","summary":"x","observations":[]}`
	p := &fakeProvider{name: "fake", response: resp}
	_, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
	if err == nil || !strings.Contains(err.Error(), "shop_type") {
		t.Errorf("expected missing-shop_type error, got %v", err)
	}
}

func TestAnalyzeVisualMissingScreenshotErrors(t *testing.T) {
	input := sampleVisualInput(t)
	input.Inspection.ScreenshotPath = ""
	p := &fakeProvider{name: "fake", response: handmadeVisualResp}
	_, err := AnalyzeVisual(context.Background(), p, "gpt-4o", input, 0)
	if err == nil || !strings.Contains(err.Error(), "ScreenshotPath required") {
		t.Errorf("expected screenshot-required error, got %v", err)
	}
}

func TestAnalyzeVisualBadJSONErrors(t *testing.T) {
	p := &fakeProvider{name: "fake", response: "not json"}
	_, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
	if err == nil || !strings.Contains(err.Error(), "parse json") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestAnalyzeVisualProviderErrorPropagated(t *testing.T) {
	p := &fakeProvider{name: "fake", err: errors.New("rate limited")}
	_, err := AnalyzeVisual(context.Background(), p, "gpt-4o", sampleVisualInput(t), 0)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected provider error, got %v", err)
	}
}

func TestAnalyzeVisualNilInputErrors(t *testing.T) {
	p := &fakeProvider{name: "fake", response: handmadeVisualResp}
	_, err := AnalyzeVisual(context.Background(), p, "gpt-4o", nil, 0)
	if err == nil {
		t.Error("nil input should error")
	}
}

// Prompt rendering: make sure the visual prompt includes target URL,
// title, headings, and context references, and lists the shop_type
// classes. Done as a smoke test, not a per-line lock.
func TestRenderVisualPromptIncludesKeyElements(t *testing.T) {
	input := sampleVisualInput(t)
	input.Contexts = []types.LoadedReplyContext{
		{ID: "kova", Source: "bank", Body: "kova body"},
	}
	prompt, err := RenderVisualPrompt(input)
	if err != nil {
		t.Fatal(err)
	}
	checks := []string{
		"https://shop.example/",
		"Handmade Ceramic Bowls",
		"(h1) Welcome to the studio",
		"From bank (kova)",
		"kova body",
		"shop_type",
		"handmade",
		"b2b_industrial",
		"kova_signals",
		"do NOT pitch",     // anti-pitch rule
		"Do NOT name the project", // explicit Kova rename ban
		// SPEC_REVIEW_DRAFT_REFINEMENT.md tightens mobile_risk severity:
		// from a desktop-only screenshot, mobile claims are inferred at
		// best, so the analyzer must cap mobile_risk severity at medium
		// and add a corresponding limitations note. This protects the
		// downstream review drafter from getting "high"-severity mobile
		// findings the user shouldn't surface as a top fix.
		"Mobile-risk severity cap",
		"Cap their severity at `medium`",
	}
	for _, s := range checks {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing %q\n--- prompt ---\n%s", s, prompt)
		}
	}
}

// pin llm import to ensure visibility — the visual analyzer is the
// first non-test code in this package that uses llm.Request.Images, so
// catching shape mismatches here is useful.
var _ = llm.Request{}
