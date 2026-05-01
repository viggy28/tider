package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultMatchesShippedFlagDefaults(t *testing.T) {
	c := Default()
	if c.LLM.Provider != "openai" {
		t.Errorf("provider = %q, want openai", c.LLM.Provider)
	}
	if c.LLM.Model != "gpt-5" {
		t.Errorf("model = %q, want gpt-5", c.LLM.Model)
	}
	if c.LLM.MaxTokens != 10000 {
		t.Errorf("max_tokens = %d, want 10000", c.LLM.MaxTokens)
	}
	if c.Defaults.Providers != "openai,anthropic" {
		t.Errorf("providers default = %q", c.Defaults.Providers)
	}
}

func TestLoadFromMissingReturnsDefault(t *testing.T) {
	c, err := LoadFrom(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Provider != "openai" {
		t.Errorf("missing config should fall back to default, got %q", c.LLM.Provider)
	}
}

func TestLoadFromOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlBody := `
llm:
  provider: anthropic
  model: claude-sonnet-4-7
  max_tokens: 12000
  tasks:
    intake:
      model: claude-haiku-4-5
      max_tokens: 4096
defaults:
  render: markdown
author_context: |
  Some custom voice.
`
	if err := os.WriteFile(path, []byte(yamlBody), 0o644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Provider != "anthropic" {
		t.Errorf("provider override missed: %q", c.LLM.Provider)
	}
	if c.LLM.Model != "claude-sonnet-4-7" {
		t.Errorf("model override missed: %q", c.LLM.Model)
	}
	if c.LLM.MaxTokens != 12000 {
		t.Errorf("max_tokens override missed: %d", c.LLM.MaxTokens)
	}
	intake := c.LLM.Tasks["intake"]
	if intake.Model != "claude-haiku-4-5" {
		t.Errorf("intake task override missed: %q", intake.Model)
	}
	if c.Defaults.Render != "markdown" {
		t.Errorf("render default missed: %q", c.Defaults.Render)
	}
	if !strings.Contains(c.AuthorContext, "Some custom voice") {
		t.Errorf("author_context missed: %q", c.AuthorContext)
	}
}

func TestForTaskFallbackToLLMDefaults(t *testing.T) {
	c := Default()
	// "unknown" task isn't in the Tasks map → should return LLM-level defaults.
	p, m, mt := c.ForTask("unknown")
	if p != "openai" || m != "gpt-5" || mt != 10000 {
		t.Errorf("fallback wrong: provider=%q model=%q mt=%d", p, m, mt)
	}
}

func TestForTaskUsesTaskOverride(t *testing.T) {
	c := Default()
	p, m, mt := c.ForTask("intake")
	// intake task overrides only Model and MaxTokens, so Provider falls back.
	if p != "openai" || m != "gpt-4o-mini" || mt != 8192 {
		t.Errorf("intake override wrong: provider=%q model=%q mt=%d", p, m, mt)
	}
}

func TestFanOutModels(t *testing.T) {
	c := Default()
	am, om, mt := c.FanOutModels("post")
	if am != "claude-sonnet-4-7" {
		t.Errorf("anthropic model = %q", am)
	}
	if om != "gpt-5" {
		t.Errorf("openai model = %q", om)
	}
	if mt != 10000 {
		t.Errorf("max_tokens = %d, want 10000", mt)
	}

	// regen has different max_tokens
	_, _, mt = c.FanOutModels("regen")
	if mt != 4096 {
		t.Errorf("regen max_tokens = %d, want 4096", mt)
	}

	// unknown task → LLM-level max_tokens
	_, _, mt = c.FanOutModels("unknown")
	if mt != 10000 {
		t.Errorf("unknown task max_tokens = %d", mt)
	}
}

func TestForTaskPartialOverride(t *testing.T) {
	// If a task only overrides Model, Provider/MaxTokens should fall back.
	c := Default()
	c.LLM.Tasks["partial"] = TaskConfig{Model: "claude-haiku-4-5"}
	p, m, mt := c.ForTask("partial")
	if p != "openai" {
		t.Errorf("partial: provider should fall back to llm-level, got %q", p)
	}
	if m != "claude-haiku-4-5" {
		t.Errorf("partial: model override missed, got %q", m)
	}
	if mt != 10000 {
		t.Errorf("partial: max_tokens should fall back, got %d", mt)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	c := Default()
	c.AuthorContext = "I built X. I write like this."
	data, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "openai") {
		t.Errorf("marshal output missing provider: %s", data)
	}
	if !strings.Contains(string(data), "I built X") {
		t.Errorf("marshal output missing author_context: %s", data)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AuthorContext != c.AuthorContext {
		t.Errorf("round trip lost author_context: %q", loaded.AuthorContext)
	}
}

func TestLoadFromTasksMapNeverNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// Config without a tasks map at all.
	if err := os.WriteFile(path, []byte("llm:\n  provider: openai\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.LLM.Tasks == nil {
		t.Errorf("Tasks should never be nil after Load")
	}
	// Range is safe.
	for k := range c.LLM.Tasks {
		_ = k
	}
}
