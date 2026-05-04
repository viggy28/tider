package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// fakeProvider is a Provider stub whose Complete returns a programmable
// sequence of responses/errors. Each call advances the sequence.
type fakeProvider struct {
	name    string
	results []fakeResult
	calls   atomic.Int32
}

type fakeResult struct {
	resp *Response
	err  error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Complete(ctx context.Context, _ Request) (*Response, error) {
	idx := int(f.calls.Add(1)) - 1
	if idx >= len(f.results) {
		return nil, fmt.Errorf("fake: ran out of results at call %d", idx)
	}
	r := f.results[idx]
	return r.resp, r.err
}

func newRetryNoSleep(p Provider) *retryProvider {
	return &retryProvider{inner: p, MaxRetry: DefaultMaxRetry, BaseDelay: 1 * time.Millisecond}
}

func TestRetryReturnsImmediatelyOnSuccess(t *testing.T) {
	want := &Response{Content: "ok"}
	f := &fakeProvider{name: "fake", results: []fakeResult{{resp: want}}}
	r := newRetryNoSleep(f)
	got, err := r.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("response = %v", got)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestRetryRetriesOn5xxThenSucceeds(t *testing.T) {
	want := &Response{Content: "ok"}
	f := &fakeProvider{
		name: "fake",
		results: []fakeResult{
			{err: &HTTPError{Provider: "openai", StatusCode: 502, Message: "bad gateway"}},
			{err: &HTTPError{Provider: "openai", StatusCode: 503}},
			{resp: want},
		},
	}
	r := newRetryNoSleep(f)
	got, err := r.Complete(context.Background(), Request{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("response = %v", got)
	}
	if n := f.calls.Load(); n != 3 {
		t.Errorf("calls = %d, want 3", n)
	}
}

func TestRetryRetriesOn429(t *testing.T) {
	want := &Response{Content: "ok"}
	f := &fakeProvider{
		name: "fake",
		results: []fakeResult{
			{err: &HTTPError{Provider: "openai", StatusCode: 429, Message: "rate limited"}},
			{resp: want},
		},
	}
	r := newRetryNoSleep(f)
	if _, err := r.Complete(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	if n := f.calls.Load(); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
}

func TestRetryDoesNotRetry4xx(t *testing.T) {
	wantErr := &HTTPError{Provider: "openai", StatusCode: 401, Message: "auth"}
	f := &fakeProvider{name: "fake", results: []fakeResult{{err: wantErr}}}
	r := newRetryNoSleep(f)
	_, err := r.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) || herr.StatusCode != 401 {
		t.Errorf("unexpected error: %v", err)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1 (no retry on 4xx)", n)
	}
}

func TestRetryDoesNotRetryNonHTTPError(t *testing.T) {
	wantErr := errors.New("marshal: bad request")
	f := &fakeProvider{name: "fake", results: []fakeResult{{err: wantErr}}}
	r := newRetryNoSleep(f)
	_, err := r.Complete(context.Background(), Request{})
	if !errors.Is(err, wantErr) {
		t.Errorf("expected %v, got %v", wantErr, err)
	}
	if n := f.calls.Load(); n != 1 {
		t.Errorf("calls = %d, want 1", n)
	}
}

func TestRetryGivesUpAfterMaxRetry(t *testing.T) {
	final := &HTTPError{Provider: "openai", StatusCode: 500, Message: "boom"}
	f := &fakeProvider{
		name: "fake",
		results: []fakeResult{
			{err: &HTTPError{Provider: "openai", StatusCode: 500}},
			{err: &HTTPError{Provider: "openai", StatusCode: 502}},
			{err: &HTTPError{Provider: "openai", StatusCode: 503}},
			{err: final},
		},
	}
	r := newRetryNoSleep(f)
	_, err := r.Complete(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	var herr *HTTPError
	if !errors.As(err, &herr) || herr != final {
		t.Errorf("expected wrapped final HTTPError, got %v", err)
	}
	// MaxRetry=3 → 1 initial + 3 retries = 4 calls total.
	if n := f.calls.Load(); n != 4 {
		t.Errorf("calls = %d, want 4", n)
	}
}

func TestRetryRespectsContextCancellation(t *testing.T) {
	f := &fakeProvider{
		name: "fake",
		results: []fakeResult{
			{err: &HTTPError{Provider: "openai", StatusCode: 500}},
			{err: &HTTPError{Provider: "openai", StatusCode: 500}},
		},
	}
	// BaseDelay long enough that the wait actually blocks; we cancel
	// before the first backoff completes.
	r := &retryProvider{inner: f, MaxRetry: 3, BaseDelay: 200 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := r.Complete(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRetryWithNilReturnsNil(t *testing.T) {
	if WithRetry(nil) != nil {
		t.Error("WithRetry(nil) should return nil")
	}
}

func TestRetryIsIdempotent(t *testing.T) {
	f := &fakeProvider{name: "fake", results: []fakeResult{{resp: &Response{}}}}
	once := WithRetry(f)
	twice := WithRetry(once)
	if once != twice {
		t.Error("WithRetry should be idempotent (same pointer)")
	}
}

func TestHTTPErrorMessageFormats(t *testing.T) {
	cases := []struct {
		name string
		e    *HTTPError
		want string
	}{
		{"type+message", &HTTPError{Provider: "openai", StatusCode: 502, ErrType: "server_error", Message: "bad gateway"}, "openai 502 server_error: bad gateway"},
		{"message only", &HTTPError{Provider: "anthropic", StatusCode: 500, Message: "boom"}, "anthropic 500: boom"},
		{"body fallback", &HTTPError{Provider: "openai", StatusCode: 503, Body: "Service Unavailable"}, "openai 503: Service Unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.Error(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRetryableStatus(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{200, false},
		{401, false},
		{404, false},
		{http.StatusTooManyRequests, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
		{599, true},
	}
	for _, tc := range cases {
		if got := RetryableStatus(tc.code); got != tc.want {
			t.Errorf("RetryableStatus(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}
