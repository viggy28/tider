// Package reddit is the only package that talks to Reddit. It exposes a
// cached HTTP client with a polite User-Agent and exponential backoff.
package reddit

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/viggy28/tider/internal/types"
)

const (
	DefaultUserAgent = "tider/0.1 (by /u/tider28)"
	DefaultBaseURL   = "https://www.reddit.com"
)

// Tiered TTLs as specified in the project plan.
const (
	TTLAbout  = 7 * 24 * time.Hour
	TTLRules  = 7 * 24 * time.Hour
	TTLWiki   = 7 * 24 * time.Hour
	TTLFlairs = 7 * 24 * time.Hour
	TTLTop    = 24 * time.Hour
	TTLHot    = 6 * time.Hour
)

type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UserAgent string
	Cache     *Cache
	MaxRetry  int
	BaseDelay time.Duration
}

func NewClient(cache *Cache) *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		BaseURL:   DefaultBaseURL,
		UserAgent: DefaultUserAgent,
		Cache:     cache,
		MaxRetry:  3,
		BaseDelay: 500 * time.Millisecond,
	}
}

// get performs a GET with exponential backoff on 429/5xx/network errors.
// The status code is returned alongside the body so callers can soft-fail
// on 403/404 without losing the signal.
func (c *Client) get(ctx context.Context, path string) ([]byte, int, error) {
	var lastStatus int
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetry; attempt++ {
		if attempt > 0 {
			delay := c.BaseDelay << (attempt - 1)
			select {
			case <-ctx.Done():
				return nil, lastStatus, ctx.Err()
			case <-time.After(delay):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
		if err != nil {
			return nil, 0, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("User-Agent", c.UserAgent)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastStatus = 0
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		lastStatus = resp.StatusCode
		switch {
		case resp.StatusCode == http.StatusOK:
			return body, resp.StatusCode, nil
		case resp.StatusCode == http.StatusTooManyRequests, resp.StatusCode >= 500:
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			continue
		default:
			return body, resp.StatusCode, fmt.Errorf("reddit %s: status %d", path, resp.StatusCode)
		}
	}
	return nil, lastStatus, fmt.Errorf("reddit %s: exhausted retries: %w", path, lastErr)
}

// fetch returns cached bytes if fresh; otherwise fetches and caches.
func (c *Client) fetch(ctx context.Context, sub, file, path string, ttl time.Duration, refresh bool) ([]byte, error) {
	if !refresh {
		if data, fresh, err := c.Cache.Get(sub, file); err == nil && fresh {
			return data, nil
		}
	}
	body, _, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := c.Cache.Put(sub, file, body, ttl); err != nil {
		return nil, err
	}
	return body, nil
}

// fetchSoft is fetch with 403/404 → (nil, nil) for endpoints that many subs
// gate or omit (wiki rules, link flairs).
func (c *Client) fetchSoft(ctx context.Context, sub, file, path string, ttl time.Duration, refresh bool) ([]byte, error) {
	if !refresh {
		if data, fresh, err := c.Cache.Get(sub, file); err == nil && fresh {
			return data, nil
		}
	}
	body, status, err := c.get(ctx, path)
	if err != nil {
		if status == http.StatusForbidden || status == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	if err := c.Cache.Put(sub, file, body, ttl); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) About(ctx context.Context, sub string, refresh bool) (types.Subreddit, error) {
	body, err := c.fetch(ctx, sub, "about.json", "/r/"+sub+"/about.json", TTLAbout, refresh)
	if err != nil {
		return types.Subreddit{}, err
	}
	return parseAbout(body)
}

func (c *Client) Rules(ctx context.Context, sub string, refresh bool) ([]types.Rule, error) {
	body, err := c.fetch(ctx, sub, "rules.json", "/r/"+sub+"/about/rules.json", TTLRules, refresh)
	if err != nil {
		return nil, err
	}
	return parseRules(body)
}

func (c *Client) WikiRules(ctx context.Context, sub string, refresh bool) (string, error) {
	body, err := c.fetchSoft(ctx, sub, "wiki_rules.json", "/r/"+sub+"/wiki/rules.json", TTLWiki, refresh)
	if err != nil {
		return "", err
	}
	if body == nil {
		return "", nil
	}
	return parseWiki(body)
}

func (c *Client) Top(ctx context.Context, sub, period string, refresh bool) ([]types.Post, error) {
	var file string
	switch period {
	case "week":
		file = "top_week.json"
	case "month":
		file = "top_month.json"
	default:
		return nil, fmt.Errorf("unknown top period: %s", period)
	}
	path := "/r/" + sub + "/top.json?t=" + period + "&limit=25"
	body, err := c.fetch(ctx, sub, file, path, TTLTop, refresh)
	if err != nil {
		return nil, err
	}
	return parseListing(body)
}

func (c *Client) Hot(ctx context.Context, sub string, refresh bool) ([]types.Post, error) {
	body, err := c.fetch(ctx, sub, "hot.json", "/r/"+sub+"/hot.json?limit=25", TTLHot, refresh)
	if err != nil {
		return nil, err
	}
	return parseListing(body)
}

func (c *Client) Flairs(ctx context.Context, sub string, refresh bool) ([]types.Flair, error) {
	body, err := c.fetchSoft(ctx, sub, "flairs.json", "/r/"+sub+"/api/link_flair_v2.json", TTLFlairs, refresh)
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, nil
	}
	return parseFlairs(body)
}

// Stickies derives stickied posts from the hot listing.
func Stickies(hot []types.Post) []types.Post {
	var out []types.Post
	for _, p := range hot {
		if p.Stickied {
			out = append(out, p)
		}
	}
	return out
}
