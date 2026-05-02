package reply

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
// Re-runs against the same thread on the same day get a numeric suffix
// (-2, -3, ...) so subsequent runs don't silently overwrite prior
// artifacts. The first run is the unsuffixed slug so the common-case
// path stays clean.
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
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir sessions root: %w", err)
	}
	base := now.UTC().Format("2006-01-02") + "-" + s + "-" + strings.ToLower(postID)

	// Try the unsuffixed slug first; if it exists, append -2, -3, etc.
	// os.Mkdir (not MkdirAll) fails atomically when the dir exists, which
	// avoids a TOCTOU race between Stat and create.
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		dir := filepath.Join(root, candidate)
		err := os.Mkdir(dir, 0o755)
		if err == nil {
			return &Session{Dir: dir}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("mkdir session: %w", err)
		}
	}
	return nil, fmt.Errorf("session: too many existing dirs for slug %q (gave up after 1000)", base)
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

func (s *Session) SavePost(p *types.ReplyPost) error {
	return s.WriteJSON("post.json", p)
}

func (s *Session) SaveOutcome(o *types.ReplyOutcome) error {
	return s.WriteJSON("outcome.json", o)
}

// HasFile reports whether <name> exists in the session directory.
func (s *Session) HasFile(name string) bool {
	_, err := os.Stat(filepath.Join(s.Dir, name))
	return err == nil
}

// LoadJSON reads <name> from the session directory and unmarshals into v.
func (s *Session) LoadJSON(name string, v any) error {
	data, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal %s: %w", name, err)
	}
	return nil
}

// ResolveSession finds a session under root by an exact directory name
// or unique substring match. Returns the matched session, or an error
// listing the candidates if the substring matches more than one.
//
// Substring (rather than strict prefix) is the contract because the
// canonical id format is date-sub-postid: users naturally remember the
// postID, which sits at the *end* of the slug, so a strict prefix rule
// would reject the most common shorthand. Exact match wins over
// substring to avoid surprises when one id happens to be a substring
// of another.
func ResolveSession(root, idOrSubstring string) (*Session, error) {
	idOrSubstring = strings.TrimSpace(idOrSubstring)
	if idOrSubstring == "" {
		return nil, errors.New("session id required")
	}

	// Reject path-traversal-y inputs before we touch the filesystem.
	// Session ids are plain directory names (date-sub-postid, possibly
	// with -2/-3 collision suffixes); anything containing a path
	// separator or ".." is either a typo or an attempt to walk out of
	// the sessions root. filepath.Join silently collapses ".." segments,
	// so without this guard `tider reply post ../../foo` would resolve
	// to a directory outside ~/.tider/sessions/replies/ on Stat.
	if strings.ContainsAny(idOrSubstring, `/\`) || strings.Contains(idOrSubstring, "..") {
		return nil, fmt.Errorf("invalid session id %q: must be a plain directory name", idOrSubstring)
	}

	// Exact match first.
	exact := filepath.Join(root, idOrSubstring)
	if fi, err := os.Stat(exact); err == nil && fi.IsDir() {
		return &Session{Dir: exact}, nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no sessions found at %s (have you run `tider reply --url=...` yet?)", root)
		}
		return nil, fmt.Errorf("read sessions root: %w", err)
	}

	var matches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.Contains(e.Name(), idOrSubstring) {
			matches = append(matches, e.Name())
		}
	}

	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no session matching %q under %s", idOrSubstring, root)
	case 1:
		return &Session{Dir: filepath.Join(root, matches[0])}, nil
	default:
		return nil, fmt.Errorf("ambiguous session id %q matches %d candidates:\n  %s", idOrSubstring, len(matches), strings.Join(matches, "\n  "))
	}
}

// SessionStatus reports the lifecycle stage of a session based on which
// artifacts are on disk. The values map directly to what `reply recent`
// displays.
type SessionStatus string

const (
	SessionStatusFailed          SessionStatus = "failed"
	SessionStatusDrafted         SessionStatus = "drafted"
	SessionStatusPosted          SessionStatus = "posted"
	SessionStatusOutcomeRecorded SessionStatus = "outcome-recorded"
)

// Status inspects the session directory and returns the current stage.
// Order matters: outcome > post > drafts > failed.
func (s *Session) Status() SessionStatus {
	switch {
	case s.HasFile("outcome.json"):
		return SessionStatusOutcomeRecorded
	case s.HasFile("post.json"):
		return SessionStatusPosted
	case s.HasFile("drafts.json"):
		return SessionStatusDrafted
	default:
		return SessionStatusFailed
	}
}

// ListSessions returns every session under root, newest first, capped at
// limit (limit <= 0 means no cap). Newest is determined by the session
// directory's mtime, which is set when the directory is created in
// NewSession and stable across subsequent file writes inside it.
func ListSessions(root string, limit int) ([]*Session, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions root: %w", err)
	}

	type entryInfo struct {
		name    string
		modTime time.Time
	}
	infos := make([]entryInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		infos = append(infos, entryInfo{name: e.Name(), modTime: fi.ModTime()})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].modTime.After(infos[j].modTime)
	})

	if limit > 0 && len(infos) > limit {
		infos = infos[:limit]
	}

	sessions := make([]*Session, 0, len(infos))
	for _, info := range infos {
		sessions = append(sessions, &Session{Dir: filepath.Join(root, info.name)})
	}
	return sessions, nil
}
