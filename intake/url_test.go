package intake

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestGithubRepoToReadme(t *testing.T) {
	cases := []struct {
		in   string
		out  string
		want bool
	}{
		{"https://github.com/owner/repo", "https://raw.githubusercontent.com/owner/repo/HEAD/README.md", true},
		{"https://github.com/owner/repo/", "https://raw.githubusercontent.com/owner/repo/HEAD/README.md", true},
		{"https://www.github.com/owner/repo", "https://raw.githubusercontent.com/owner/repo/HEAD/README.md", true},
		{"https://GitHub.com/owner/repo", "https://raw.githubusercontent.com/owner/repo/HEAD/README.md", true},
		{"https://github.com/owner", "", false},
		{"https://github.com/owner/repo/blob/main/README.md", "", false},
		{"https://github.com/owner/repo/issues/1", "", false},
		{"https://example.com/owner/repo", "", false},
		{"https://gitlab.com/owner/repo", "", false},
		{"not a url at all", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := githubRepoToReadme(c.in)
		if ok != c.want || got != c.out {
			t.Errorf("githubRepoToReadme(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.out, c.want)
		}
	}
}

func TestFromURLFetchesAndExtracts(t *testing.T) {
	const html = `<html><head><title>Streambed</title></head>
<body>
<h1>Streambed</h1>
<p>WAL-native CDC for Postgres.</p>
</body></html>`

	var capturedUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()

	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{
		HTTP:      srv.Client(),
		UserAgent: DefaultUserAgent,
		Provider:  fake,
		MaxBytes:  1 << 20,
		MaxTokens: 1024,
	}

	brief, err := i.FromURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if capturedUA != DefaultUserAgent {
		t.Errorf("user-agent = %q", capturedUA)
	}
	if brief.Source.Mode != "url" {
		t.Errorf("source mode = %q", brief.Source.Mode)
	}
	if brief.Source.Value != srv.URL {
		t.Errorf("source value = %q (want server URL passthrough)", brief.Source.Value)
	}
	if brief.Title != "Streambed" {
		t.Errorf("title = %q", brief.Title)
	}
	if !strings.Contains(brief.RawContent, "WAL-native CDC") {
		t.Error("raw HTML body not preserved")
	}
}

func TestFromURLNon200Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: srv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.FromURL(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got %v", err)
	}
}

func TestFromURLRewritesGithubRepo(t *testing.T) {
	// Capture which path the server is asked for. We point the user-given
	// URL at our test server but make the path look like a GitHub repo
	// path; the rewriter should convert it to a raw README path.
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = w.Write([]byte("# Sample\n\nA project."))
	}))
	defer srv.Close()

	// Quick unit-level check: rewrite happens for github.com URLs.
	out, ok := githubRepoToReadme("https://github.com/owner/repo")
	if !ok || !strings.Contains(out, "raw.githubusercontent.com/owner/repo/HEAD/README.md") {
		t.Fatalf("rewrite failed: %q", out)
	}

	// Without faking DNS we can't intercept https://raw.githubusercontent.com,
	// so we exercise the non-rewrite path against the test server. This
	// guarantees that non-github URLs flow through unchanged.
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: srv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	if _, err := i.FromURL(context.Background(), srv.URL+"/some/path"); err != nil {
		t.Fatal(err)
	}
	if seenPath != "/some/path" {
		t.Errorf("seen path = %q, want unchanged passthrough", seenPath)
	}
}

func TestFromURLRespectsMaxBytes(t *testing.T) {
	body := strings.Repeat("x", 10000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: srv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1024, MaxTokens: 1024}

	brief, err := i.FromURL(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(brief.RawContent); got != 1024 {
		t.Errorf("raw content len = %d, want 1024 (cap)", got)
	}
}
