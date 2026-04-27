package reddit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const aboutJSON = `{"kind":"t5","data":{"display_name":"golang","subscribers":250000,"public_description":"Go community","over18":false,"url":"/r/golang/"}}`

const rulesJSON = `{"rules":[{"short_name":"No spam","description":"do not spam","kind":"link","priority":0}]}`

const wikiJSON = `{"data":{"content_md":"# Detailed rules\n\n* No spam"}}`

const listingJSON = `{"kind":"Listing","data":{"children":[
  {"kind":"t3","data":{"id":"abc","title":"Hello","author":"alice","score":100,"num_comments":5,"is_self":true,"selftext":"hi","stickied":false,"link_flair_text":"discussion","created_utc":1700000000.0,"permalink":"/r/golang/comments/abc/hello/","url":"https://reddit.com/r/golang/comments/abc/hello/"}},
  {"kind":"t3","data":{"id":"def","title":"Sticky announcement","stickied":true,"created_utc":1700000100.0}}
]}}`

const flairsJSON = `[{"id":"f1","text":"discussion","text_editable":false},{"id":"f2","text":"show & tell","text_editable":false}]`

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(NewCache(t.TempDir()))
	c.BaseURL = srv.URL
	c.MaxRetry = 2
	c.BaseDelay = time.Millisecond
	return c
}

func TestClientAboutCachesAndRefreshes(t *testing.T) {
	var hits int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != DefaultUserAgent {
			t.Errorf("user-agent = %q, want %q", got, DefaultUserAgent)
		}
		if r.URL.Path != "/r/golang/about.json" {
			t.Errorf("path = %q", r.URL.Path)
		}
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(aboutJSON))
	})
	c := newTestClient(t, h)

	sub, err := c.About(context.Background(), "golang", false)
	if err != nil {
		t.Fatalf("about: %v", err)
	}
	if sub.Name != "golang" || sub.Subscribers != 250000 {
		t.Fatalf("parsed: %+v", sub)
	}
	// second call: cache hit, no new HTTP
	if _, err := c.About(context.Background(), "golang", false); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("hits after cache hit = %d, want 1", got)
	}
	// refresh: forces fresh fetch
	if _, err := c.About(context.Background(), "golang", true); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("hits after refresh = %d, want 2", got)
	}
}

func TestClientRetriesOn429(t *testing.T) {
	var calls int32
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(aboutJSON))
	})
	c := newTestClient(t, h)
	if _, err := c.About(context.Background(), "golang", false); err != nil {
		t.Fatalf("about: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("calls = %d, want 3", got)
	}
}

func TestClientRules(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/about/rules.json") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(rulesJSON))
	})
	c := newTestClient(t, h)
	rules, err := c.Rules(context.Background(), "golang", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ShortName != "No spam" {
		t.Fatalf("rules = %+v", rules)
	}
}

func TestClientWikiSoftFails404(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := newTestClient(t, h)
	s, err := c.WikiRules(context.Background(), "golang", false)
	if err != nil {
		t.Fatalf("expected soft fail, got %v", err)
	}
	if s != "" {
		t.Fatalf("expected empty, got %q", s)
	}
}

func TestClientWikiSuccess(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(wikiJSON))
	})
	c := newTestClient(t, h)
	s, err := c.WikiRules(context.Background(), "golang", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "Detailed rules") {
		t.Fatalf("wiki content = %q", s)
	}
}

func TestClientFlairsSoftFails403(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	c := newTestClient(t, h)
	flairs, err := c.Flairs(context.Background(), "golang", false)
	if err != nil {
		t.Fatalf("expected soft fail, got %v", err)
	}
	if flairs != nil {
		t.Fatalf("expected nil, got %+v", flairs)
	}
}

func TestClientFlairsAuthEnvelope(t *testing.T) {
	// Reddit returns 200 with a json-errors envelope when auth is required.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"json":{"errors":[["USER_REQUIRED","Please log in to do that.",null]]}}`))
	})
	c := newTestClient(t, h)
	flairs, err := c.Flairs(context.Background(), "golang", false)
	if err != nil {
		t.Fatalf("expected soft fail, got %v", err)
	}
	if flairs != nil {
		t.Fatalf("expected nil, got %+v", flairs)
	}
}

func TestClientFlairsSuccess(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(flairsJSON))
	})
	c := newTestClient(t, h)
	flairs, err := c.Flairs(context.Background(), "golang", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(flairs) != 2 || flairs[0].Text != "discussion" {
		t.Fatalf("flairs = %+v", flairs)
	}
}

func TestClientHotAndStickies(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(listingJSON))
	})
	c := newTestClient(t, h)
	hot, err := c.Hot(context.Background(), "golang", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(hot) != 2 {
		t.Fatalf("hot len = %d", len(hot))
	}
	stickies := Stickies(hot)
	if len(stickies) != 1 || stickies[0].ID != "def" {
		t.Fatalf("stickies = %+v", stickies)
	}
}

func TestClientTopPeriodInQuery(t *testing.T) {
	var captured string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.URL.RawQuery
		_, _ = w.Write([]byte(listingJSON))
	})
	c := newTestClient(t, h)
	if _, err := c.Top(context.Background(), "golang", "week", false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(captured, "t=week") {
		t.Fatalf("query = %q, missing t=week", captured)
	}
}

func TestClientTopUnknownPeriod(t *testing.T) {
	c := newTestClient(t, http.NewServeMux())
	if _, err := c.Top(context.Background(), "golang", "yearly", false); err == nil {
		t.Fatal("expected error for unknown period")
	}
}

func TestClientPropagatesHardError(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := newTestClient(t, h)
	if _, err := c.About(context.Background(), "doesnotexist", false); err == nil {
		t.Fatal("expected error on 404 for non-soft endpoint")
	}
}
