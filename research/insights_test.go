package research

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
)

type fakeInsightProvider struct {
	response string
	gotReq   llm.Request
	calls    int32
}

func (f *fakeInsightProvider) Name() string { return "openai" }

func (f *fakeInsightProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	atomic.AddInt32(&f.calls, 1)
	f.gotReq = req
	return &llm.Response{Content: f.response, InputTokens: 11, OutputTokens: 22}, nil
}

func sampleInsightResearch() types.Research {
	return types.Research{
		Sub: types.Subreddit{Name: "woocommerce", Subscribers: 47460},
		Rules: []types.Rule{{
			ShortName:   "No promotional material",
			Description: "No promotional posts.",
		}},
		TopWeek: []types.Post{
			{
				ID:          "a",
				Title:       "What is one change that helped your WooCommerce store?",
				Score:       14,
				NumComments: 17,
				Selftext:    "Sometimes even small changes can make a big difference. What helped your store?",
				Permalink:   "/r/woocommerce/comments/a/example/",
			},
			{
				ID:          "b",
				Title:       "Feels like WooCommerce is quietly becoming the infrastructure for AI agents",
				Score:       12,
				NumComments: 17,
				Selftext:    strings.Repeat("Structured product data matters. ", 80),
				Permalink:   "/r/woocommerce/comments/b/example/",
			},
		},
		TopMonth: []types.Post{
			{ID: "a", Title: "duplicate should be skipped", Score: 1},
			{
				ID:          "c",
				Title:       "Plugin conflict after checkout update",
				Score:       7,
				NumComments: 9,
				Selftext:    "Checkout extension conflict after the latest update.",
				Permalink:   "/r/woocommerce/comments/c/example/",
			},
		},
		Generated: time.Now().UTC(),
	}
}

const cannedInsightsJSON = `{
  "subreddit": "woocommerce",
  "takeaway": "The strongest signal is store-operator friction around WooCommerce performance workflows, while several other issues are narrower support cases.",
  "pain_points": [
    {
      "name": "Checkout changes and conversion friction",
      "summary": "Posts ask what store changes helped and discuss checkout-specific improvements.",
      "confidence": "medium",
      "evidence": [
        {"title": "What is one change that helped your WooCommerce store?", "score": 14, "comments": 17, "source": "top_week", "permalink": "/r/woocommerce/comments/a/example/"}
      ]
    }
  ],
  "specific_friction": [
    {
      "name": "Theme/plugin checkout compatibility",
      "summary": "A theme and deposit plugin interaction can lose checkout metadata during AJAX cart flows.",
      "confidence": "low",
      "evidence": [
        {"title": "Plugin conflict after checkout update", "score": 7, "comments": 9, "source": "top_month", "permalink": "/r/woocommerce/comments/c/example/"}
      ]
    }
  ],
  "repeated_asks": ["What changes actually helped a store?", "How can checkout plugin conflicts be diagnosed?"],
  "opportunity": ["Fewer-screen performance workflows and clearer checkout diagnostics show up as useful opportunity areas."],
  "language": ["checkout", "store", "structured product data"],
  "evidence": [
    {"title": "What is one change that helped your WooCommerce store?", "score": 14, "comments": 17, "source": "top_week", "permalink": "/r/woocommerce/comments/a/example/"},
    {"title": "Feels like WooCommerce is quietly becoming the infrastructure for AI agents", "score": 12, "comments": 17, "source": "top_week", "permalink": "/r/woocommerce/comments/b/example/"}
  ],
  "limitations": ["Only a few recent posts were supplied."]
}`

func TestRenderInsightsPromptIsEvidenceGrounded(t *testing.T) {
	prompt, err := RenderInsightsPrompt(sampleInsightResearch())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"r/woocommerce",
		"Do not invent facts",
		"Do not extrapolate",
		"Do not add advice about posting",
		"specific_friction",
		"Do not turn a question into a pain point",
		"What is one change that helped your WooCommerce store?",
		"Plugin conflict after checkout update",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	if strings.Contains(prompt, "duplicate should be skipped") {
		t.Errorf("duplicate post leaked into prompt")
	}
	if strings.Count(prompt, "Structured product data matters.") > 20 {
		t.Errorf("long selftext was not trimmed")
	}
}

func TestGenerateInsightsParsesAndRecordsTokens(t *testing.T) {
	p := &fakeInsightProvider{response: cannedInsightsJSON}
	insights, err := GenerateInsights(context.Background(), p, "gpt-5", sampleInsightResearch(), 1234)
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&p.calls) != 1 {
		t.Fatalf("calls = %d", p.calls)
	}
	if !p.gotReq.JSONMode {
		t.Error("JSONMode should be enabled")
	}
	if p.gotReq.Model != "gpt-5" {
		t.Errorf("model = %q", p.gotReq.Model)
	}
	if p.gotReq.MaxTokens != 1234 {
		t.Errorf("max_tokens = %d", p.gotReq.MaxTokens)
	}
	if insights.Subreddit != "woocommerce" {
		t.Errorf("subreddit = %q", insights.Subreddit)
	}
	if len(insights.PainPoints) != 1 {
		t.Fatalf("pain_points len = %d", len(insights.PainPoints))
	}
	if insights.Takeaway == "" {
		t.Error("takeaway not parsed")
	}
	if len(insights.SpecificFriction) != 1 {
		t.Fatalf("specific_friction len = %d", len(insights.SpecificFriction))
	}
	if insights.InputTokens != 11 || insights.OutputTokens != 22 {
		t.Errorf("tokens = in=%d out=%d", insights.InputTokens, insights.OutputTokens)
	}
	if insights.Generated.IsZero() {
		t.Error("generated not set")
	}
}

func TestGenerateInsightsEmptyResponseIsActionable(t *testing.T) {
	p := &fakeInsightProvider{response: ""}
	_, err := GenerateInsights(context.Background(), p, "gpt-5", sampleInsightResearch(), 1234)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty content") {
		t.Errorf("expected empty-content error, got %v", err)
	}
	if !strings.Contains(err.Error(), "--max-tokens") {
		t.Errorf("expected max-token guidance, got %v", err)
	}
}

func TestRenderMarkdownIsConcise(t *testing.T) {
	insights := &types.ResearchInsights{}
	if err := json.Unmarshal([]byte(cannedInsightsJSON), insights); err != nil {
		t.Fatal(err)
	}
	md := RenderMarkdown(insights)
	for _, want := range []string{
		"# r/woocommerce Research",
		"## Takeaway",
		"## Strongest Pain Points",
		"Checkout changes and conversion friction",
		"medium confidence",
		"## Specific Friction Seen",
		"Theme/plugin checkout compatibility",
		"## Evidence Posts",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "Body excerpt") {
		t.Errorf("markdown should not include raw body excerpts")
	}
}

func TestGenerateInsightsTrimsForHumanDigestibility(t *testing.T) {
	var payload types.ResearchInsights
	payload.Subreddit = "woocommerce"
	for i := 0; i < 5; i++ {
		payload.PainPoints = append(payload.PainPoints, types.PainPointCluster{Name: "pain"})
	}
	for i := 0; i < 6; i++ {
		payload.SpecificFriction = append(payload.SpecificFriction, types.SpecificFriction{Name: "friction"})
	}
	for i := 0; i < 10; i++ {
		payload.RepeatedAsks = append(payload.RepeatedAsks, "ask")
		payload.Opportunity = append(payload.Opportunity, "pain opportunity")
		payload.Language = append(payload.Language, "term")
		payload.Evidence = append(payload.Evidence, types.ResearchEvidence{Title: "evidence"})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeInsightProvider{response: string(raw)}
	insights, err := GenerateInsights(context.Background(), p, "gpt-5", sampleInsightResearch(), 1234)
	if err != nil {
		t.Fatal(err)
	}
	if len(insights.PainPoints) != 3 {
		t.Errorf("pain points len = %d", len(insights.PainPoints))
	}
	if len(insights.SpecificFriction) != 4 {
		t.Errorf("specific friction len = %d", len(insights.SpecificFriction))
	}
	if len(insights.RepeatedAsks) != 4 {
		t.Errorf("repeated asks len = %d", len(insights.RepeatedAsks))
	}
	if len(insights.Opportunity) != 3 {
		t.Errorf("opportunity len = %d", len(insights.Opportunity))
	}
	if len(insights.Language) != 8 {
		t.Errorf("language len = %d", len(insights.Language))
	}
	if len(insights.Evidence) != 5 {
		t.Errorf("evidence len = %d", len(insights.Evidence))
	}
}

func TestGenerateInsightsFiltersUnsupportedOpportunity(t *testing.T) {
	payload := types.ResearchInsights{
		Subreddit: "woocommerce",
		PainPoints: []types.PainPointCluster{{
			Name:    "Fragmented analytics workflows",
			Summary: "Owners juggle multiple screens for performance reporting.",
		}},
		SpecificFriction: []types.SpecificFriction{{
			Name:    "Theme plugin checkout compatibility",
			Summary: "AJAX cart flows can drop deposit metadata.",
		}},
		Opportunity: []string{
			"Fewer-screen analytics workflows inside Woo admin",
			"Restaurant online ordering after GloriaFood",
			"Checkout compatibility diagnostics for AJAX cart flows",
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	p := &fakeInsightProvider{response: string(raw)}
	insights, err := GenerateInsights(context.Background(), p, "gpt-5", sampleInsightResearch(), 1234)
	if err != nil {
		t.Fatal(err)
	}
	if len(insights.Opportunity) != 2 {
		t.Fatalf("opportunity len = %d: %+v", len(insights.Opportunity), insights.Opportunity)
	}
	for _, note := range insights.Opportunity {
		if strings.Contains(note, "Restaurant") {
			t.Fatalf("unsupported opportunity survived: %+v", insights.Opportunity)
		}
	}
}
