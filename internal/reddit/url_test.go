package reddit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
)

func TestParseThreadURL(t *testing.T) {
	cases := []struct {
		in     string
		sub    string
		postID string
		ok     bool
		desc   string
	}{
		// Canonical
		{"https://www.reddit.com/r/golang/comments/abc123/title-slug/", "golang", "abc123", true, "www canonical"},
		{"https://www.reddit.com/r/golang/comments/abc123/", "golang", "abc123", true, "www canonical no slug"},
		{"https://reddit.com/r/golang/comments/abc123/title-slug/", "golang", "abc123", true, "no www"},

		// old. / np. / new.
		{"https://old.reddit.com/r/golang/comments/abc123/title-slug/", "golang", "abc123", true, "old.reddit"},
		{"https://np.reddit.com/r/golang/comments/abc123/", "golang", "abc123", true, "np.reddit"},
		{"https://new.reddit.com/r/golang/comments/abc123/", "golang", "abc123", true, "new.reddit"},

		// /comments/<id>/ form (no sub in path)
		{"https://www.reddit.com/comments/abc123/", "", "abc123", true, "global comments form"},
		{"https://reddit.com/comments/abc123", "", "abc123", true, "global no trailing slash"},

		// Short link
		{"https://redd.it/abc123", "", "abc123", true, "short link"},

		// Mixed case host
		{"https://Reddit.com/r/Golang/comments/abc123/", "Golang", "abc123", true, "case-insensitive host preserves path case"},

		// Malformed
		{"", "", "", false, "empty"},
		{"not a url", "", "", false, "not a url"},
		{"https://example.com/r/golang/comments/abc/", "", "", false, "wrong host"},
		{"https://reddit.com/r/golang", "", "", false, "no comments path"},
		{"https://reddit.com/r/golang/comments", "", "", false, "comments without id"},
		{"https://reddit.com/r/golang/wiki/index", "", "", false, "non-comments path"},
		{"https://redd.it/", "", "", false, "redd.it without id"},
		{"https://reddit.com/comments//slug", "", "", false, "empty post id global form"},
		{"https://reddit.com/r/golang/comments//slug/", "", "", false, "empty post id sub form"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			sub, id, err := ParseThreadURL(c.in)
			if c.ok && err != nil {
				t.Fatalf("expected success, got err=%v", err)
			}
			if !c.ok && err == nil {
				t.Fatalf("expected error, got sub=%q id=%q", sub, id)
			}
			if c.ok && (sub != c.sub || id != c.postID) {
				t.Errorf("got (%q,%q), want (%q,%q)", sub, id, c.sub, c.postID)
			}
		})
	}
}

func TestResolveShortLinkFollowsRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://www.reddit.com/r/golang/comments/abc123/test-title/")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	canonical, err := ResolveShortLink(context.Background(), srv.Client(), srv.URL+"/abc123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(canonical, "/r/golang/comments/abc123/") {
		t.Errorf("canonical URL not as expected: %s", canonical)
	}
}

func TestResolveShortLinkNoRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("page body"))
	}))
	defer srv.Close()

	_, err := ResolveShortLink(context.Background(), srv.Client(), srv.URL+"/abc123")
	if err == nil || !strings.Contains(err.Error(), "expected redirect") {
		t.Errorf("expected no-redirect error, got %v", err)
	}
}

func TestResolveShortLinkNoLocationHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound) // 302 with no Location
	}))
	defer srv.Close()

	_, err := ResolveShortLink(context.Background(), srv.Client(), srv.URL+"/abc123")
	if err == nil || !strings.Contains(err.Error(), "no Location header") {
		t.Errorf("expected no-location error, got %v", err)
	}
}

// Canonicalize is the entry point used by the CLI: must be a no-op for
// already-canonical URLs (no I/O) and must follow the redirect for
// Reddit mobile share links (/s/<token>) so users pasting from the
// Reddit app can use `tider reply --url=...`.

func TestCanonicalizeNoOpForCanonicalForms(t *testing.T) {
	cases := []string{
		"https://www.reddit.com/r/golang/comments/abc123/title-slug/",
		"https://reddit.com/r/golang/comments/abc123/",
		"https://reddit.com/comments/abc123/",
		"https://old.reddit.com/r/golang/comments/abc123/",
		"https://redd.it/abc123",
		"https://example.com/anything", // non-reddit host: passthrough, ParseThreadURL surfaces error
	}
	// Client we'd panic if it actually got called (no httptest server set
	// up): if Canonicalize tries to make a request, the test fails.
	client := &http.Client{Transport: &noNetTransport{t: t}}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := Canonicalize(context.Background(), client, in)
			if err != nil {
				t.Fatal(err)
			}
			if got != in {
				t.Errorf("expected passthrough, got %q (was %q)", got, in)
			}
		})
	}
}

func TestCanonicalizeFollowsShareLinkRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reddit's actual share endpoint serves a 30x with Location
		// pointing at the canonical thread URL.
		if !strings.HasPrefix(r.URL.Path, "/s/") {
			t.Errorf("share link redirect should hit /s/ path, got %q", r.URL.Path)
		}
		w.Header().Set("Location", "https://www.reddit.com/r/golang/comments/abc123/title-slug/")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	// Build a /s/<token> URL whose host is the test server (the function
	// only checks for reddit.com host before resolving — patch via custom
	// host check by using rawpath that includes /s/).
	// Trick: use the raw URL but override host detection by changing the
	// test setup. Easier: hit the server's URL directly. The function's
	// host check requires reddit.com, so we use an http.Client that
	// targets the test server while the URL claims to be reddit.com.
	resolved, err := Canonicalize(context.Background(), shareLinkRedirectClient(srv.URL), "https://www.reddit.com/s/abc123XYZ")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolved, "/r/golang/comments/abc123/") {
		t.Errorf("expected canonical thread URL, got %q", resolved)
	}
}

func TestCanonicalizeShareLinkPropagatesRedirectError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 302 with no Location — same failure mode the existing
		// ResolveShortLink test covers, but reached through the share
		// link entry point.
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	_, err := Canonicalize(context.Background(), shareLinkRedirectClient(srv.URL), "https://www.reddit.com/s/abc123XYZ")
	if err == nil || !strings.Contains(err.Error(), "no Location header") {
		t.Errorf("expected no-location error from share link resolution, got %v", err)
	}
}

// noNetTransport fails the test if any request is attempted — used to
// assert Canonicalize is purely local for the no-op cases.
type noNetTransport struct{ t *testing.T }

func (n *noNetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n.t.Errorf("unexpected network call to %s — Canonicalize should be I/O-free for canonical URLs", req.URL)
	return nil, errors.New("network not allowed in this test")
}

// shareLinkRedirectClient returns an http.Client that rewrites every
// request's Host/Scheme to point at the test server, so we can exercise
// Canonicalize with a URL claiming reddit.com host while actually
// hitting httptest.
func shareLinkRedirectClient(testServerBaseURL string) *http.Client {
	return &http.Client{
		Transport: &rewriteHostTransport{baseURL: testServerBaseURL},
	}
}

type rewriteHostTransport struct {
	baseURL string
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := neturl.Parse(t.baseURL)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}
