package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// HTTPError is returned by providers when the upstream API responds with
// a non-2xx status. The retry wrapper inspects StatusCode to decide
// whether to retry (5xx + 429) or surface the error immediately (4xx).
type HTTPError struct {
	Provider   string
	StatusCode int
	Body       string
	ErrType    string
	Message    string
}

func (e *HTTPError) Error() string {
	switch {
	case e.ErrType != "" && e.Message != "":
		return fmt.Sprintf("%s %d %s: %s", e.Provider, e.StatusCode, e.ErrType, e.Message)
	case e.Message != "":
		return fmt.Sprintf("%s %d: %s", e.Provider, e.StatusCode, e.Message)
	default:
		return fmt.Sprintf("%s %d: %s", e.Provider, e.StatusCode, e.Body)
	}
}

// RetryableStatus reports whether a status warrants a retry. Mirrors the
// Reddit client policy: any 5xx or 429.
func RetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

const (
	DefaultMaxRetry  = 3
	DefaultBaseDelay = 500 * time.Millisecond
)

type retryProvider struct {
	inner     Provider
	MaxRetry  int
	BaseDelay time.Duration
}

// WithRetry wraps p so transient 5xx + 429 responses retry with
// exponential backoff. Wrapping is idempotent. Non-retryable errors
// (4xx, request build failures, JSON parse failures) propagate
// immediately. Context cancellation aborts the wait.
func WithRetry(p Provider) Provider {
	if p == nil {
		return nil
	}
	if _, ok := p.(*retryProvider); ok {
		return p
	}
	return &retryProvider{
		inner:     p,
		MaxRetry:  DefaultMaxRetry,
		BaseDelay: DefaultBaseDelay,
	}
}

func (r *retryProvider) Name() string { return r.inner.Name() }

// Unwrap exposes the inner Provider so callers (and tests) can reach
// concrete provider types when needed.
func (r *retryProvider) Unwrap() Provider { return r.inner }

func (r *retryProvider) Complete(ctx context.Context, req Request) (*Response, error) {
	var lastErr error
	for attempt := 0; attempt <= r.MaxRetry; attempt++ {
		if attempt > 0 {
			delay := r.BaseDelay << (attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := r.inner.Complete(ctx, req)
		if err == nil {
			return resp, nil
		}
		var herr *HTTPError
		if !errors.As(err, &herr) || !RetryableStatus(herr.StatusCode) {
			return nil, err
		}
		lastErr = err
	}
	return nil, fmt.Errorf("after %d attempts: %w", r.MaxRetry+1, lastErr)
}
