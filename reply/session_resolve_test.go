package reply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func TestResolveSessionExactMatch(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	s, err := NewSession(root, "shopify", "abc123", now)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveSession(root, filepath.Base(s.Path()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Path() != s.Path() {
		t.Errorf("got %s, want %s", got.Path(), s.Path())
	}
}

func TestResolveSessionSubstringMatch(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	s, err := NewSession(root, "shopify", "xyz789", now)
	if err != nil {
		t.Fatal(err)
	}

	// Substring match — user remembers just the postID, which lives at
	// the end of the canonical slug (date-sub-postid), so a strict
	// prefix rule would reject this common case.
	got, err := ResolveSession(root, "xyz789")
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
	if got.Path() != s.Path() {
		t.Errorf("got %s, want %s", got.Path(), s.Path())
	}
}

func TestResolveSessionAmbiguousErrors(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	if _, err := NewSession(root, "shopify", "abc123", now); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSession(root, "shopify", "abc123", now); err != nil {
		t.Fatal(err)
	}

	// Two same-day collisions produce abc123 and abc123-2 — the
	// substring "shopify" matches both.
	_, err := ResolveSession(root, "shopify")
	if err == nil {
		t.Fatal("expected ambiguous-match error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("error should mention ambiguity, got: %v", err)
	}
}

func TestResolveSessionNoMatch(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)
	if _, err := NewSession(root, "shopify", "abc123", now); err != nil {
		t.Fatal(err)
	}

	_, err := ResolveSession(root, "definitely-not-here")
	if err == nil {
		t.Fatal("expected no-match error")
	}
}

func TestResolveSessionEmptyID(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveSession(root, "")
	if err == nil {
		t.Fatal("expected error for empty id")
	}
	_, err = ResolveSession(root, "   ")
	if err == nil {
		t.Fatal("expected error for whitespace-only id")
	}
}

func TestResolveSessionMissingRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := ResolveSession(missing, "anything")
	if err == nil {
		t.Fatal("expected error when root does not exist")
	}
}

func TestResolveSessionRejectsPathTraversal(t *testing.T) {
	// Defense against the path-traversal class of inputs. Without the
	// guard, filepath.Join collapses ".." segments and the exact-match
	// branch would happily Stat outside the sessions root — letting
	// `tider reply post ../../foo` operate on arbitrary directories.
	root := t.TempDir()
	if _, err := NewSession(root, "shopify", "abc123", time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"../foo",
		"../../etc",
		"foo/bar",
		"foo\\bar",
		"..",
		"foo/../bar",
		"/absolute/path",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			_, err := ResolveSession(root, in)
			if err == nil {
				t.Errorf("expected error for traversal-y input %q", in)
				return
			}
			if !strings.Contains(err.Error(), "invalid session id") {
				t.Errorf("expected invalid-session-id error, got %v", err)
			}
		})
	}
}

func TestSessionStatusProgression(t *testing.T) {
	root := t.TempDir()
	s, err := NewSession(root, "shopify", "abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// Empty session — no drafts.json yet — counts as failed.
	if got := s.Status(); got != SessionStatusFailed {
		t.Errorf("empty session status = %s, want %s", got, SessionStatusFailed)
	}

	// drafted: drafts.json present.
	if err := s.SaveDrafts(&types.ReplyBundle{Mode: types.ReplyModeReply}); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(); got != SessionStatusDrafted {
		t.Errorf("after drafts.json, status = %s, want %s", got, SessionStatusDrafted)
	}

	// posted: post.json present.
	if err := s.SavePost(&types.ReplyPost{SessionID: filepath.Base(s.Path()), FinalText: "hi", PostedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(); got != SessionStatusPosted {
		t.Errorf("after post.json, status = %s, want %s", got, SessionStatusPosted)
	}

	// outcome-recorded: outcome.json present (highest priority).
	if err := s.SaveOutcome(&types.ReplyOutcome{SessionID: filepath.Base(s.Path()), CheckedAt: time.Now(), LandedState: "landed", KovaSignal: "neutral"}); err != nil {
		t.Fatal(err)
	}
	if got := s.Status(); got != SessionStatusOutcomeRecorded {
		t.Errorf("after outcome.json, status = %s, want %s", got, SessionStatusOutcomeRecorded)
	}
}

func TestListSessionsNewestFirstAndLimit(t *testing.T) {
	root := t.TempDir()

	// Create three sessions with separate mtimes so ordering is
	// deterministic regardless of filesystem timestamp granularity.
	mkSession := func(sub, postID string, mtime time.Time) string {
		s, err := NewSession(root, sub, postID, mtime)
		if err != nil {
			t.Fatal(err)
		}
		// Stamp the directory mtime explicitly — NewSession sets it to
		// "now" implicitly, which collapses on fast machines.
		if err := os.Chtimes(s.Path(), mtime, mtime); err != nil {
			t.Fatal(err)
		}
		return filepath.Base(s.Path())
	}

	t1 := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	oldest := mkSession("a", "p1", t1)
	mid := mkSession("b", "p2", t2)
	newest := mkSession("c", "p3", t3)

	got, err := ListSessions(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d sessions, want 3", len(got))
	}
	if filepath.Base(got[0].Path()) != newest {
		t.Errorf("newest first: got[0] = %s, want %s", filepath.Base(got[0].Path()), newest)
	}
	if filepath.Base(got[2].Path()) != oldest {
		t.Errorf("oldest last: got[2] = %s, want %s", filepath.Base(got[2].Path()), oldest)
	}

	// Limit=2 returns only the two newest, dropping the oldest.
	got2, err := ListSessions(root, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got2) != 2 {
		t.Fatalf("limit=2: got %d, want 2", len(got2))
	}
	if filepath.Base(got2[0].Path()) != newest || filepath.Base(got2[1].Path()) != mid {
		t.Errorf("limit=2 ordering wrong: %s, %s", filepath.Base(got2[0].Path()), filepath.Base(got2[1].Path()))
	}
}

func TestListSessionsEmptyRoot(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-sessions-yet")
	got, err := ListSessions(missing, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("missing root should return nil, got %v", got)
	}
}

func TestSavePostAndOutcomeRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := NewSession(root, "shopify", "abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	post := &types.ReplyPost{
		SessionID: filepath.Base(s.Path()),
		ThreadURL: "https://reddit.com/r/shopify/comments/abc/x",
		FinalText: "the actual posted reply\n\nwith blank lines",
		PostedAt:  time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC),
		Feedback:  []string{"shorter", "added-personal-opener"},
		Note:      "removed the checklist at the end",
	}
	if err := s.SavePost(post); err != nil {
		t.Fatal(err)
	}

	var got types.ReplyPost
	if err := s.LoadJSON("post.json", &got); err != nil {
		t.Fatal(err)
	}
	if got.FinalText != post.FinalText {
		t.Errorf("final_text round-trip lost data: %q", got.FinalText)
	}
	if len(got.Feedback) != 2 || got.Feedback[0] != "shorter" {
		t.Errorf("feedback round-trip wrong: %v", got.Feedback)
	}

	outcome := &types.ReplyOutcome{
		SessionID:              filepath.Base(s.Path()),
		ThreadURL:              post.ThreadURL,
		PostedAt:               post.PostedAt,
		CheckedAt:              time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC),
		Upvotes:                12,
		OPReplied:              true,
		OtherCommentEngagement: false,
		LandedState:            "landed",
		KovaSignal:             "helped",
		Note:                   "OP asked a follow-up about pricing",
	}
	if err := s.SaveOutcome(outcome); err != nil {
		t.Fatal(err)
	}

	var gotOutcome types.ReplyOutcome
	if err := s.LoadJSON("outcome.json", &gotOutcome); err != nil {
		t.Fatal(err)
	}
	if gotOutcome.Upvotes != 12 || gotOutcome.LandedState != "landed" || gotOutcome.KovaSignal != "helped" {
		t.Errorf("outcome round-trip wrong: %+v", gotOutcome)
	}
}
