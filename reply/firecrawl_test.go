package reply

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// countingErrorRT is an http.RoundTripper that always errors and counts
// invocations. Used to assert retry policy without spinning up a real
// network endpoint.
type countingErrorRT struct {
	calls atomic.Int32
	err   error
}

func (rt *countingErrorRT) RoundTrip(*http.Request) (*http.Response, error) {
	rt.calls.Add(1)
	return nil, rt.err
}

// stubFirecrawl returns an httptest server that pretends to be the
// Firecrawl /v1/scrape endpoint, plus a teardown that resets the
// package-level base URL. Tests pass a payload (or an http.Handler) to
// shape the response.
func stubFirecrawl(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	prev := firecrawlAPIBase
	firecrawlAPIBase = srv.URL
	t.Cleanup(func() {
		firecrawlAPIBase = prev
		srv.Close()
	})
	return srv
}

func TestInspectFirecrawlHappyPath(t *testing.T) {
	resp := firecrawlScrapeResp{
		Success: true,
		Data: firecrawlData{
			Markdown: `# Welcome to my shop

Hand-thrown ceramic mugs from a single-artisan studio in Portland. Each piece is unique.

## Latest collection

This is a long enough paragraph block to qualify as a snippet for downstream consumption.

![Mug](https://cdn.example.com/mug.jpg)
![Vase](https://cdn.example.com/vase.png)

### Shipping

Free domestic shipping over $75.`,
			Links:      []string{"https://example.com/about", "https://example.com/cart"},
			Screenshot: "https://service.firecrawl.dev/storage/v1/object/public/screenshots/abc.png",
			Metadata: firecrawlMetadata{
				Title:         "My Etsy Shop — Handmade Ceramics",
				Description:   "Wheel-thrown ceramics by a single artisan.",
				OGTitle:       "My Etsy Shop",
				OGDescription: "OG description goes here.",
				StatusCode:    200,
			},
		},
	}

	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/scrape" {
			t.Errorf("path = %q, want /v1/scrape", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("auth header = %q", got)
		}
		var got firecrawlScrapeReq
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got.URL != "https://my-shop.example.com" {
			t.Errorf("body url = %q", got.URL)
		}
		// Formats should request all three useful pieces.
		hasMarkdown, hasScreenshot, hasLinks := false, false, false
		for _, f := range got.Formats {
			switch f {
			case "markdown":
				hasMarkdown = true
			case "screenshot@fullPage":
				hasScreenshot = true
			case "links":
				hasLinks = true
			}
		}
		if !hasMarkdown || !hasScreenshot || !hasLinks {
			t.Errorf("formats missing pieces: %v", got.Formats)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	insp, err := InspectFirecrawl(context.Background(), srv.Client(), "test-key", "https://my-shop.example.com")
	if err != nil {
		t.Fatal(err)
	}

	if insp.Source != "firecrawl" {
		t.Errorf("source = %q", insp.Source)
	}
	if insp.Title != "My Etsy Shop — Handmade Ceramics" {
		t.Errorf("title = %q", insp.Title)
	}
	if insp.MetaDescription != "Wheel-thrown ceramics by a single artisan." {
		t.Errorf("meta = %q", insp.MetaDescription)
	}
	if insp.OGTitle != "My Etsy Shop" {
		t.Errorf("og title = %q", insp.OGTitle)
	}
	if !strings.Contains(insp.Markdown, "Welcome to my shop") {
		t.Errorf("markdown not preserved: %q", insp.Markdown)
	}
	if insp.ScreenshotURL != "https://service.firecrawl.dev/storage/v1/object/public/screenshots/abc.png" {
		t.Errorf("screenshot URL = %q", insp.ScreenshotURL)
	}
	if insp.Status != 200 {
		t.Errorf("status = %d", insp.Status)
	}
	if insp.FetchedAt.IsZero() {
		t.Error("FetchedAt unset")
	}
}

func TestInspectFirecrawlExtractsHeadingsFromMarkdown(t *testing.T) {
	resp := firecrawlScrapeResp{
		Success: true,
		Data: firecrawlData{
			Markdown: "# H1 first\n\nbody text\n\n## H2 second\n\n### H3 third",
			Metadata: firecrawlMetadata{StatusCode: 200},
		},
	}
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))

	insp, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(insp.Headings) != 3 {
		t.Fatalf("got %d headings: %+v", len(insp.Headings), insp.Headings)
	}
	want := []struct {
		level int
		text  string
	}{{1, "H1 first"}, {2, "H2 second"}, {3, "H3 third"}}
	for i, w := range want {
		if insp.Headings[i].Level != w.level || insp.Headings[i].Text != w.text {
			t.Errorf("heading %d: got %+v, want %+v", i, insp.Headings[i], w)
		}
	}
}

func TestInspectFirecrawlExtractsImageURLs(t *testing.T) {
	resp := firecrawlScrapeResp{
		Success: true,
		Data: firecrawlData{
			Markdown: `Some text here.

![alt one](https://cdn.example.com/a.jpg)
![alt two](https://cdn.example.com/b.png "optional title")
![alt three](https://other.example.com/c.webp)
![rel](relative-path.png)
![data](data:image/png;base64,abc)
![dup](https://CDN.example.com/a.jpg)`,
			Metadata: firecrawlMetadata{StatusCode: 200},
		},
	}
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))

	insp, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Should have 3 absolute http(s) URLs after dedup. relative + data: rejected.
	if len(insp.ImageURLs) != 3 {
		t.Fatalf("got %d images: %v", len(insp.ImageURLs), insp.ImageURLs)
	}
	want := []string{
		"https://cdn.example.com/a.jpg",
		"https://cdn.example.com/b.png",
		"https://other.example.com/c.webp",
	}
	for i, w := range want {
		if insp.ImageURLs[i] != w {
			t.Errorf("image %d: got %q, want %q", i, insp.ImageURLs[i], w)
		}
	}
}

func TestInspectFirecrawlSnippetsSkipHeadingsAndCodeFences(t *testing.T) {
	resp := firecrawlScrapeResp{
		Success: true,
		Data: firecrawlData{
			Markdown: "# Heading skipped\n\nThis is a long enough paragraph block to qualify as a snippet for the test.\n\n## Another heading\n\n```\ncode block also skipped\n```\n\nAnother substantive paragraph block that should appear as its own snippet.",
			Metadata: firecrawlMetadata{StatusCode: 200},
		},
	}
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))

	insp, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range insp.Snippets {
		if strings.HasPrefix(s, "#") {
			t.Errorf("heading leaked into snippet: %q", s)
		}
		if strings.Contains(s, "code block also skipped") {
			t.Errorf("code fence content leaked: %q", s)
		}
	}
	mustContain := []string{
		"This is a long enough paragraph",
		"Another substantive paragraph",
	}
	for _, w := range mustContain {
		found := false
		for _, s := range insp.Snippets {
			if strings.Contains(s, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("snippet missing %q: %v", w, insp.Snippets)
		}
	}
}

func TestInspectFirecrawlEmptyKeyErrors(t *testing.T) {
	_, err := InspectFirecrawl(context.Background(), http.DefaultClient, "", "https://x.example.com")
	if err == nil || !strings.Contains(err.Error(), "empty API key") {
		t.Errorf("expected empty-key error, got %v", err)
	}
}

func TestInspectFirecrawlNon200Errors(t *testing.T) {
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))

	_, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
	if strings.Contains(err.Error(), "Bearer") || strings.Contains(err.Error(), "test-key") {
		t.Errorf("error should not leak auth header: %v", err)
	}
}

// withFastFirecrawlRetry shrinks the retry delay so retry tests don't
// sleep the production 500ms+ between attempts.
func withFastFirecrawlRetry(t *testing.T) {
	t.Helper()
	prev := firecrawlBaseDelay
	firecrawlBaseDelay = 1 * time.Millisecond
	t.Cleanup(func() { firecrawlBaseDelay = prev })
}

func TestInspectFirecrawlRetriesOn5xxThenSucceeds(t *testing.T) {
	withFastFirecrawlRetry(t)
	resp := firecrawlScrapeResp{
		Success: true,
		Data: firecrawlData{
			Markdown: "# ok",
			Metadata: firecrawlMetadata{Title: "T", StatusCode: 200},
		},
	}
	var calls atomic.Int32
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		switch n {
		case 1:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"success":false,"code":"SCRAPE_SITE_ERROR","error":"ERR_ABORTED"}`))
		case 2:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("Bad Gateway"))
		default:
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))

	insp, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if insp.Title != "T" {
		t.Errorf("title = %q", insp.Title)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

func TestInspectFirecrawlRetriesExhaustedOn5xx(t *testing.T) {
	withFastFirecrawlRetry(t)
	var calls atomic.Int32
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"code":"SCRAPE_SITE_ERROR","error":"ERR_ABORTED"}`))
	}))

	_, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "exhausted retries") {
		t.Errorf("expected 'exhausted retries' in error, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected status 500 surfaced, got %v", err)
	}
	// MaxRetry=3 → 1 initial + 3 retries = 4 calls.
	if got := calls.Load(); got != int32(firecrawlMaxRetry+1) {
		t.Errorf("calls = %d, want %d", got, firecrawlMaxRetry+1)
	}
}

func TestInspectFirecrawlDoesNotRetryOn4xx(t *testing.T) {
	withFastFirecrawlRetry(t)
	var calls atomic.Int32
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid api key"}`))
	}))

	_, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("4xx should not retry; calls = %d, want 1", got)
	}
}

func TestInspectFirecrawlDoesNotRetryTransportErrors(t *testing.T) {
	// Transport-level errors (connection reset, client timeout) must
	// not retry: we can't tell whether the server already accepted the
	// POST, and Firecrawl /v1/scrape is billed per request. Codex P1
	// finding from PR #44 review.
	withFastFirecrawlRetry(t)

	rt := &countingErrorRT{err: errors.New("connection refused")}
	client := &http.Client{Transport: rt}

	_, err := InspectFirecrawl(context.Background(), client, "k", "https://x.example.com")
	if err == nil {
		t.Fatal("expected transport error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected transport error surfaced, got %v", err)
	}
	if got := rt.calls.Load(); got != 1 {
		t.Errorf("transport errors must not retry; calls = %d, want 1", got)
	}
}

func TestInspectFirecrawlRetriesOn429(t *testing.T) {
	withFastFirecrawlRetry(t)
	resp := firecrawlScrapeResp{
		Success: true,
		Data:    firecrawlData{Markdown: "# ok", Metadata: firecrawlMetadata{StatusCode: 200}},
	}
	var calls atomic.Int32
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))

	if _, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com"); err != nil {
		t.Fatalf("expected success after 429 retry, got %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
}

func TestInspectFirecrawlSuccessFalseSurfaced(t *testing.T) {
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(firecrawlScrapeResp{
			Success: false,
			Error:   "page rendering timed out",
		})
	}))

	_, err := InspectFirecrawl(context.Background(), srv.Client(), "k", "https://x.example.com")
	if err == nil || !strings.Contains(err.Error(), "page rendering timed out") {
		t.Errorf("expected error message surfaced, got %v", err)
	}
}

func TestDispatchByEnv(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "")

	// With no key set, Inspect should call the HTML backend (which we
	// can verify by pointing it at an httptest server that returns HTML
	// and checking Source = "html").
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><head><title>plain</title></head></html>"))
	}))
	defer srv.Close()
	insp, err := Inspect(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Source != "html" {
		t.Errorf("expected html backend, got source = %q", insp.Source)
	}
}

func TestDispatchToFirecrawlWhenKeySet(t *testing.T) {
	resp := firecrawlScrapeResp{
		Success: true,
		Data: firecrawlData{
			Markdown: "# x",
			Metadata: firecrawlMetadata{Title: "T", StatusCode: 200},
		},
	}
	srv := stubFirecrawl(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Setenv("FIRECRAWL_API_KEY", "test-key")

	insp, err := Inspect(context.Background(), srv.Client(), "https://x.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if insp.Source != "firecrawl" {
		t.Errorf("expected firecrawl backend, got source = %q", insp.Source)
	}
}

func TestDownloadScreenshotWritesPNGToDestDir(t *testing.T) {
	const fakePNG = "\x89PNG\r\n\x1a\n" + "fake-image-bytes-go-here"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte(fakePNG))
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "screenshots")
	path, err := DownloadScreenshot(context.Background(), srv.Client(), srv.URL+"/abc.png", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, dir) {
		t.Errorf("path not under destDir: %s", path)
	}
	if !strings.HasSuffix(path, ".png") {
		t.Errorf("expected .png suffix, got %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "\x89PNG") {
		t.Error("written file does not have PNG header")
	}
}

func TestDownloadScreenshotEmptyURLErrors(t *testing.T) {
	_, err := DownloadScreenshot(context.Background(), http.DefaultClient, "", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "empty URL") {
		t.Errorf("expected empty-URL error, got %v", err)
	}
}

func TestDownloadScreenshotErrorsOnOversizedResponse(t *testing.T) {
	// Codex flagged this: previous code used io.LimitReader and silently
	// truncated oversized responses, leaving a corrupt PNG on disk. New
	// behavior: detect oversized response (read cap+1 bytes, check) and
	// fail BEFORE writing the file.
	const oversize = firecrawlScreenshotMaxBytes + 1024 // 1KB over the cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
		// Pad to oversize.
		junk := make([]byte, oversize-8)
		_, _ = w.Write(junk)
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "screenshots")
	_, err := DownloadScreenshot(context.Background(), srv.Client(), srv.URL+"/big.png", dir)
	if err == nil {
		t.Fatal("expected error for oversized response")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.Contains(err.Error(), "cap") {
		t.Errorf("error should mention cap exceeded, got: %v", err)
	}
	// No file should have been written.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			t.Errorf("oversized response left a partial PNG on disk: %s", e.Name())
		}
	}
}

func TestDownloadScreenshotNon200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := DownloadScreenshot(context.Background(), srv.Client(), srv.URL+"/x.png", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("expected 403 error, got %v", err)
	}
}
