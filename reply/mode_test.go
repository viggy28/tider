package reply

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
)

type fakeProvider struct {
	name     string
	response string
	err      error
	gotReq   llm.Request
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.gotReq = req
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.response, InputTokens: 50, OutputTokens: 30}, nil
}

func sampleThread() *types.Thread {
	return &types.Thread{
		Subreddit:   "EtsySellers",
		Title:       "Looking for feedback on my Etsy shop",
		Flair:       "feedback",
		Body:        "Here is my shop: https://my-shop.example.com — would love thoughts.",
		OutboundURL: "",
	}
}

func TestDetectModeReplyClassifier(t *testing.T) {
	const resp = `{"mode":"reply","reason":"OP asks for tooling opinions, not review of a specific resource","target_urls":[]}`
	p := &fakeProvider{name: "fake", response: resp}
	thread := &types.Thread{
		Subreddit: "golang",
		Title:     "What's everyone using for CDC these days?",
		Body:      "Curious about logical replication tooling in 2026.",
	}

	res, err := DetectMode(context.Background(), p, "gpt-4o-mini", thread, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != types.ReplyModeReply {
		t.Errorf("mode = %q, want reply", res.Mode)
	}
	if len(res.TargetURLs) != 0 {
		t.Errorf("reply mode should have empty target_urls, got %v", res.TargetURLs)
	}
	if !p.gotReq.JSONMode {
		t.Error("classifier call should have JSONMode=true")
	}
}

func TestDetectModeReviewClassifier(t *testing.T) {
	const resp = `{"mode":"review","reason":"OP explicitly asks for shop feedback","target_urls":["https://my-shop.example.com"]}`
	p := &fakeProvider{name: "fake", response: resp}
	thread := sampleThread()

	res, err := DetectMode(context.Background(), p, "gpt-4o-mini", thread, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != types.ReplyModeReview {
		t.Errorf("mode = %q, want review", res.Mode)
	}
	if len(res.TargetURLs) != 1 || res.TargetURLs[0] != "https://my-shop.example.com" {
		t.Errorf("target_urls: %v", res.TargetURLs)
	}
}

func TestDetectModeMergesExtractedURLs(t *testing.T) {
	// LLM returns one URL; body has another not mentioned by the model.
	// Both should appear in the merged result, model's first.
	const resp = `{"mode":"review","reason":"...","target_urls":["https://a.example.com"]}`
	p := &fakeProvider{name: "fake", response: resp}
	thread := &types.Thread{
		Subreddit:   "test",
		Title:       "Review my stuff",
		Body:        "main: https://a.example.com\nbackup: https://b.example.com",
		OutboundURL: "",
	}
	res, err := DetectMode(context.Background(), p, "x", thread, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TargetURLs) != 2 {
		t.Fatalf("expected 2 unique URLs, got %v", res.TargetURLs)
	}
	if res.TargetURLs[0] != "https://a.example.com" {
		t.Errorf("LLM-picked URL should rank first: got %v", res.TargetURLs)
	}
	if res.TargetURLs[1] != "https://b.example.com" {
		t.Errorf("second should be the body-extracted URL: got %v", res.TargetURLs)
	}
}

func TestDetectModePromptIncludesOPFieldsOnly(t *testing.T) {
	p := &fakeProvider{name: "fake", response: `{"mode":"reply","reason":"x","target_urls":[]}`}
	thread := &types.Thread{
		Subreddit: "shopify",
		Title:     "best plugins?",
		Flair:     "discussion",
		Body:      "what do folks use",
		// Comments must not be in the prompt.
		Comments: []types.Comment{
			{Author: "alice", Body: "I'd love feedback on my shop too: https://other.example.com"},
		},
	}
	_, err := DetectMode(context.Background(), p, "x", thread, 0)
	if err != nil {
		t.Fatal(err)
	}
	prompt := p.gotReq.Messages[0].Content
	if !strings.Contains(prompt, "best plugins?") {
		t.Error("prompt missing title")
	}
	if !strings.Contains(prompt, "discussion") {
		t.Error("prompt missing flair")
	}
	if !strings.Contains(prompt, "what do folks use") {
		t.Error("prompt missing body")
	}
	if strings.Contains(prompt, "feedback on my shop too") {
		t.Error("prompt leaked comment content (mode detection must use OP only)")
	}
	if strings.Contains(prompt, "other.example.com") {
		t.Error("prompt leaked URL from a comment")
	}
}

func TestDetectModeFiltersRedditAndImageURLs(t *testing.T) {
	const resp = `{"mode":"review","reason":"...","target_urls":[]}`
	p := &fakeProvider{name: "fake", response: resp}
	thread := &types.Thread{
		Subreddit: "test",
		Title:     "review",
		Body: `look at https://my-real-shop.example.com/store
and screenshot https://i.imgur.com/abc.png
and earlier thread https://reddit.com/r/foo/comments/x/y/`,
	}
	res, err := DetectMode(context.Background(), p, "x", thread, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TargetURLs) != 1 {
		t.Fatalf("expected 1 URL after filtering, got %v", res.TargetURLs)
	}
	if !strings.Contains(res.TargetURLs[0], "my-real-shop.example.com") {
		t.Errorf("wrong URL kept: %s", res.TargetURLs[0])
	}
}

func TestDetectModeRejectsInvalidModeValue(t *testing.T) {
	const resp = `{"mode":"weirdo","reason":"x","target_urls":[]}`
	p := &fakeProvider{name: "fake", response: resp}
	_, err := DetectMode(context.Background(), p, "x", sampleThread(), 0)
	if err == nil || !strings.Contains(err.Error(), "invalid mode") {
		t.Errorf("expected invalid-mode error, got %v", err)
	}
}

func TestDetectModePropagatesProviderError(t *testing.T) {
	p := &fakeProvider{name: "fake", err: errors.New("rate limited")}
	_, err := DetectMode(context.Background(), p, "x", sampleThread(), 0)
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected provider error, got %v", err)
	}
}

func TestDetectModeRejectsBadJSON(t *testing.T) {
	p := &fakeProvider{name: "fake", response: "not json"}
	_, err := DetectMode(context.Background(), p, "x", sampleThread(), 0)
	if err == nil || !strings.Contains(err.Error(), "parse json") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestExtractURLsBasicShapes(t *testing.T) {
	thread := &types.Thread{
		OutboundURL: "https://outbound.example.com/post",
		Body: `markdown link: [my shop](https://shop.example.com)
raw url: https://docs.example.com/guide
also a deep link https://blog.example.com/2026/04/post.html`,
	}
	urls := extractURLs(thread)
	want := []string{
		"https://outbound.example.com/post",
		"https://shop.example.com",
		"https://docs.example.com/guide",
		"https://blog.example.com/2026/04/post.html",
	}
	if len(urls) != len(want) {
		t.Fatalf("got %v, want %v", urls, want)
	}
	for i, w := range want {
		if urls[i] != w {
			t.Errorf("position %d: got %q, want %q", i, urls[i], w)
		}
	}
}

func TestMergeTargetURLsDedup(t *testing.T) {
	// Both classifier URLs appear in fallback (i.e. were extracted from
	// the OP body) — they're "grounded" and rank first, in classifier
	// order. fallback contributes c.example.com which the LLM didn't
	// pick — appended after grounded.
	primary := []string{"https://A.example.com", "https://b.example.com"}
	fallback := []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"}
	got := mergeTargetURLs(primary, fallback)
	if len(got) != 3 {
		t.Fatalf("expected 3, got %v", got)
	}
	// Grounded classifier picks (case from primary preserved), then the
	// extra fallback URL.
	if got[0] != "https://A.example.com" || got[1] != "https://b.example.com" || got[2] != "https://c.example.com" {
		t.Errorf("dedup result: %v", got)
	}
}

func TestMergeTargetURLsDemotesHallucinatedClassifierURLs(t *testing.T) {
	// Classifier returned two URLs:
	//   - https://real-shop.example.com   IS in fallback (came from OP)
	//   - https://invented.example.com    NOT in fallback (hallucinated)
	// fallback also has https://other.example.com that the classifier
	// didn't pick. Expected order:
	//   1. real-shop  (grounded — passed verification)
	//   2. other      (extra fallback URL)
	//   3. invented   (ungrounded — kept but demoted, so [0] is safe to inspect)
	primary := []string{"https://invented.example.com", "https://real-shop.example.com"}
	fallback := []string{"https://real-shop.example.com", "https://other.example.com"}
	got := mergeTargetURLs(primary, fallback)
	if len(got) != 3 {
		t.Fatalf("expected 3 URLs, got %v", got)
	}
	if got[0] != "https://real-shop.example.com" {
		t.Errorf("hallucinated URL not demoted: got[0] = %q", got[0])
	}
	if got[1] != "https://other.example.com" {
		t.Errorf("extra fallback URL position wrong: got[1] = %q", got[1])
	}
	if got[2] != "https://invented.example.com" {
		t.Errorf("hallucinated URL not last: got[2] = %q", got[2])
	}
}

func TestMergeTargetURLsPreservesFallbackOrderWhenClassifierEmpty(t *testing.T) {
	// Codex flagged this: previous code alphabetically sorted fallback
	// URLs when the classifier returned nothing. In review mode the CLI
	// inspects TargetURLs[0] — so alpha-sort would pick "about" before
	// "shop" and inspect the wrong page. Body order is itself a signal
	// (the user mentioned shop first), so preserve insertion order.
	primary := []string{}
	fallback := []string{
		"https://shop.example.com",
		"https://about.example.com",
		"https://docs.example.com",
	}
	got := mergeTargetURLs(primary, fallback)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "https://shop.example.com" {
		t.Errorf("body order should be preserved: got[0] = %q (want shop)", got[0])
	}
	if got[1] != "https://about.example.com" || got[2] != "https://docs.example.com" {
		t.Errorf("rest of order changed: %v", got)
	}
}

func TestMergeTargetURLsAllHallucinatedFallbackEmpty(t *testing.T) {
	// Edge case: classifier returned URLs but fallback is empty (the OP
	// body had no parseable URLs). Without verification info we have to
	// trust the classifier — preserve original order.
	primary := []string{"https://a.example.com", "https://b.example.com"}
	got := mergeTargetURLs(primary, nil)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got[0] != "https://a.example.com" || got[1] != "https://b.example.com" {
		t.Errorf("classifier order should be preserved when fallback is empty: %v", got)
	}
}
