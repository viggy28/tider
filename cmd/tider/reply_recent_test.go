package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/reply"
)

func TestBuildRecentRowDraftedNoMode(t *testing.T) {
	// Drafted-state session (drafts.json present, no post.json). Should
	// surface mode + sub + title from the session files, status=drafted,
	// no outcome-due annotation.
	root := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s, err := reply.NewSession(root, "shopify", "abc123", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThread(&types.Thread{Subreddit: "shopify", PostID: "abc123", Title: "Help with my store"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMode(&types.ReplyModeResult{Mode: types.ReplyModeReply}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDrafts(&types.ReplyBundle{Mode: types.ReplyModeReply, Generated: now}); err != nil {
		t.Fatal(err)
	}

	row := buildRecentRow(s, now.Add(2*time.Hour))
	if row.status != string(reply.SessionStatusDrafted) {
		t.Errorf("status = %q, want drafted", row.status)
	}
	if row.sub != "r/shopify" {
		t.Errorf("sub = %q, want r/shopify", row.sub)
	}
	if row.mode != "reply" {
		t.Errorf("mode = %q, want reply", row.mode)
	}
	if !strings.Contains(row.title, "Help with my store") {
		t.Errorf("title = %q, want containing 'Help with my store'", row.title)
	}
	if row.age != "2h" {
		t.Errorf("age = %q, want 2h", row.age)
	}
}

func TestBuildRecentRowFailedSession(t *testing.T) {
	// Session dir exists (e.g. review-mode bailed at inspection) with
	// only thread.json + mode.json. No drafts.json → status=failed.
	root := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s, err := reply.NewSession(root, "shopify", "fail1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThread(&types.Thread{Subreddit: "shopify", PostID: "fail1", Title: "T"}); err != nil {
		t.Fatal(err)
	}

	row := buildRecentRow(s, now)
	if row.status != string(reply.SessionStatusFailed) {
		t.Errorf("status = %q, want failed", row.status)
	}
}

func TestBuildRecentRowOutcomeDueAfter48h(t *testing.T) {
	root := t.TempDir()
	postedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s, err := reply.NewSession(root, "shopify", "old1", postedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveThread(&types.Thread{Subreddit: "shopify", PostID: "old1", Title: "T"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDrafts(&types.ReplyBundle{Mode: types.ReplyModeReply, Generated: postedAt}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePost(&types.ReplyPost{
		SessionID: filepath.Base(s.Path()),
		FinalText: "x",
		PostedAt:  postedAt,
	}); err != nil {
		t.Fatal(err)
	}

	// 50 hours later — past the 48h soak window.
	row := buildRecentRow(s, postedAt.Add(50*time.Hour))
	if !strings.Contains(row.status, "outcome-due") {
		t.Errorf("expected outcome-due annotation after 48h, got %q", row.status)
	}
}

func TestBuildRecentRowPostedNotYetDue(t *testing.T) {
	root := t.TempDir()
	postedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s, err := reply.NewSession(root, "shopify", "fresh1", postedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDrafts(&types.ReplyBundle{Mode: types.ReplyModeReply, Generated: postedAt}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePost(&types.ReplyPost{
		SessionID: filepath.Base(s.Path()),
		FinalText: "x",
		PostedAt:  postedAt,
	}); err != nil {
		t.Fatal(err)
	}

	// 12h later — well within the soak window, no annotation.
	row := buildRecentRow(s, postedAt.Add(12*time.Hour))
	if strings.Contains(row.status, "outcome-due") {
		t.Errorf("did not expect outcome-due before 48h, got %q", row.status)
	}
	if row.status != string(reply.SessionStatusPosted) {
		t.Errorf("status = %q, want posted", row.status)
	}
}

func TestBuildRecentRowOutcomeRecordedNoAnnotation(t *testing.T) {
	// outcome-recorded should not get the outcome-due annotation even
	// if the post is old — the loop is closed.
	root := t.TempDir()
	postedAt := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	s, err := reply.NewSession(root, "shopify", "done1", postedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDrafts(&types.ReplyBundle{Mode: types.ReplyModeReply, Generated: postedAt}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePost(&types.ReplyPost{SessionID: filepath.Base(s.Path()), FinalText: "x", PostedAt: postedAt}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOutcome(&types.ReplyOutcome{
		SessionID:   filepath.Base(s.Path()),
		PostedAt:    postedAt,
		CheckedAt:   postedAt.Add(72 * time.Hour),
		LandedState: "landed",
		KovaSignal:  "neutral",
	}); err != nil {
		t.Fatal(err)
	}

	row := buildRecentRow(s, postedAt.Add(72*time.Hour))
	if row.status != string(reply.SessionStatusOutcomeRecorded) {
		t.Errorf("status = %q, want outcome-recorded", row.status)
	}
}

func TestHumanAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m"},
		{59 * time.Minute, "59m"},
		{1 * time.Hour, "1h"},
		{23 * time.Hour, "23h"},
		{24 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}
	for _, c := range cases {
		if got := humanAge(c.d); got != c.want {
			t.Errorf("humanAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 60, "short"},
		{"a long title that should be truncated at the boundary", 20, "a long title that s…"},
		{"with\ttab", 60, "with tab"}, // tabs replaced for tabwriter safety
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
