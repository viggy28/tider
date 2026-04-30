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
  "repeated_asks": ["What changes actually helped a store?"],
  "opportunity": ["Practical diagnostics around checkout and product data show up as concrete needs."],
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
		"## Pain Point Clusters",
		"Checkout changes and conversion friction",
		"medium confidence",
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
