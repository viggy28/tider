package reddit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ParseThreadURL extracts the subreddit and post id from a Reddit thread
// URL. Subreddit is optional in some URL forms — when not present in the
// URL, it's returned empty and callers can fill it from the fetched
// thread metadata.
//
// Supported forms:
//
//   https://www.reddit.com/r/<sub>/comments/<id>/<slug>/
//   https://reddit.com/r/<sub>/comments/<id>/
//   https://reddit.com/comments/<id>/
//   https://old.reddit.com/r/<sub>/comments/<id>/...
//   https://np.reddit.com/r/<sub>/comments/<id>/...
//   https://redd.it/<id>            # short link; sub not in URL, must be resolved
//
// redd.it short links are returned with an empty sub; the caller should
// either rely on the thread fetch to fill it in, or resolve via
// ResolveShortLink.
func ParseThreadURL(raw string) (sub, postID string, err error) {
	if raw == "" {
		return "", "", errors.New("reddit url is empty")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "old.")
	host = strings.TrimPrefix(host, "np.")
	host = strings.TrimPrefix(host, "new.")

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch host {
	case "redd.it":
		// Short link: redd.it/<id>
		if len(parts) != 1 || parts[0] == "" {
			return "", "", fmt.Errorf("malformed redd.it short link: %s", raw)
		}
		return "", parts[0], nil
	case "reddit.com":
		// Two long forms:
		//   /r/<sub>/comments/<id>[/...]
		//   /comments/<id>[/...]
		// Empty segments (e.g. /comments//slug or /r/sub/comments//slug)
		// are rejected here rather than left to fail deep in fetch with
		// a misleading network error.
		switch {
		case len(parts) >= 2 && parts[0] == "comments":
			if parts[1] == "" {
				return "", "", fmt.Errorf("malformed reddit url: %s (empty post id between /comments/ and trailing slash)", raw)
			}
			return "", parts[1], nil
		case len(parts) >= 4 && parts[0] == "r" && parts[2] == "comments":
			if parts[1] == "" {
				return "", "", fmt.Errorf("malformed reddit url: %s (empty subreddit between /r/ and /comments/)", raw)
			}
			if parts[3] == "" {
				return "", "", fmt.Errorf("malformed reddit url: %s (empty post id between /comments/ and slug)", raw)
			}
			return parts[1], parts[3], nil
		default:
			return "", "", fmt.Errorf("unsupported reddit url shape: %s (expected /r/<sub>/comments/<id>/... or /comments/<id>/...)", raw)
		}
	default:
		return "", "", fmt.Errorf("unsupported host %q (expected reddit.com or redd.it)", u.Host)
	}
}

// Canonicalize returns a Reddit URL in a form that ParseThreadURL can
// consume. URLs that ParseThreadURL already handles (canonical
// /r/<sub>/comments/<id>/... and redd.it short links) are returned
// unchanged with no network I/O.
//
// Reddit's mobile app share links — https://www.reddit.com/s/<token> —
// are an opaque form: the token isn't a post id, just a redirect handle.
// Pasting one to `tider reply --url=...` is common (it's what "Share" in
// the app produces), so this helper follows the 30x redirect to reach
// the canonical /r/<sub>/comments/<id>/... URL before parsing.
//
// Errors from the redirect lookup are surfaced as-is. Non-share URLs
// pass through even if they later fail ParseThreadURL — letting that
// function surface the better shape error.
func Canonicalize(ctx context.Context, client *http.Client, raw string) (string, error) {
	if raw == "" {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw, nil
	}
	host := strings.ToLower(u.Host)
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "old.")
	host = strings.TrimPrefix(host, "np.")
	host = strings.TrimPrefix(host, "new.")
	if host != "reddit.com" {
		return raw, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "s" && parts[1] != "" {
		// Mobile share link — follow the redirect using the same one-shot
		// redirect logic as ResolveShortLink (the function is generic;
		// the name reflects redd.it being the original use case).
		return ResolveShortLink(ctx, client, raw)
	}
	return raw, nil
}

// ResolveShortLink follows a redd.it/<id> URL to its canonical reddit.com
// thread URL. Useful when you want the subreddit name without fetching
// the full thread first; not strictly required because /comments/<id>.json
// works regardless of subreddit, but handy for printing canonical URLs.
//
// Also used by Canonicalize for mobile share URLs (/s/<token>): the
// underlying logic is just "follow one redirect," so it works on any
// URL Reddit serves with a 30x + Location header.
//
// HTTP client should not auto-follow redirects when calling this — we
// look at the Location header from the first response.
func ResolveShortLink(ctx context.Context, client *http.Client, shortURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, shortURL, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", DefaultUserAgent)

	// Use a one-shot client that does NOT follow redirects so we can grab
	// the Location header directly. Falls back to client.Do behavior
	// otherwise (timeout, cookie jar inheritance not relevant here).
	noRedirect := *client
	noRedirect.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	resp, err := noRedirect.Do(req)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", shortURL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return "", fmt.Errorf("resolve %s: status %d (expected redirect)", shortURL, resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("resolve %s: redirect with no Location header", shortURL)
	}
	return loc, nil
}
