package llm

import "strings"

// visionCapableOpenAIModels is a hardcoded allowlist of OpenAI models
// known to accept image_url content blocks. Maintained by hand — when
// OpenAI ships a new vision model, add it here. We intentionally avoid
// a Provider.Capabilities() abstraction in v1; a static list is honest
// about the maintenance cost and easy to audit.
//
// Match is a prefix check so model identifiers with date suffixes
// (e.g. "gpt-4o-2024-11-20") still resolve.
var visionCapableOpenAIModels = []string{
	"gpt-4o",
	"gpt-4o-mini",
	"gpt-4-turbo",
	"gpt-4-vision",
	"gpt-5", // gpt-5 family supports vision per OpenAI docs
}

// SupportsVision reports whether the given (provider, model) pair can
// accept image inputs. Anthropic returns false in v1 — vision support
// for the Anthropic provider is deferred per
// SPEC_REVIEW_VISUAL_FIRECRAWL.md.
//
// Callers should use this before constructing a Request with non-empty
// Images, to fail fast with a clear error rather than triggering a
// provider-level "model does not support image inputs" error mid-flow.
func SupportsVision(provider, model string) bool {
	if provider != "openai" {
		return false
	}
	for _, m := range visionCapableOpenAIModels {
		if strings.HasPrefix(model, m) {
			return true
		}
	}
	return false
}
