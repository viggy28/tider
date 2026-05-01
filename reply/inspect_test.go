package reply

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const sampleHTML = `<!doctype html>
<html>
<head>
<title>My Etsy Shop — Handmade Ceramics</title>
<meta name="description" content="Wheel-thrown ceramics by a single artisan.">
<meta property="og:title" content="My Etsy Shop">
<meta property="og:description" content="OG description goes here.">
<style>.junk{color:red}</style>
</head>
<body>
<nav>Home / Shop / Cart</nav>
<h1>Welcome to my shop</h1>
<p>Each piece is handmade in my home studio — no AI, no dropshipping, real clay.</p>
<h2>Latest pieces</h2>
<p>This is a long enough paragraph to count as a snippet so it should appear in the inspection output.</p>
<ul>
  <li>Mug — $40, glazed celadon, 10oz capacity, handmade by me this week.</li>
  <li>Vase</li>
</ul>
<h3>Read more</h3>
<script>console.log('do not include this');</script>
<footer>Copyright 2026 — boilerplate that should be skipped</footer>
</body>
</html>`

func TestInspectExtractsCoreFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(sampleHTML))
	}))
	defer srv.Close()

	insp, err := Inspect(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	if insp.Title != "My Etsy Shop — Handmade Ceramics" {
		t.Errorf("title: %q", insp.Title)
	}
	if insp.MetaDescription != "Wheel-thrown ceramics by a single artisan." {
		t.Errorf("meta desc: %q", insp.MetaDescription)
	}
	if insp.OGTitle != "My Etsy Shop" {
		t.Errorf("og title: %q", insp.OGTitle)
	}
	if insp.OGDescription != "OG description goes here." {
		t.Errorf("og desc: %q", insp.OGDescription)
	}
	if insp.Status != http.StatusOK {
		t.Errorf("status: %d", insp.Status)
	}
	if insp.URL != srv.URL {
		t.Errorf("url: %q", insp.URL)
	}
	if insp.FetchedAt.IsZero() {
		t.Error("FetchedAt unset")
	}
}

func TestInspectHeadings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleHTML))
	}))
	defer srv.Close()

	insp, _ := Inspect(context.Background(), srv.Client(), srv.URL)
	if len(insp.Headings) != 3 {
		t.Fatalf("expected 3 headings, got %d: %+v", len(insp.Headings), insp.Headings)
	}
	want := []struct {
		Level int
		Text  string
	}{
		{1, "Welcome to my shop"},
		{2, "Latest pieces"},
		{3, "Read more"},
	}
	for i, w := range want {
		if insp.Headings[i].Level != w.Level || insp.Headings[i].Text != w.Text {
			t.Errorf("heading %d: got %+v, want %+v", i, insp.Headings[i], w)
		}
	}
}

func TestInspectSnippetsSkipNoiseSections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleHTML))
	}))
	defer srv.Close()

	insp, _ := Inspect(context.Background(), srv.Client(), srv.URL)

	// Should pick up the long paragraph and the long li, skip the short
	// li ("Vase"), skip nav/footer/script content entirely.
	for _, s := range insp.Snippets {
		if strings.Contains(s, "do not include this") {
			t.Errorf("script content leaked: %q", s)
		}
		if strings.Contains(s, "Copyright 2026") {
			t.Errorf("footer content leaked: %q", s)
		}
		if strings.Contains(s, "Home / Shop / Cart") {
			t.Errorf("nav content leaked: %q", s)
		}
	}

	mustContain := []string{
		"Each piece is handmade",
		"This is a long enough paragraph",
		"Mug — $40, glazed celadon",
	}
	for _, want := range mustContain {
		found := false
		for _, s := range insp.Snippets {
			if strings.Contains(s, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("snippet missing %q\n--- snippets ---\n%s", want, strings.Join(insp.Snippets, "\n"))
		}
	}
}

func TestInspectNon200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Inspect(context.Background(), srv.Client(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestInspectEmptyHTMLReturnsEmptyButValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(""))
	}))
	defer srv.Close()

	insp, err := Inspect(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if insp.Title != "" || len(insp.Headings) != 0 {
		t.Errorf("expected empty inspection, got %+v", insp)
	}
	if insp.Status != http.StatusOK {
		t.Errorf("status should still be set: %d", insp.Status)
	}
}

func TestInspectRespectsByteCap(t *testing.T) {
	huge := strings.Repeat("<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt.</p>", 5000)
	body := "<html><head><title>Big</title></head><body>" + huge + "</body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	insp, err := Inspect(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	// Snippet count is capped.
	if len(insp.Snippets) > inspectMaxSnippets {
		t.Errorf("snippets exceed cap: %d", len(insp.Snippets))
	}
}

func TestInspectSendsUserAgent(t *testing.T) {
	var seenUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte("<html><head><title>x</title></head></html>"))
	}))
	defer srv.Close()

	if _, err := Inspect(context.Background(), srv.Client(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seenUA, "tider") {
		t.Errorf("user agent should identify tider, got %q", seenUA)
	}
}

// SPEC_REPLY.md mandates "if inspection fails → fail clearly, preserve
// session" — Inspect must NOT fall back from Firecrawl to HTML when the
// user opted in to Firecrawl by setting FIRECRAWL_API_KEY. Silently
// downgrading would mask a backend failure under output that looks
// Firecrawl-shaped to the caller but is actually missing markdown,
// screenshot, and image fields.
func TestInspectFirecrawlErrorDoesNotFallBackToHTML(t *testing.T) {
	fcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer fcSrv.Close()
	prev := firecrawlAPIBase
	firecrawlAPIBase = fcSrv.URL
	defer func() { firecrawlAPIBase = prev }()
	t.Setenv("FIRECRAWL_API_KEY", "test-key")

	// HTML server we expect to NEVER be reached on Firecrawl failure.
	htmlReached := false
	htmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		htmlReached = true
		_, _ = w.Write([]byte(sampleHTML))
	}))
	defer htmlSrv.Close()

	_, err := Inspect(context.Background(), &http.Client{}, htmlSrv.URL)
	if err == nil {
		t.Fatal("expected Firecrawl failure to propagate, got nil error")
	}
	if !strings.Contains(err.Error(), "firecrawl") {
		t.Errorf("expected error to identify Firecrawl as the source, got: %v", err)
	}
	if htmlReached {
		t.Error("HTML inspector was reached after Firecrawl failure — fallback violates spec")
	}
}

// SPEC_REVIEW_VISUAL_FIRECRAWL.md mandates that review mode require
// Firecrawl + a screenshot. InspectReviewTarget enforces both invariants
// and produces explicit errors callers can route around with
// `--mode=reply`.

func TestInspectReviewTargetRequiresFirecrawlKey(t *testing.T) {
	t.Setenv("FIRECRAWL_API_KEY", "")
	_, err := InspectReviewTarget(context.Background(), &http.Client{}, "https://example.com/")
	if err == nil {
		t.Fatal("expected error when FIRECRAWL_API_KEY is unset")
	}
	if !strings.Contains(err.Error(), "FIRECRAWL_API_KEY") {
		t.Errorf("error should name the missing env var: %v", err)
	}
	if !strings.Contains(err.Error(), "--mode=reply") {
		t.Errorf("error should hint at the --mode=reply escape hatch: %v", err)
	}
}

func TestInspectReviewTargetRequiresScreenshot(t *testing.T) {
	// Firecrawl returns success but with no screenshot URL.
	resp := `{
  "success": true,
  "data": {
    "markdown": "# Welcome",
    "metadata": {"title": "X", "statusCode": 200}
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()
	prev := firecrawlAPIBase
	firecrawlAPIBase = srv.URL
	defer func() { firecrawlAPIBase = prev }()
	t.Setenv("FIRECRAWL_API_KEY", "test-key")

	_, err := InspectReviewTarget(context.Background(), &http.Client{}, "https://example.com/")
	if err == nil {
		t.Fatal("expected error when Firecrawl returns no screenshot")
	}
	if !strings.Contains(err.Error(), "screenshot") {
		t.Errorf("error should name the missing screenshot: %v", err)
	}
}

func TestInspectReviewTargetHappyPath(t *testing.T) {
	resp := `{
  "success": true,
  "data": {
    "markdown": "# Welcome\n\nReal content.",
    "screenshot": "https://firecrawl-cdn.example/sig/abc.png",
    "links": [],
    "metadata": {"title": "Shop", "statusCode": 200}
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()
	prev := firecrawlAPIBase
	firecrawlAPIBase = srv.URL
	defer func() { firecrawlAPIBase = prev }()
	t.Setenv("FIRECRAWL_API_KEY", "test-key")

	insp, err := InspectReviewTarget(context.Background(), &http.Client{}, "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	if insp.Source != "firecrawl" {
		t.Errorf("Source = %q", insp.Source)
	}
	if insp.ScreenshotURL == "" {
		t.Error("ScreenshotURL should be populated on happy path")
	}
}

func TestCollapseWhitespace(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  hello  world  ", "hello world"},
		{"line1\nline2\tline3", "line1 line2 line3"},
		{"   ", ""},
		{"", ""},
		{"single", "single"},
	}
	for _, c := range cases {
		if got := collapseWhitespace(c.in); got != c.want {
			t.Errorf("collapseWhitespace(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
