package reddit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// realistic-ish Reddit JSON for a self post with nested comments. Trimmed
// to the fields parseThread reads.
const sampleThreadJSON = `[
  {
    "kind": "Listing",
    "data": {
      "children": [
        {
          "kind": "t3",
          "data": {
            "id": "abc123",
            "title": "Looking for feedback on my Etsy shop",
            "selftext": "Here is the link: https://my-shop.example.com — would love thoughts.",
            "author": "shopowner",
            "subreddit": "EtsySellers",
            "link_flair_text": "feedback",
            "url": "https://www.reddit.com/r/EtsySellers/comments/abc123/...",
            "is_self": true,
            "permalink": "/r/EtsySellers/comments/abc123/looking-for-feedback/"
          }
        }
      ]
    }
  },
  {
    "kind": "Listing",
    "data": {
      "children": [
        {
          "kind": "t1",
          "data": {
            "id": "c1",
            "parent_id": "t3_abc123",
            "author": "alice",
            "body": "Top-level comment, score 100",
            "score": 100,
            "created_utc": 1700000000.0,
            "replies": {
              "kind": "Listing",
              "data": {
                "children": [
                  {
                    "kind": "t1",
                    "data": {
                      "id": "c1r1",
                      "parent_id": "t1_c1",
                      "author": "bob",
                      "body": "Nested reply, score 200",
                      "score": 200,
                      "created_utc": 1700000010.0,
                      "replies": ""
                    }
                  }
                ]
              }
            }
          }
        },
        {
          "kind": "t1",
          "data": {
            "id": "c2",
            "parent_id": "t3_abc123",
            "author": "carol",
            "body": "Another top-level, score 50",
            "score": 50,
            "created_utc": 1700000020.0,
            "replies": ""
          }
        },
        {
          "kind": "t1",
          "data": {
            "id": "c3deleted",
            "parent_id": "t3_abc123",
            "author": "[deleted]",
            "body": "[deleted]",
            "score": 5,
            "created_utc": 1700000030.0,
            "replies": ""
          }
        },
        {
          "kind": "more",
          "data": {"count": 50}
        }
      ]
    }
  }
]`

func TestFetchThreadParsesPostAndComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/comments/abc123.json") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("raw_json") != "1" {
			t.Errorf("expected raw_json=1 query, got %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(sampleThreadJSON))
	}))
	defer srv.Close()

	c := NewClient(NewCache(t.TempDir()))
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL
	c.MaxRetry = 0

	thread, err := c.FetchThread(context.Background(), "EtsySellers", "abc123")
	if err != nil {
		t.Fatal(err)
	}

	if thread.PostID != "abc123" {
		t.Errorf("post id: %q", thread.PostID)
	}
	if thread.Subreddit != "EtsySellers" {
		t.Errorf("subreddit: %q", thread.Subreddit)
	}
	if !strings.Contains(thread.Title, "feedback on my Etsy shop") {
		t.Errorf("title: %q", thread.Title)
	}
	if !strings.Contains(thread.Body, "https://my-shop.example.com") {
		t.Errorf("body did not preserve outbound link in selftext: %q", thread.Body)
	}
	if thread.Flair != "feedback" {
		t.Errorf("flair: %q", thread.Flair)
	}
	if thread.OutboundURL != "" {
		t.Errorf("self post should have empty OutboundURL, got %q", thread.OutboundURL)
	}
	if thread.URL != "https://www.reddit.com/r/EtsySellers/comments/abc123/looking-for-feedback/" {
		t.Errorf("URL not built from permalink: %q", thread.URL)
	}
	if thread.FetchedAt.IsZero() {
		t.Error("FetchedAt not set")
	}
}

func TestFetchThreadFlattensCommentsByScore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sampleThreadJSON))
	}))
	defer srv.Close()
	c := NewClient(NewCache(t.TempDir()))
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL

	thread, err := c.FetchThread(context.Background(), "EtsySellers", "abc123")
	if err != nil {
		t.Fatal(err)
	}

	// 3 valid comments in fixture: c1 (100), c1r1 (200, nested), c2 (50).
	// c3deleted is body="[deleted]" → skipped.
	// Should be sorted by score desc: c1r1, c1, c2
	if len(thread.Comments) != 3 {
		t.Fatalf("expected 3 comments, got %d: %+v", len(thread.Comments), thread.Comments)
	}
	if thread.Comments[0].ID != "c1r1" {
		t.Errorf("highest-score comment should be c1r1 (200): got %+v", thread.Comments[0])
	}
	if thread.Comments[1].ID != "c1" {
		t.Errorf("second should be c1 (100): got %+v", thread.Comments[1])
	}
	if thread.Comments[2].ID != "c2" {
		t.Errorf("third should be c2 (50): got %+v", thread.Comments[2])
	}
	// Nested comment retains its parent_id pointing at t1_c1
	if thread.Comments[0].ParentID != "t1_c1" {
		t.Errorf("nested comment parent_id lost: %q", thread.Comments[0].ParentID)
	}
}

func TestFetchThreadGlobalCommentsPathWhenSubEmpty(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_, _ = w.Write([]byte(sampleThreadJSON))
	}))
	defer srv.Close()
	c := NewClient(NewCache(t.TempDir()))
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL

	thread, err := c.FetchThread(context.Background(), "", "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if seenPath != "/comments/abc123.json" {
		t.Errorf("expected global path /comments/<id>.json, got %s", seenPath)
	}
	// Subreddit still populated from the response payload.
	if thread.Subreddit != "EtsySellers" {
		t.Errorf("subreddit not filled from response: %q", thread.Subreddit)
	}
}

func TestFetchThreadLinkPostOutboundURL(t *testing.T) {
	const linkPostJSON = `[
      {"kind":"Listing","data":{"children":[{"kind":"t3","data":{
        "id":"link1","title":"check this out","selftext":"","author":"u",
        "subreddit":"webdev","link_flair_text":"","url":"https://example.com/article",
        "is_self":false,"permalink":"/r/webdev/comments/link1/check-this-out/"
      }}]}},
      {"kind":"Listing","data":{"children":[]}}
    ]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(linkPostJSON))
	}))
	defer srv.Close()
	c := NewClient(NewCache(t.TempDir()))
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL

	thread, err := c.FetchThread(context.Background(), "webdev", "link1")
	if err != nil {
		t.Fatal(err)
	}
	if thread.OutboundURL != "https://example.com/article" {
		t.Errorf("link-post OutboundURL wrong: %q", thread.OutboundURL)
	}
}

func TestFetchThreadCapsAt20Comments(t *testing.T) {
	// Build a fixture with 30 top-level comments at varying scores.
	var sb strings.Builder
	sb.WriteString(`[{"kind":"Listing","data":{"children":[{"kind":"t3","data":{"id":"x","title":"t","selftext":"","author":"u","subreddit":"s","link_flair_text":"","url":"u","is_self":true,"permalink":"/r/s/comments/x/t/"}}]}},`)
	sb.WriteString(`{"kind":"Listing","data":{"children":[`)
	for i := 0; i < 30; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		// Make every comment have a unique score so sort is deterministic.
		score := 100 - i
		sb.WriteString(`{"kind":"t1","data":{"id":"c` + itoa(i) + `","parent_id":"t3_x","author":"a","body":"b","score":` + itoa(score) + `,"created_utc":0,"replies":""}}`)
	}
	sb.WriteString(`]}}]`)
	body := sb.String()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	c := NewClient(NewCache(t.TempDir()))
	c.HTTP = srv.Client()
	c.BaseURL = srv.URL

	thread, err := c.FetchThread(context.Background(), "s", "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Comments) != 20 {
		t.Errorf("expected 20 comments after cap, got %d", len(thread.Comments))
	}
	// Highest-score comment is c0 (score 100).
	if thread.Comments[0].ID != "c0" {
		t.Errorf("top comment after sort: %q (want c0)", thread.Comments[0].ID)
	}
}

// minimal local itoa to avoid pulling strconv into test boilerplate.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func TestParseThreadBadShape(t *testing.T) {
	_, err := parseThread([]byte(`{"not":"an array"}`))
	if err == nil || !strings.Contains(err.Error(), "top-level") {
		t.Errorf("expected top-level parse error, got %v", err)
	}

	_, err = parseThread([]byte(`[{}]`))
	if err == nil || !strings.Contains(err.Error(), "expected 2 listings") {
		t.Errorf("expected listings-count error, got %v", err)
	}
}
