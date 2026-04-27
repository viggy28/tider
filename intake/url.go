package intake

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/viggy28/tider/internal/types"
)

// FromURL fetches urlStr and runs LLM extraction against the body. GitHub
// repo URLs (e.g., https://github.com/owner/repo) are rewritten to the
// raw README so the extractor sees substance rather than the GitHub UI.
func (i *Intake) FromURL(ctx context.Context, urlStr string) (*types.Brief, error) {
	target := urlStr
	if rewritten, ok := githubRepoToReadme(urlStr); ok {
		target = rewritten
	}
	body, err := i.fetch(ctx, target)
	if err != nil {
		return nil, err
	}
	return i.extract(ctx, types.BriefSource{Mode: "url", Value: target}, body)
}

func (i *Intake) fetch(ctx context.Context, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", i.UserAgent)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: status %d", target, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, i.MaxBytes))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	return string(body), nil
}

// githubRepoToReadme rewrites a github.com/owner/repo URL to the raw
// README at HEAD. Returns ok=false for any other URL shape, including
// deeper paths within a repo (where the raw rewrite wouldn't apply).
func githubRepoToReadme(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(parsed.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	owner, repo := parts[0], parts[1]
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/README.md", owner, repo), true
}
