package reply

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func TestSaveRegenWritesUnderRegensDir(t *testing.T) {
	root := t.TempDir()
	s, err := NewSession(root, "shopify", "abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	gen := time.Date(2026, 5, 2, 21, 37, 10, 0, time.UTC)
	regen := &types.ReplyRegen{
		SessionID:        filepath.Base(s.Path()),
		Generated:        gen,
		Note:             "shorter",
		SourceDraftsPath: "drafts.json",
		Bundle: &types.ReplyBundle{
			Mode: types.ReplyModeReply,
			Drafts: []types.ReplyDraft{
				{ID: "best", Label: "best", Text: "x"},
			},
		},
	}
	rel, err := s.SaveRegen(regen)
	if err != nil {
		t.Fatal(err)
	}

	// Filename uses hyphens for time components — colons are unsafe on
	// Windows-side filesystems, and hyphens are still sortable.
	want := "regens/2026-05-02T21-37-10Z.json"
	if rel != want {
		t.Errorf("regen path = %q, want %q", rel, want)
	}

	full := filepath.Join(s.Path(), rel)
	if _, err := os.Stat(full); err != nil {
		t.Fatalf("regen file not created: %v", err)
	}

	// Round-trip the saved regen so we know schema didn't drift.
	var got types.ReplyRegen
	if err := s.LoadJSON(rel, &got); err != nil {
		t.Fatal(err)
	}
	if got.Note != "shorter" || got.SourceDraftsPath != "drafts.json" {
		t.Errorf("regen round-trip lost data: %+v", got)
	}
}

func TestSaveRegenDefaultsTimestampWhenZero(t *testing.T) {
	// Caller forgot to populate Generated — should fall back to
	// time.Now().UTC() rather than producing a "0001-01-01..." filename.
	root := t.TempDir()
	s, _ := NewSession(root, "x", "y", time.Now())

	rel, err := s.SaveRegen(&types.ReplyRegen{
		Note:   "n",
		Bundle: &types.ReplyBundle{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(rel, "regens/0001-") {
		t.Errorf("zero-time fallback failed; got %q", rel)
	}
}

func TestSaveRegenRejectsNil(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSession(root, "x", "y", time.Now())
	if _, err := s.SaveRegen(nil); err == nil {
		t.Error("expected error for nil regen")
	}
}

func TestAppendHistoryEventCreatesAndAppends(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSession(root, "x", "y", time.Now())

	t1 := time.Date(2026, 5, 2, 21, 37, 10, 0, time.UTC)
	t2 := t1.Add(5 * time.Minute)

	if err := s.AppendHistoryEvent(types.HistoryEvent{Type: "regen", Generated: t1, Note: "n1", Path: "regens/a.json"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendHistoryEvent(types.HistoryEvent{Type: "regen", Generated: t2, Note: "n2", Path: "regens/b.json"}); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(filepath.Join(s.Path(), "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var lines []types.HistoryEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev types.HistoryEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("non-JSON line in history.jsonl: %q", sc.Text())
		}
		lines = append(lines, ev)
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 history events, got %d", len(lines))
	}
	if lines[0].Note != "n1" || lines[1].Note != "n2" {
		t.Errorf("event order wrong: %+v", lines)
	}
	if lines[0].Type != "regen" {
		t.Errorf("event type = %q, want regen", lines[0].Type)
	}
}

func TestSaveRegenSameSecondNoOverwrite(t *testing.T) {
	// Two regens with identical Generated timestamps must not clobber
	// each other. Whole-second filename precision is fine for the
	// common case but breaks the auditable append-only contract when
	// runs land in the same second (codex P1 on PR #42). The collision
	// suffix (-2, -3, ...) preserves both artifacts.
	root := t.TempDir()
	s, err := NewSession(root, "shopify", "abc", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	gen := time.Date(2026, 5, 2, 21, 37, 10, 0, time.UTC)
	first := &types.ReplyRegen{
		SessionID: filepath.Base(s.Path()),
		Generated: gen,
		Note:      "first",
		Bundle: &types.ReplyBundle{
			Mode:   types.ReplyModeReply,
			Drafts: []types.ReplyDraft{{ID: "best", Label: "best", Text: "FIRST"}},
		},
	}
	second := &types.ReplyRegen{
		SessionID: filepath.Base(s.Path()),
		Generated: gen,
		Note:      "second",
		Bundle: &types.ReplyBundle{
			Mode:   types.ReplyModeReply,
			Drafts: []types.ReplyDraft{{ID: "best", Label: "best", Text: "SECOND"}},
		},
	}

	rel1, err := s.SaveRegen(first)
	if err != nil {
		t.Fatal(err)
	}
	rel2, err := s.SaveRegen(second)
	if err != nil {
		t.Fatal(err)
	}

	if rel1 == rel2 {
		t.Fatalf("same-second regens collided on %q — append-only contract broken", rel1)
	}

	// The first run gets the unsuffixed slot, the second run gets -2 —
	// matches the NewSession idiom for predictability.
	wantFirst := "regens/2026-05-02T21-37-10Z.json"
	wantSecond := "regens/2026-05-02T21-37-10Z-2.json"
	if rel1 != wantFirst {
		t.Errorf("first regen path = %q, want %q", rel1, wantFirst)
	}
	if rel2 != wantSecond {
		t.Errorf("second regen path = %q, want %q", rel2, wantSecond)
	}

	// Both artifacts must exist and carry the right content — proving
	// neither overwrote the other.
	var got1, got2 types.ReplyRegen
	if err := s.LoadJSON(rel1, &got1); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadJSON(rel2, &got2); err != nil {
		t.Fatal(err)
	}
	if got1.Bundle.Drafts[0].Text != "FIRST" || got2.Bundle.Drafts[0].Text != "SECOND" {
		t.Errorf("artifact contents wrong: rel1=%q rel2=%q", got1.Bundle.Drafts[0].Text, got2.Bundle.Drafts[0].Text)
	}

	// And a third run lands at -3, demonstrating the suffix counter
	// keeps incrementing.
	third := &types.ReplyRegen{
		SessionID: filepath.Base(s.Path()),
		Generated: gen,
		Note:      "third",
		Bundle:    &types.ReplyBundle{Mode: types.ReplyModeReply, Drafts: []types.ReplyDraft{{ID: "best", Text: "T"}}},
	}
	rel3, err := s.SaveRegen(third)
	if err != nil {
		t.Fatal(err)
	}
	if rel3 != "regens/2026-05-02T21-37-10Z-3.json" {
		t.Errorf("third regen path = %q, want -3 suffix", rel3)
	}
}

func TestSaveRegenDoesNotTouchOriginalDrafts(t *testing.T) {
	// v1 contract: regen never overwrites drafts.json. Write a sentinel
	// drafts.json, then save a regen, then verify drafts.json is byte-
	// identical to the sentinel.
	root := t.TempDir()
	s, _ := NewSession(root, "x", "y", time.Now())

	original := &types.ReplyBundle{
		Mode: types.ReplyModeReply,
		Drafts: []types.ReplyDraft{
			{ID: "best", Label: "best", Text: "ORIGINAL TEXT — must not change"},
		},
	}
	if err := s.SaveDrafts(original); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(s.Path(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.SaveRegen(&types.ReplyRegen{
		Generated: time.Now().UTC(),
		Note:      "n",
		Bundle: &types.ReplyBundle{
			Mode:   types.ReplyModeReply,
			Drafts: []types.ReplyDraft{{ID: "best", Label: "best", Text: "REGEN TEXT"}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(filepath.Join(s.Path(), "drafts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("drafts.json was modified by SaveRegen — v1 contract broken\nbefore: %s\nafter: %s", before, after)
	}
}
