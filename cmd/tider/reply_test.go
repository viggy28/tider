package main

import (
	"testing"

	"github.com/viggy28/tider/config"
)

func TestResolveProviderModel(t *testing.T) {
	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider:       "openai",
			Model:          "gpt-5",
			MaxTokens:      10000,
			AnthropicModel: "claude-sonnet-4-7",
			OpenAIModel:    "gpt-5",
			Tasks: map[string]config.TaskConfig{
				"reply_mode": {Model: "gpt-4o-mini", MaxTokens: 2048},
				"reply":      {MaxTokens: 8192},
			},
		},
	}

	cases := []struct {
		desc                string
		task                string
		providerOverride    string
		modelOverride       string
		maxTokensOverride   int
		wantProvider        string
		wantModel           string
		wantMaxTokens       int
	}{
		{
			desc:          "no overrides — task config wins",
			task:          "reply_mode",
			wantProvider:  "openai",
			wantModel:     "gpt-4o-mini",
			wantMaxTokens: 2048,
		},
		{
			desc:             "provider override swaps to per-provider model",
			task:             "reply_mode",
			providerOverride: "anthropic",
			wantProvider:     "anthropic",
			// Task default model was gpt-4o-mini (OpenAI). Switching to
			// anthropic must replace it with the anthropic model so we
			// don't send an OpenAI model name to Anthropic.
			wantModel:     "claude-sonnet-4-7",
			wantMaxTokens: 2048,
		},
		{
			desc:             "provider override matches task provider — no model swap",
			task:             "reply_mode",
			providerOverride: "openai",
			wantProvider:     "openai",
			wantModel:        "gpt-4o-mini",
			wantMaxTokens:    2048,
		},
		{
			desc:             "explicit model overrides per-provider default",
			task:             "reply",
			providerOverride: "anthropic",
			modelOverride:    "claude-opus-4-7",
			wantProvider:     "anthropic",
			wantModel:        "claude-opus-4-7",
			wantMaxTokens:    8192,
		},
		{
			desc:              "max-tokens override wins",
			task:              "reply",
			maxTokensOverride: 4096,
			wantProvider:      "openai",
			wantModel:         "gpt-5",
			wantMaxTokens:     4096,
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			gotP, gotM, gotMT := resolveProviderModel(cfg, c.task, c.providerOverride, c.modelOverride, c.maxTokensOverride)
			if gotP != c.wantProvider || gotM != c.wantModel || gotMT != c.wantMaxTokens {
				t.Errorf("got (%q, %q, %d), want (%q, %q, %d)",
					gotP, gotM, gotMT,
					c.wantProvider, c.wantModel, c.wantMaxTokens)
			}
		})
	}
}
