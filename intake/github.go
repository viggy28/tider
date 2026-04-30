package intake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
)

// Public-API endpoints. BaseURLs are package-level so tests can point
// the fetcher at httptest.Server URLs.
var (
	githubAPIBase = "https://api.github.com"
	githubRawBase = "https://raw.githubusercontent.com"
)

// readmeCasings is the ordered list of paths we try at HEAD when
// rewriting a github.com/owner/repo URL to a raw README path. README.md
// covers ~95% of repos; the rest cover the long tail.
var readmeCasings = []string{
	"README.md",
	"readme.md",
	"Readme.md",
	"README.markdown",
	"README.MD",
	"README",
}

// manifestFiles are the standard project-root files whose presence
// reveals what kind of project this is. Order is preference (most
// information-dense first) but we'll fetch all that exist.
var manifestFiles = []string{
	"go.mod",
	"package.json",
	"Cargo.toml",
	"pyproject.toml",
	"Gemfile",
	"setup.py",
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"composer.json",
}

// Per-file and total byte budgets for the metadata-fallback corpus.
// Single very large file (300KB CHANGELOG.md) shouldn't crowd out other
// docs; total cap protects the LLM context.
const (
	githubFetchPerFileBytes = 64 * 1024
	githubFetchTotalBytes   = 256 * 1024
)

type githubRepoMeta struct {
	Name          string   `json:"name"`
	FullName      string   `json:"full_name"`
	Description   string   `json:"description"`
	Topics        []string `json:"topics"`
	Language      string   `json:"language"`
	DefaultBranch string   `json:"default_branch"`
	Stars         int      `json:"stargazers_count"`
	Private       bool     `json:"private"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"` // "blob" | "tree"
	Size int    `json:"size"`
}

type githubTreeResp struct {
	Tree     []githubTreeEntry `json:"tree"`
	Truncated bool             `json:"truncated"`
}

// parseGitHubRepo returns owner/repo for github.com/{owner}/{repo} URLs
// (with or without trailing slash). Returns ok=false for any other shape
// — deeper paths within a repo (e.g. /blob/main/...) aren't supported by
// the fallback because we'd need to fetch a specific file rather than
// synthesize a project corpus.
func parseGitHubRepo(raw string) (owner, repo string, ok bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false
	}
	host := strings.ToLower(u.Host)
	if host != "github.com" && host != "www.github.com" {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// applyGitHubAuth attaches GITHUB_TOKEN as a Bearer credential when set.
// Required for private repos and lifts the unauthenticated rate limit
// from 60 to 5000 requests/hour. Token is read fresh on every call so
// .env updates take effect without restart.
func applyGitHubAuth(req *http.Request) {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
}

// fetchGitHubReadme tries each casing in readmeCasings against
// raw.githubusercontent.com and returns the first 200. err==nil + body=="" + ok=false
// means none of the casings hit (caller falls back to API metadata path);
// any other error is returned verbatim.
func (i *Intake) fetchGitHubReadme(ctx context.Context, owner, repo string) (body string, ok bool, err error) {
	for _, name := range readmeCasings {
		url := fmt.Sprintf("%s/%s/%s/HEAD/%s", githubRawBase, owner, repo, name)
		body, status, err := i.fetchRawWithAuth(ctx, url)
		if err != nil && status != http.StatusNotFound {
			return "", false, err
		}
		if status == http.StatusOK {
			return body, true, nil
		}
		// 404 → try the next casing.
	}
	return "", false, nil
}

// fetchRawWithAuth is fetch + GitHub auth. Returns body, status, err.
// status is set even when err != nil so callers can distinguish 404 from
// network errors / rate limits.
func (i *Intake) fetchRawWithAuth(ctx context.Context, target string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", i.UserAgent)
	applyGitHubAuth(req)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("fetch %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode, fmt.Errorf("fetch %s: status %d", target, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, i.MaxBytes))
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("read body: %w", err)
	}
	return string(body), resp.StatusCode, nil
}

// errGitHubRepoNotFound is returned when api.github.com confirms the
// repo doesn't exist (or, for unauthenticated callers, is private —
// indistinguishable from missing).
var errGitHubRepoNotFound = errors.New("github repo not found (private, deleted, or typo)")

// fetchGitHubRepoMeta hits api.github.com/repos/{owner}/{repo}.
// Returns errGitHubRepoNotFound for 404 so callers can produce a precise
// user-facing error.
func (i *Intake) fetchGitHubRepoMeta(ctx context.Context, owner, repo string) (*githubRepoMeta, error) {
	target := fmt.Sprintf("%s/repos/%s/%s", githubAPIBase, owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", i.UserAgent)
	applyGitHubAuth(req)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errGitHubRepoNotFound
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github api %s: status %d (%s)", target, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var meta githubRepoMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("parse github meta: %w", err)
	}
	return &meta, nil
}

// fetchGitHubTree gets the top-level tree listing (recursive=0). Used to
// discover what root-level files exist so we can decide which to fetch.
func (i *Intake) fetchGitHubTree(ctx context.Context, owner, repo, branch string) (*githubTreeResp, error) {
	target := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s", githubAPIBase, owner, repo, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", i.UserAgent)
	applyGitHubAuth(req)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github tree: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("github tree %s: status %d (%s)", target, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tree githubTreeResp
	if err := json.NewDecoder(resp.Body).Decode(&tree); err != nil {
		return nil, fmt.Errorf("parse github tree: %w", err)
	}
	return &tree, nil
}

// pickInterestingFiles selects a prioritized list of root-level files to
// fetch from a tree. Top-level .md files (any casing) come first because
// repos without a README often have substantive design docs at root
// (KOVA_SPEC.md, DECISIONS.md, ARCHITECTURE.md). Manifests come second —
// names + dependencies tell the LLM what kind of project this is.
//
// We deliberately avoid fetching subdirectory contents in the MVP. They
// add complexity and rarely change Brief quality at the margin.
func pickInterestingFiles(tree []githubTreeEntry) []string {
	var mdFiles []string
	manifests := map[string]bool{}
	for _, m := range manifestFiles {
		manifests[m] = false
	}

	for _, e := range tree {
		if e.Type != "blob" {
			continue
		}
		// Top-level only — skip anything with a slash in the path.
		if strings.Contains(e.Path, "/") {
			continue
		}
		lower := strings.ToLower(e.Path)
		ext := strings.ToLower(path.Ext(e.Path))
		switch {
		case ext == ".md" || ext == ".markdown":
			mdFiles = append(mdFiles, e.Path)
		case manifests[e.Path] == false:
			// Not a manifest we care about; check exact name.
			if _, ok := manifests[e.Path]; ok {
				manifests[e.Path] = true
			}
			_ = lower
		}
	}

	// Mark which manifests are present.
	for _, e := range tree {
		if e.Type != "blob" || strings.Contains(e.Path, "/") {
			continue
		}
		if _, ok := manifests[e.Path]; ok {
			manifests[e.Path] = true
		}
	}

	// Sort .md files so README/CLAUDE/-spec/-business-style names come first;
	// stable order across runs makes test snapshots stable too.
	sort.Slice(mdFiles, func(a, b int) bool {
		return mdPriority(mdFiles[a]) < mdPriority(mdFiles[b])
	})

	out := append([]string(nil), mdFiles...)
	for _, m := range manifestFiles {
		if manifests[m] {
			out = append(out, m)
		}
	}
	return out
}

// mdPriority orders top-level .md files by likely usefulness for a Brief.
// Lower number = higher priority.
func mdPriority(filename string) int {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasPrefix(lower, "readme"):
		return 0
	case strings.Contains(lower, "spec"):
		return 1
	case strings.Contains(lower, "business"):
		return 2
	case strings.Contains(lower, "architecture") || strings.Contains(lower, "design"):
		return 3
	case strings.Contains(lower, "phase") || strings.Contains(lower, "roadmap"):
		return 4
	case strings.Contains(lower, "decision"):
		return 5
	case strings.Contains(lower, "future") || strings.Contains(lower, "todo"):
		return 6
	case strings.HasPrefix(lower, "claude"):
		return 7
	case strings.Contains(lower, "test") || strings.Contains(lower, "log"):
		return 9 // testing logs are usually noise for a Brief
	default:
		return 8
	}
}

// fetchGitHubFiles pulls the listed paths concurrently-respectful (serial,
// to keep it polite) and stops once the running total exceeds
// githubFetchTotalBytes. Each individual file is also capped at
// githubFetchPerFileBytes so a single bloated CHANGELOG can't dominate.
func (i *Intake) fetchGitHubFiles(ctx context.Context, owner, repo, branch string, paths []string) (map[string]string, error) {
	out := map[string]string{}
	total := 0
	for _, p := range paths {
		if total >= githubFetchTotalBytes {
			break
		}
		remaining := githubFetchTotalBytes - total
		perFileCap := min(githubFetchPerFileBytes, remaining)
		body, err := i.fetchGitHubFileBytes(ctx, owner, repo, branch, p, int64(perFileCap))
		if err != nil {
			// One file failing isn't fatal — surface in raw_content but
			// keep going.
			out[p] = fmt.Sprintf("(could not fetch %s: %v)", p, err)
			continue
		}
		out[p] = body
		total += len(body)
	}
	return out, nil
}

func (i *Intake) fetchGitHubFileBytes(ctx context.Context, owner, repo, branch, p string, cap int64) (string, error) {
	target := fmt.Sprintf("%s/%s/%s/%s/%s", githubRawBase, owner, repo, branch, p)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", i.UserAgent)
	applyGitHubAuth(req)
	resp, err := i.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, cap))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// synthesizeRepoCorpus builds the raw_content the LLM will see when the
// fallback path triggers. Keeps a stable structure so the intake prompt
// can reason about it: header with metadata, then file sections in
// fetch-order under "## File: <path>" markers.
func synthesizeRepoCorpus(meta *githubRepoMeta, files map[string]string, orderedPaths []string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# GitHub repository: %s\n\n", meta.FullName)
	if meta.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n\n", meta.Description)
	}
	if meta.Language != "" {
		fmt.Fprintf(&sb, "Primary language: %s\n", meta.Language)
	}
	if len(meta.Topics) > 0 {
		fmt.Fprintf(&sb, "Topics: %s\n", strings.Join(meta.Topics, ", "))
	}
	if meta.Stars > 0 {
		fmt.Fprintf(&sb, "Stars: %d\n", meta.Stars)
	}
	fmt.Fprintf(&sb, "Default branch: %s\n", meta.DefaultBranch)
	if meta.Private {
		sb.WriteString("Visibility: private\n")
	}
	sb.WriteString("\nThis repository has no README. The contents below are top-level documentation files and project manifests, fetched in priority order.\n\n---\n\n")

	for _, p := range orderedPaths {
		body, ok := files[p]
		if !ok {
			continue
		}
		fmt.Fprintf(&sb, "## File: %s\n\n%s\n\n---\n\n", p, body)
	}
	return sb.String()
}
