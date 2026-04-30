package intake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/viggy28/tider/internal/types"
)

// FromURL fetches urlStr and runs LLM extraction against the body.
//
// GitHub repo URLs (https://github.com/owner/repo) get a richer pipeline:
//
//   1. Try README.md and common casing variants (readme.md, README, ...).
//      Most repos have one and this is the cheapest path.
//
//   2. If no README casing hits, fall back to the GitHub API:
//      - GET /repos/{owner}/{repo} for description, topics, language, etc.
//      - GET /repos/.../git/trees/{default_branch} for the top-level
//        listing.
//      - Fetch top-level .md files (KOVA_SPEC.md, DECISIONS.md, etc.)
//        and standard manifests (go.mod, package.json, ...) up to a
//        byte budget.
//      - Synthesize a corpus and feed it to the extraction prompt.
//
//   3. If the API returns 404 on metadata, surface a clear "repo not
//      found (private, deleted, or typo)" error.
//
// Private repos require GITHUB_TOKEN in env (or .env). Same env-var-only
// invariant as the LLM provider keys.
//
// Non-GitHub URLs flow through the simple fetch + extract path unchanged.
func (i *Intake) FromURL(ctx context.Context, urlStr string) (*types.Brief, error) {
	if owner, repo, ok := parseGitHubRepo(urlStr); ok {
		return i.fromGitHubRepo(ctx, urlStr, owner, repo)
	}
	body, err := i.fetch(ctx, urlStr)
	if err != nil {
		return nil, err
	}
	return i.extract(ctx, types.BriefSource{Mode: "url", Value: urlStr}, body)
}

func (i *Intake) fromGitHubRepo(ctx context.Context, originalURL, owner, repo string) (*types.Brief, error) {
	// Path 1: try README casings.
	body, ok, err := i.fetchGitHubReadme(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	if ok {
		// Tag the source with the originally requested URL — that's what
		// the user typed, even if internally we resolved to README.md.
		return i.extract(ctx, types.BriefSource{Mode: "url", Value: originalURL}, body)
	}

	// Path 2: API metadata fallback.
	meta, err := i.fetchGitHubRepoMeta(ctx, owner, repo)
	if err != nil {
		if errors.Is(err, errGitHubRepoNotFound) {
			return nil, fmt.Errorf("github.com/%s/%s not found — repo doesn't exist, is private without GITHUB_TOKEN set, or the URL has a typo", owner, repo)
		}
		return nil, err
	}

	tree, err := i.fetchGitHubTree(ctx, owner, repo, meta.DefaultBranch)
	if err != nil {
		return nil, fmt.Errorf("github tree (no README found, can't read top-level files either): %w", err)
	}

	paths := pickInterestingFiles(tree.Tree)
	if len(paths) == 0 {
		return nil, fmt.Errorf("github.com/%s/%s has no README and no top-level .md or manifest files — try `tider intake --file=<your-notes.md>` instead", owner, repo)
	}

	files, err := i.fetchGitHubFiles(ctx, owner, repo, meta.DefaultBranch, paths)
	if err != nil {
		return nil, fmt.Errorf("github files: %w", err)
	}

	corpus := synthesizeRepoCorpus(meta, files, paths)
	return i.extract(ctx, types.BriefSource{Mode: "url", Value: originalURL}, corpus)
}

// fetch is the simple HTTP path used for non-GitHub URLs. (GitHub URLs
// are handled by fromGitHubRepo above, which has its own fetch helpers
// that attach GITHUB_TOKEN auth.)
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

// githubRepoToReadme is kept as an exported-ish (tested-from-within-package)
// shim so existing tests in url_test.go continue to compile. Equivalent
// to parseGitHubRepo + the README.md path that fetchGitHubReadme tries
// first; deeper paths still return ok=false.
func githubRepoToReadme(raw string) (string, bool) {
	owner, repo, ok := parseGitHubRepo(raw)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s/%s/%s/HEAD/README.md", githubRawBase, owner, repo), true
}
