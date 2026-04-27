// Package research assembles per-subreddit research bundles by combining
// curated notes (subreddits.yaml) with live (cached) Reddit data.
package research

import (
	"context"
	"fmt"
	"time"

	"github.com/viggy28/tider/internal/reddit"
	"github.com/viggy28/tider/internal/types"
)

// Fetcher is the subset of reddit.Client that research depends on.
// Defining it here keeps research/ free of HTTP/cache plumbing in tests.
type Fetcher interface {
	About(ctx context.Context, sub string, refresh bool) (types.Subreddit, error)
	Rules(ctx context.Context, sub string, refresh bool) ([]types.Rule, error)
	WikiRules(ctx context.Context, sub string, refresh bool) (string, error)
	Top(ctx context.Context, sub, period string, refresh bool) ([]types.Post, error)
	Hot(ctx context.Context, sub string, refresh bool) ([]types.Post, error)
	Flairs(ctx context.Context, sub string, refresh bool) ([]types.Flair, error)
}

// For assembles a Research bundle for sub.
func For(ctx context.Context, f Fetcher, notes *types.SubsConfig, sub string, refresh bool) (*types.Research, error) {
	about, err := f.About(ctx, sub, refresh)
	if err != nil {
		return nil, fmt.Errorf("about: %w", err)
	}
	rules, err := f.Rules(ctx, sub, refresh)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	wiki, err := f.WikiRules(ctx, sub, refresh)
	if err != nil {
		return nil, fmt.Errorf("wiki rules: %w", err)
	}
	topW, err := f.Top(ctx, sub, "week", refresh)
	if err != nil {
		return nil, fmt.Errorf("top week: %w", err)
	}
	topM, err := f.Top(ctx, sub, "month", refresh)
	if err != nil {
		return nil, fmt.Errorf("top month: %w", err)
	}
	hot, err := f.Hot(ctx, sub, refresh)
	if err != nil {
		return nil, fmt.Errorf("hot: %w", err)
	}
	flairs, err := f.Flairs(ctx, sub, refresh)
	if err != nil {
		return nil, fmt.Errorf("flairs: %w", err)
	}
	return &types.Research{
		Sub:       about,
		Notes:     FindSub(notes, sub),
		Rules:     rules,
		WikiRules: wiki,
		TopWeek:   topW,
		TopMonth:  topM,
		Hot:       hot,
		Stickies:  reddit.Stickies(hot),
		Flairs:    flairs,
		Generated: time.Now().UTC(),
	}, nil
}
