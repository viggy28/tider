// Package contextbank stores reusable project/context notes for drafting.
package contextbank

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var idRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Entry is one saved context document.
type Entry struct {
	ID   string
	Path string
	Body string
}

// DefaultDir returns the canonical context-bank directory.
func DefaultDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".tider", "contexts"), nil
}

// ValidateID checks whether id is safe for use as a context-bank filename.
func ValidateID(id string) error {
	if !idRE.MatchString(id) {
		return fmt.Errorf("invalid context id %q: use letters, numbers, dashes, or underscores", id)
	}
	return nil
}

// PathFor returns dir/<id>.md after validating id.
func PathFor(dir, id string) (string, error) {
	if err := ValidateID(id); err != nil {
		return "", err
	}
	return filepath.Join(dir, id+".md"), nil
}

// List returns all markdown context entries in dir, sorted by id. Missing
// context directories are treated as empty.
func List(dir string) ([]Entry, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read contexts: %w", err)
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		if ValidateID(id) != nil {
			continue
		}
		out = append(out, Entry{ID: id, Path: filepath.Join(dir, e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Load resolves ref as either a context id or a filesystem path and reads it.
// Path refs are useful for ad hoc contexts; id refs come from the bank.
func Load(dir, ref string) (*Entry, error) {
	path, id, err := resolveRef(dir, ref)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("context %q not found", ref)
	}
	if err != nil {
		return nil, fmt.Errorf("read context %q: %w", ref, err)
	}
	return &Entry{ID: id, Path: path, Body: string(data)}, nil
}

// Import copies sourcePath into dir/<id>.md. Existing entries are overwritten
// only when force is true.
func Import(dir, id, sourcePath string, force bool) (*Entry, error) {
	dest, err := PathFor(dir, id)
	if err != nil {
		return nil, err
	}
	if !force {
		if _, err := os.Stat(dest); err == nil {
			return nil, fmt.Errorf("context %q already exists; use --force to overwrite", id)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("stat context %q: %w", id, err)
		}
	}
	in, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("open source context: %w", err)
	}
	defer in.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir contexts: %w", err)
	}
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create context: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return nil, fmt.Errorf("copy context: %w", err)
	}
	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("close context: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return nil, fmt.Errorf("rename context: %w", err)
	}
	entry, err := Load(dir, id)
	if err != nil {
		return nil, err
	}
	return entry, nil
}

// Ensure returns the bank path for id, creating dir and an empty file when the
// entry does not exist. It is used by `tider context edit`.
func Ensure(dir, id string) (string, error) {
	path, err := PathFor(dir, id)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir contexts: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE, 0o644)
	if err != nil {
		return "", fmt.Errorf("ensure context: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close context: %w", err)
	}
	return path, nil
}

func resolveRef(dir, ref string) (path, id string, err error) {
	if looksLikePath(ref) {
		return ref, strings.TrimSuffix(filepath.Base(ref), filepath.Ext(ref)), nil
	}
	path, err = PathFor(dir, ref)
	if err != nil {
		return "", "", err
	}
	return path, ref, nil
}

func looksLikePath(ref string) bool {
	return strings.ContainsRune(ref, os.PathSeparator) || strings.HasPrefix(ref, ".") || filepath.Ext(ref) != ""
}
