package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live tests hit the real provider APIs. They are gated behind TIDER_LIVE_LLM
// so default `go test ./...` runs offline. Set TIDER_LIVE_LLM=1 plus the
// relevant API key(s) to exercise them.
//
//   TIDER_LIVE_LLM=1 ANTHROPIC_API_KEY=... OPENAI_API_KEY=... go test ./internal/llm/ -v -run Live

func liveEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("TIDER_LIVE_LLM") == "" {
		t.Skip("set TIDER_LIVE_LLM=1 to enable live API tests")
	}
}

func TestLiveAnthropic(t *testing.T) {
	liveEnabled(t)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("TIDER_LIVE_ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-haiku-4-5"
	}
	p, err := NewAnthropic(model)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Complete(ctx, Request{
		MaxTokens: 32,
		Messages: []Message{
			{Role: RoleUser, Content: "Reply with exactly: ok"},
		},
	})
	if err != nil {
		t.Fatalf("live anthropic: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("empty content")
	}
	if resp.OutputTokens == 0 {
		t.Errorf("output_tokens=0 (parsing usage failed?): %+v", resp)
	}
	t.Logf("anthropic %s → %q  (in=%d out=%d)", model, strings.TrimSpace(resp.Content), resp.InputTokens, resp.OutputTokens)
}

func TestLiveAnthropicJSONMode(t *testing.T) {
	liveEnabled(t)
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("TIDER_LIVE_ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-haiku-4-5"
	}
	p, err := NewAnthropic(model)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Complete(ctx, Request{
		MaxTokens: 64,
		JSONMode:  true,
		Messages: []Message{
			{Role: RoleUser, Content: `Return a JSON object with one key "ok" set to true.`},
		},
	})
	if err != nil {
		t.Fatalf("live anthropic json: %v", err)
	}
	got := strings.TrimSpace(resp.Content)
	if !strings.HasPrefix(got, "{") {
		t.Errorf("expected JSON object, got: %q", got)
	}
	t.Logf("anthropic json mode → %s", got)
}

func TestLiveOpenAI(t *testing.T) {
	liveEnabled(t)
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("TIDER_LIVE_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	p, err := NewOpenAI(model)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Complete(ctx, Request{
		MaxTokens: 32,
		Messages: []Message{
			{Role: RoleUser, Content: "Reply with exactly: ok"},
		},
	})
	if err != nil {
		t.Fatalf("live openai: %v", err)
	}
	if resp.Content == "" {
		t.Fatal("empty content")
	}
	if resp.OutputTokens == 0 {
		t.Errorf("output_tokens=0 (parsing usage failed?): %+v", resp)
	}
	t.Logf("openai %s → %q  (in=%d out=%d)", model, strings.TrimSpace(resp.Content), resp.InputTokens, resp.OutputTokens)
}

func TestLiveOpenAIJSONMode(t *testing.T) {
	liveEnabled(t)
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("TIDER_LIVE_OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	p, err := NewOpenAI(model)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := p.Complete(ctx, Request{
		MaxTokens: 64,
		JSONMode:  true,
		Messages: []Message{
			{Role: RoleUser, Content: `Return a JSON object with one key "ok" set to true.`},
		},
	})
	if err != nil {
		t.Fatalf("live openai json: %v", err)
	}
	got := strings.TrimSpace(resp.Content)
	if !strings.HasPrefix(got, "{") {
		t.Errorf("expected JSON object, got: %q", got)
	}
	t.Logf("openai json mode → %s", got)
}
