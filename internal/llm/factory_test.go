package llm

import (
	"strings"
	"testing"
)

func TestNewAnthropicProvider(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test")
	p, err := New(Config{Provider: "anthropic", Model: "claude-sonnet-4-7"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestNewOpenAIProvider(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test")
	p, err := New(Config{Provider: "openai", Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "openai" {
		t.Errorf("name = %q", p.Name())
	}
}

func TestNewUnknownProvider(t *testing.T) {
	_, err := New(Config{Provider: "cohere"})
	if err == nil || !strings.Contains(err.Error(), `unknown provider "cohere"`) {
		t.Errorf("expected unknown-provider error, got %v", err)
	}
}

func TestNewEmptyProvider(t *testing.T) {
	_, err := New(Config{})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Errorf("expected not-configured error, got %v", err)
	}
}

func TestNewMissingAPIKeyPropagated(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := New(Config{Provider: "anthropic", Model: "claude-sonnet-4-7"})
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("expected missing-key error, got %v", err)
	}
}

func TestForTaskOverride(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ak")
	t.Setenv("OPENAI_API_KEY", "ok")
	cfg := Config{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-7",
		Tasks: map[string]TaskConfig{
			"draft": {Provider: "openai", Model: "gpt-5"},
		},
	}
	p, err := ForTask(cfg, "draft")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "openai" {
		t.Errorf("override not applied: name = %q", p.Name())
	}
	if openai, ok := p.(*OpenAI); !ok || openai.Model != "gpt-5" {
		t.Errorf("override model not applied: %+v", p)
	}
}

func TestForTaskFallsBackToBase(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ak")
	cfg := Config{
		Provider: "anthropic",
		Model:    "claude-sonnet-4-7",
		Tasks:    map[string]TaskConfig{},
	}
	p, err := ForTask(cfg, "anything")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "anthropic" {
		t.Errorf("fallback failed: name = %q", p.Name())
	}
}

func TestForTaskNilMap(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "ak")
	cfg := Config{Provider: "anthropic", Model: "m"}
	if _, err := ForTask(cfg, "draft"); err != nil {
		t.Errorf("nil tasks map should be safe: %v", err)
	}
}
