package reply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/viggy28/tider/internal/types"
)

// SessionsRoot returns the canonical sessions directory under the user's
// home: ~/.tider/sessions/replies. Tests can pass an explicit root to
// NewSession instead.
func SessionsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".tider", "sessions", "replies"), nil
}

// Session is a directory under the sessions root that collects artifacts
// from a single tider reply invocation: thread.json, contexts.json,
// mode.json, drafts.json, output.md, plus review-mode-only files
// (target.json, inspection.json, review-notes.json).
//
// Files are written incrementally as each pipeline step completes. A
// session for a partially-successful run still preserves whatever made
// it through (e.g. thread + mode for a review-mode failure with no
// target URL).
type Session struct {
	Dir string
}

// NewSession creates a session directory under root using a slug derived
// from now/sub/postID — date prefix keeps multiple runs against the
// same thread sortable; lowercasing sub keeps filesystem behavior
// consistent across case-insensitive filesystems and Reddit's case-
// preserved URLs.
//
// PostID is required (without it the directory wouldn't be unique to a
// thread). An empty sub is allowed — slot becomes "unknown" — because
// short-link parses don't carry the sub upfront.
func NewSession(root string, sub, postID string, now time.Time) (*Session, error) {
	postID = strings.TrimSpace(postID)
	if postID == "" {
		return nil, errors.New("session: postID required")
	}
	s := strings.ToLower(strings.TrimSpace(sub))
	if s == "" {
		s = "unknown"
	}
	slug := now.UTC().Format("2006-01-02") + "-" + s + "-" + strings.ToLower(postID)
	dir := filepath.Join(root, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir session: %w", err)
	}
	return &Session{Dir: dir}, nil
}

// Path returns the absolute session directory.
func (s *Session) Path() string { return s.Dir }

// WriteJSON marshals v indented and writes it atomically to <dir>/<name>.
// Atomic via temp file + rename so a partial write (interrupted run,
// disk full mid-marshal) doesn't leave a corrupt JSON.
func (s *Session) WriteJSON(name string, v any) error {
	if name == "" {
		return errors.New("session: empty name")
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	return s.writeAtomic(name, data)
}

// WriteMarkdown writes raw markdown body to <dir>/<name>. Atomic.
func (s *Session) WriteMarkdown(name, body string) error {
	if name == "" {
		return errors.New("session: empty name")
	}
	return s.writeAtomic(name, []byte(body))
}

func (s *Session) writeAtomic(name string, data []byte) error {
	final := filepath.Join(s.Dir, name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp) // best-effort cleanup
		return fmt.Errorf("rename %s: %w", name, err)
	}
	return nil
}

// Convenience helpers — typed wrappers so the CLI code reads cleanly
// (s.SaveMode(m) vs s.WriteJSON("mode.json", m)).

func (s *Session) SaveThread(t *types.Thread) error {
	return s.WriteJSON("thread.json", t)
}

func (s *Session) SaveContexts(ctxs []types.LoadedReplyContext) error {
	return s.WriteJSON("contexts.json", ctxs)
}

func (s *Session) SaveMode(m *types.ReplyModeResult) error {
	return s.WriteJSON("mode.json", m)
}

func (s *Session) SaveDrafts(b *types.ReplyBundle) error {
	return s.WriteJSON("drafts.json", b)
}

func (s *Session) SaveOutput(md string) error {
	return s.WriteMarkdown("output.md", md)
}
