// Package lastdraft persists the most recent draft Snapshot per subreddit
// at ~/.tider/last/{sub}.json so `tider regen` can pick up where `tider
// draft` left off without flag-passing the bundle around.
//
// This is intentionally simpler than the full session structure described
// in CLAUDE.md (which lives at ~/.tider/projects/{project}/sessions/...).
// Sessions are a separate refactor; for now, last-per-sub is the smallest
// thing that makes regen ergonomic.
package lastdraft

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/viggy28/tider/internal/types"
)

// Default returns ~/.tider/last as the storage directory.
func Default() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".tider", "last"), nil
}

func path(root, sub string) string {
	return filepath.Join(root, sub+".json")
}

// Save writes snap to root/<sub>.json, stamping SavedAt.
func Save(root, sub string, snap *types.Snapshot) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("mkdir last: %w", err)
	}
	snap.SavedAt = time.Now().UTC()
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("encode snapshot: %w", err)
	}
	// Write to a temp file and rename for atomicity — a partial write
	// otherwise corrupts the cache for a sub.
	tmp := path(root, sub) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := os.Rename(tmp, path(root, sub)); err != nil {
		return fmt.Errorf("rename snapshot: %w", err)
	}
	return nil
}

// ErrNotFound is returned by Load when no snapshot exists for sub.
var ErrNotFound = errors.New("no snapshot found — run `tider draft --sub=<sub>` first")

// Load reads root/<sub>.json. Returns ErrNotFound if absent.
func Load(root, sub string) (*types.Snapshot, error) {
	data, err := os.ReadFile(path(root, sub))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read snapshot: %w", err)
	}
	var snap types.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("parse snapshot: %w", err)
	}
	return &snap, nil
}
