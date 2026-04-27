package reddit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache stores per-sub Reddit responses on disk under Root/subs/{name}/,
// with TTLs tracked in a sibling _meta.json.
type Cache struct {
	Root string
	mu   sync.Mutex
}

type cacheMeta struct {
	FetchedAt map[string]time.Time `json:"fetched_at"`
	TTLNanos  map[string]int64     `json:"ttl_nanos"`
}

func NewCache(root string) *Cache { return &Cache{Root: root} }

func (c *Cache) subDir(sub string) string  { return filepath.Join(c.Root, "subs", sub) }
func (c *Cache) metaPath(sub string) string { return filepath.Join(c.subDir(sub), "_meta.json") }

func (c *Cache) loadMeta(sub string) (*cacheMeta, error) {
	data, err := os.ReadFile(c.metaPath(sub))
	if errors.Is(err, os.ErrNotExist) {
		return &cacheMeta{
			FetchedAt: map[string]time.Time{},
			TTLNanos:  map[string]int64{},
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read cache meta: %w", err)
	}
	var m cacheMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse cache meta: %w", err)
	}
	if m.FetchedAt == nil {
		m.FetchedAt = map[string]time.Time{}
	}
	if m.TTLNanos == nil {
		m.TTLNanos = map[string]int64{}
	}
	return &m, nil
}

func (c *Cache) saveMeta(sub string, m *cacheMeta) error {
	if err := os.MkdirAll(c.subDir(sub), 0o755); err != nil {
		return fmt.Errorf("mkdir cache: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode cache meta: %w", err)
	}
	return os.WriteFile(c.metaPath(sub), data, 0o644)
}

// Get returns cached bytes for (sub, file). fresh==false means caller should
// fetch — either the entry is missing or its TTL has expired.
func (c *Cache) Get(sub, file string) (data []byte, fresh bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, err := c.loadMeta(sub)
	if err != nil {
		return nil, false, err
	}
	fetchedAt, ok := m.FetchedAt[file]
	if !ok {
		return nil, false, nil
	}
	ttl := time.Duration(m.TTLNanos[file])
	if ttl > 0 && time.Since(fetchedAt) > ttl {
		return nil, false, nil
	}
	body, err := os.ReadFile(filepath.Join(c.subDir(sub), file))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cached file: %w", err)
	}
	return body, true, nil
}

// Put stores bytes for (sub, file) with the given TTL.
func (c *Cache) Put(sub, file string, data []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.MkdirAll(c.subDir(sub), 0o755); err != nil {
		return fmt.Errorf("mkdir cache: %w", err)
	}
	if err := os.WriteFile(filepath.Join(c.subDir(sub), file), data, 0o644); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}
	m, err := c.loadMeta(sub)
	if err != nil {
		return err
	}
	m.FetchedAt[file] = time.Now().UTC()
	m.TTLNanos[file] = int64(ttl)
	return c.saveMeta(sub, m)
}
