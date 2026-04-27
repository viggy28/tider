// Package llm is the only package in tider allowed to talk to LLM providers.
// All callers go through the Provider interface; provider-native types
// (Anthropic, OpenAI) never leak past this package boundary.
package llm

import "context"

const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Request struct {
	Model       string
	Messages    []Message
	MaxTokens   int
	Temperature float64
	// JSONMode signals "I want a JSON value back, no prose." Each provider
	// implements this idiomatically: OpenAI uses native response_format,
	// Anthropic uses a system-instruction nudge.
	JSONMode bool
}

type Response struct {
	Content      string
	InputTokens  int
	OutputTokens int
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (*Response, error)
}

// ProviderRef pairs a Provider implementation with the model name to use
// for completions. Used by callers that fan out across multiple providers
// (draft, regen) and need to record which model produced which output.
type ProviderRef struct {
	Provider Provider
	Model    string
}
