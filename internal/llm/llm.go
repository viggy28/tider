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

// ImageInput is a vision-model input. Path is preferred — local files
// are read and base64-encoded by the provider so the wire request is
// self-contained even when the original signed URL has expired
// (Firecrawl screenshot URLs are an example). URL is the fallback for
// callers that only have a remote reference; OpenAI passes those
// through as `image_url`. Set MIME to override the inferred type
// (defaults to image/png for screenshots, image/jpeg for product
// photos, derived from extension).
type ImageInput struct {
	Path string // local path; preferred when set
	URL  string // remote URL; fallback when Path is empty
	MIME string // optional override
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
	// Images attach to the last user-role message. Providers that don't
	// support image inputs (Anthropic in v1) return a clear error when
	// Images is non-empty.
	Images []ImageInput
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
