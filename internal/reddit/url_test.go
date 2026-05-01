package reddit

import (
	"context"
	"net/http"
	"net/http/httptest"
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
