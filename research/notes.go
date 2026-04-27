package research

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/viggy28/tider/internal/types"
)

// LoadNotes reads subreddits.yaml from path. A missing file returns (nil, nil)
// — the curated layer is optional.
func LoadNotes(path string) (*types.SubsConfig, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read subreddits.yaml: %w", err)
	}
	var cfg types.SubsConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse subreddits.yaml: %w", err)
	}
	return &cfg, nil
}

// FindSub returns the curated notes for sub (case-insensitive), or nil.
func FindSub(cfg *types.SubsConfig, sub string) *types.SubNotes {
	if cfg == nil {
		return nil
	}
	for i := range cfg.Subs {
		if strings.EqualFold(cfg.Subs[i].Name, sub) {
			return &cfg.Subs[i]
		}
	}
	return nil
}
