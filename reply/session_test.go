package reply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func TestNewSessionPathFormat(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 4, 30, 22, 22, 0, 0, time.UTC)

	s, err := NewSession(root, "Shopify", "1t06474", now)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "2026-04-30-shopify-1t06474")
	if s.Path() != want {
		t.Errorf("path = %q, want %q", s.Path(), want)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Errorf("session dir not created: %v", err)
	}
}

func TestNewSessionLowercasesSubAndPostID(t *testing.T) {
	root := t.TempDir()
	s, err := NewSession(root, "GOLANG", "ABC123", time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(s.Path(), "2026-04-30-golang-abc123") {
		t.Errorf("expected lowercased slug, got %s", s.Path())
	}
}

func TestNewSessionEmptySubFallsBackToUnknown(t *testing.T) {
	// redd.it short links don't carry the subreddit until the thread
	// fetch returns it — but session creation can happen earlier in some
	// flows. Allow empty sub with an explicit fallback.
	root := t.TempDir()
	s, err := NewSession(root, "", "abc123", time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s.Path(), "unknown") {
		t.Errorf("empty sub should fall back to 'unknown', got %s", s.Path())
	}
}

func TestNewSessionRequiresPostID(t *testing.T) {
	root := t.TempDir()
	_, err := NewSession(root, "x", "", time.Now())
	if err == nil || !strings.Contains(err.Error(), "postID required") {
		t.Errorf("expected postID-required error, got %v", err)
	}
	_, err = NewSession(root, "x", "   ", time.Now())
	if err == nil {
		t.Errorf("whitespace-only postID should error")
	}
}

func TestWriteJSONRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := NewSession(root, "test", "x1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	in := map[string]any{"hello": "world", "n": 42.0}
	if err := s.WriteJSON("test.json", in); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(s.Path(), "test.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out["hello"] != "world" || out["n"] != 42.0 {
		t.Errorf("round-trip lost data: %+v", out)
	}
}

func TestWriteJSONAtomicLeavesNoTempFile(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSession(root, "test", "x1", time.Now())
	if err := s.WriteJSON("a.json", "hello"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf(".tmp file lingered: %s", e.Name())
		}
	}
}

func TestWriteMarkdownAtomic(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSession(root, "test", "x1", time.Now())
	body := "# Hello\n\nA markdown file.\n"
	if err := s.WriteMarkdown("output.md", body); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Path(), "output.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Errorf("body changed: %q", string(data))
	}
}

func TestSaveTypedHelpers(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSession(root, "test", "x1", time.Now())

	thread := &types.Thread{Subreddit: "test", PostID: "x1", Title: "T"}
	mode := &types.ReplyModeResult{Mode: types.ReplyModeReply, Reason: "r"}
	contexts := []types.LoadedReplyContext{{ID: "kova", Source: "bank", Body: "..."}}
	bundle := &types.ReplyBundle{ThreadURL: "u", Subreddit: "test", Mode: types.ReplyModeReply}

	if err := s.SaveThread(thread); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveContexts(contexts); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMode(mode); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDrafts(bundle); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveOutput("# rendered output"); err != nil {
		t.Fatal(err)
	}

	for _, fname := range []string{"thread.json", "contexts.json", "mode.json", "drafts.json", "output.md"} {
		if _, err := os.Stat(filepath.Join(s.Path(), fname)); err != nil {
			t.Errorf("expected file %s: %v", fname, err)
		}
	}
}

func TestWriteJSONRejectsEmptyName(t *testing.T) {
	root := t.TempDir()
	s, _ := NewSession(root, "test", "x1", time.Now())
	if err := s.WriteJSON("", "x"); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestSessionsRootUsesHome(t *testing.T) {
	// Just verify the path shape — we can't easily mock UserHomeDir.
	root, err := SessionsRoot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(root, filepath.Join(".tider", "sessions", "replies")) {
		t.Errorf("unexpected root path: %s", root)
	}
}
