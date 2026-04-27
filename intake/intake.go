// Package intake converts a URL, file path, or topic into a structured Brief.
// All three modes funnel through one LLM-backed extraction step so the
// output shape is consistent regardless of input.
package intake

import (
	"net/http"
	"time"

	"github.com/viggy28/tider/internal/llm"
)

const DefaultUserAgent = "tider/0.1 (by /u/tider28)"

// Intake holds the dependencies needed to turn raw input into a Brief.
type Intake struct {
	HTTP      *http.Client
	UserAgent string
	Provider  llm.Provider
	// MaxBytes caps the bytes read from a URL or file before the LLM sees
	// them. Stops a giant page from blowing the context window.
	MaxBytes int64
	// MaxTokens is the LLM completion budget for the extraction call.
	MaxTokens int
}

// New constructs an Intake with sensible defaults. Provider is required.
func New(p llm.Provider) *Intake {
	return &Intake{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Provider:  p,
		MaxBytes:  256 * 1024,
		MaxTokens: 2048,
	}
}
