package research

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/viggy28/tider/internal/types"
)

const RawBundleTTL = 6 * time.Hour

var subNameRE = regexp.MustCompile(`^[A-Za-z0-9_]{2,30}$`)

// NormalizeSub validates a subreddit name for both Reddit URLs and local
// cache paths. A leading r/ is accepted for CLI ergonomics.
func NormalizeSub(sub string) (string, error) {
	if len(sub) >= 2 && (sub[0] == 'r' || sub[0] == 'R') && sub[1] == '/' {
		sub = sub[2:]
	}
	if !subNameRE.MatchString(sub) {
		return "", fmt.Errorf("invalid subreddit %q: use letters, numbers, or underscores", sub)
	}
	return sub, nil
}

func rawDir(cacheRoot string) string { return filepath.Join(cacheRoot, "research") }

func rawPath(cacheRoot, sub string) string {
	return filepath.Join(rawDir(cacheRoot), sub+".json")
}

// LoadRaw reads a fresh assembled Research bundle from cache. Missing or stale
// bundles return (nil, nil), letting callers fetch from Reddit as usual.
func LoadRaw(cacheRoot, sub string, ttl time.Duration) (*types.Research, error) {
	path := rawPath(cacheRoot, sub)
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("stat raw research cache: %w", err)
	}
	if ttl > 0 && time.Since(st.ModTime()) > ttl {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read raw research cache: %w", err)
	}
	var r types.Research
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse raw research cache: %w", err)
	}
	return &r, nil
}

// SaveRaw stores the assembled Research bundle so insight generation can be
// rerun without hitting Reddit while the cache is fresh.
func SaveRaw(cacheRoot, sub string, r *types.Research) error {
	if err := os.MkdirAll(rawDir(cacheRoot), 0o755); err != nil {
		return fmt.Errorf("mkdir raw research cache: %w", err)
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("encode raw research cache: %w", err)
	}
	tmp := rawPath(cacheRoot, sub) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write raw research cache: %w", err)
	}
	if err := os.Rename(tmp, rawPath(cacheRoot, sub)); err != nil {
		return fmt.Errorf("rename raw research cache: %w", err)
	}
	return nil
}
