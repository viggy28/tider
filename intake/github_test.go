package intake

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeGitHub spins up an httptest.Server that mimics the slice of GitHub
// API + raw.githubusercontent surface that intake/github.go uses. Tests
// pass a routing function so each test composes the responses it cares
// about.
type fakeGitHub struct {
	t       *testing.T
	hits    map[string]*int32 // path → count, for verifying which endpoints we hit
	apiSrv  *httptest.Server
	rawSrv  *httptest.Server
	handler func(kind, path string, w http.ResponseWriter, r *http.Request)
}

func newFakeGitHub(t *testing.T, handler func(kind, path string, w http.ResponseWriter, r *http.Request)) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{t: t, hits: map[string]*int32{}, handler: handler}
	f.apiSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.recordHit("api", r.URL.Path)
		handler("api", r.URL.Path, w, r)
	}))
	f.rawSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.recordHit("raw", r.URL.Path)
		handler("raw", r.URL.Path, w, r)
	}))
	t.Cleanup(func() {
		f.apiSrv.Close()
		f.rawSrv.Close()
	})

	// Point package URLs at the test servers and restore at end of test.
	prevAPI, prevRaw := githubAPIBase, githubRawBase
	githubAPIBase = f.apiSrv.URL
	githubRawBase = f.rawSrv.URL
	t.Cleanup(func() {
		githubAPIBase = prevAPI
		githubRawBase = prevRaw
	})
	return f
}

func (f *fakeGitHub) recordHit(kind, path string) {
	key := kind + ":" + path
	if _, ok := f.hits[key]; !ok {
		var n int32
		f.hits[key] = &n
	}
	atomic.AddInt32(f.hits[key], 1)
}

func (f *fakeGitHub) hitCount(kind, path string) int {
	if c, ok := f.hits[kind+":"+path]; ok {
		return int(atomic.LoadInt32(c))
	}
	return 0
}

func TestParseGitHubRepo(t *testing.T) {
	cases := []struct {
		in    string
		owner string
		repo  string
		ok    bool
	}{
		{"https://github.com/viggy28/streambed", "viggy28", "streambed", true},
		{"https://github.com/viggy28/streambed/", "viggy28", "streambed", true},
		{"https://www.github.com/Owner/Repo", "Owner", "Repo", true},
		{"https://github.com/owner/repo/blob/main/README.md", "", "", false},
		{"https://github.com/owner", "", "", false},
		{"https://gitlab.com/owner/repo", "", "", false},
		{"not a url", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		o, r, ok := parseGitHubRepo(c.in)
		if ok != c.ok || o != c.owner || r != c.repo {
			t.Errorf("parseGitHubRepo(%q) = (%q, %q, %v); want (%q, %q, %v)", c.in, o, r, ok, c.owner, c.repo, c.ok)
		}
	}
}

func TestFromURLGitHubReadmeCasingFallback(t *testing.T) {
	// README.md 404, readme.md 200 → should succeed via the second casing.
	const body = "# My project\nThe whole real thing."
	gh := newFakeGitHub(t, func(kind, path string, w http.ResponseWriter, r *http.Request) {
		if kind == "raw" && strings.HasSuffix(path, "/readme.md") {
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: gh.apiSrv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	brief, err := i.FromURL(context.Background(), "https://github.com/owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief.RawContent, "The whole real thing") {
		t.Errorf("readme body not preserved; raw=%q", brief.RawContent)
	}
	if gh.hitCount("raw", "/owner/repo/HEAD/readme.md") != 1 {
		t.Errorf("expected fallback to readme.md, hits=%v", gh.hits)
	}
	if gh.hitCount("api", "/repos/owner/repo") != 0 {
		t.Errorf("API path should not have been hit when a casing succeeded")
	}
}

func TestFromURLGitHubAPIFallbackWhenAllREADMEsAre404(t *testing.T) {
	// All README casings 404 → API fallback path triggers.
	gh := newFakeGitHub(t, func(kind, path string, w http.ResponseWriter, r *http.Request) {
		switch {
		case kind == "raw" && strings.Contains(path, "/HEAD/"):
			// All README casings missing.
			w.WriteHeader(http.StatusNotFound)
		case kind == "api" && path == "/repos/owner/repo":
			_ = json.NewEncoder(w).Encode(githubRepoMeta{
				Name: "repo", FullName: "owner/repo",
				Description:   "A small CLI tool",
				Topics:        []string{"go", "cli"},
				Language:      "Go",
				DefaultBranch: "main",
				Stars:         42,
			})
		case kind == "api" && path == "/repos/owner/repo/git/trees/main":
			_ = json.NewEncoder(w).Encode(githubTreeResp{Tree: []githubTreeEntry{
				{Path: "SPEC.md", Type: "blob"},
				{Path: "DECISIONS.md", Type: "blob"},
				{Path: "go.mod", Type: "blob"},
				{Path: "cmd", Type: "tree"},
				{Path: "internal/types/types.go", Type: "blob"}, // nested, should be skipped
			}})
		case kind == "raw" && strings.HasSuffix(path, "/main/SPEC.md"):
			_, _ = w.Write([]byte("# Spec\nThe spec body."))
		case kind == "raw" && strings.HasSuffix(path, "/main/DECISIONS.md"):
			_, _ = w.Write([]byte("# Decisions\nWhy we built it this way."))
		case kind == "raw" && strings.HasSuffix(path, "/main/go.mod"):
			_, _ = w.Write([]byte("module example.com/repo\n\ngo 1.22"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: gh.apiSrv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	brief, err := i.FromURL(context.Background(), "https://github.com/owner/repo")
	if err != nil {
		t.Fatal(err)
	}

	// Corpus header should include metadata fields the LLM needs.
	wants := []string{
		"GitHub repository: owner/repo",
		"A small CLI tool",
		"Primary language: Go",
		"Topics: go, cli",
		"Stars: 42",
		"Default branch: main",
		"This repository has no README",
		"## File: SPEC.md",
		"The spec body.",
		"## File: DECISIONS.md",
		"Why we built it this way.",
		"## File: go.mod",
		"module example.com/repo",
	}
	for _, w := range wants {
		if !strings.Contains(brief.RawContent, w) {
			t.Errorf("corpus missing %q\n--- corpus ---\n%s", w, brief.RawContent)
		}
	}
	// Subdirectory file should not appear.
	if strings.Contains(brief.RawContent, "internal/types/types.go") {
		t.Errorf("nested file leaked into corpus")
	}
	// All five README casings + API meta + tree + 3 file fetches.
	if gh.hitCount("raw", "/owner/repo/HEAD/README.md") == 0 {
		t.Errorf("expected README.md to be tried")
	}
	if gh.hitCount("api", "/repos/owner/repo") != 1 {
		t.Errorf("expected api meta hit, hits=%v", gh.hits)
	}
	if gh.hitCount("api", "/repos/owner/repo/git/trees/main") != 1 {
		t.Errorf("expected api tree hit")
	}
}

func TestFromURLGitHubAPIRepoNotFound(t *testing.T) {
	// All raw 404 + API 404 → clear "repo not found" error.
	gh := newFakeGitHub(t, func(kind, path string, w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: gh.apiSrv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.FromURL(context.Background(), "https://github.com/owner/missing")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"github.com/owner/missing not found", "private", "GITHUB_TOKEN", "typo"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q: %v", want, err)
		}
	}
}

func TestFromURLGitHubNoExtractableContent(t *testing.T) {
	// Repo exists per API, but no top-level .md or manifests → clear
	// suggestion to use --file or write notes manually.
	gh := newFakeGitHub(t, func(kind, path string, w http.ResponseWriter, r *http.Request) {
		switch {
		case kind == "raw":
			w.WriteHeader(http.StatusNotFound)
		case path == "/repos/owner/empty":
			_ = json.NewEncoder(w).Encode(githubRepoMeta{FullName: "owner/empty", DefaultBranch: "main"})
		case path == "/repos/owner/empty/git/trees/main":
			// Only subdirectories at root, no top-level .md or manifests.
			_ = json.NewEncoder(w).Encode(githubTreeResp{Tree: []githubTreeEntry{
				{Path: "src", Type: "tree"},
				{Path: ".gitignore", Type: "blob"},
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: gh.apiSrv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.FromURL(context.Background(), "https://github.com/owner/empty")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no README") || !strings.Contains(err.Error(), "tider intake --file") {
		t.Errorf("error should suggest --file fallback, got: %v", err)
	}
}

func TestGitHubTokenAttachedWhenEnvSet(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_test_token")

	var sawAuth string
	gh := newFakeGitHub(t, func(kind, path string, w http.ResponseWriter, r *http.Request) {
		if kind == "raw" && strings.HasSuffix(path, "/HEAD/README.md") {
			sawAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("# README\nbody"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: gh.apiSrv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	if _, err := i.FromURL(context.Background(), "https://github.com/owner/repo"); err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer ghp_test_token" {
		t.Errorf("Authorization header missing/wrong: %q", sawAuth)
	}
}

func TestGitHubTokenAbsentNoAuthHeader(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	var sawAuth string
	gh := newFakeGitHub(t, func(kind, path string, w http.ResponseWriter, r *http.Request) {
		if kind == "raw" && strings.HasSuffix(path, "/HEAD/README.md") {
			sawAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte("# README\nbody"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{HTTP: gh.apiSrv.Client(), UserAgent: DefaultUserAgent, Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	if _, err := i.FromURL(context.Background(), "https://github.com/owner/repo"); err != nil {
		t.Fatal(err)
	}
	if sawAuth != "" {
		t.Errorf("Authorization header should be empty, got %q", sawAuth)
	}
}

func TestPickInterestingFilesPriorityAndScope(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "TESTING_LOG.md", Type: "blob"},
		{Path: "FUTURE.md", Type: "blob"},
		{Path: "DECISIONS.md", Type: "blob"},
		{Path: "KOVA_BUSINESS.md", Type: "blob"},
		{Path: "KOVA_SPEC.md", Type: "blob"},
		{Path: "PHASES.md", Type: "blob"},
		{Path: "CLAUDE.md", Type: "blob"},
		{Path: "go.mod", Type: "blob"},
		{Path: "Cargo.toml", Type: "blob"},   // present alongside go.mod, both fetched
		{Path: "package-lock.json", Type: "blob"}, // not in manifest list, skip
		{Path: ".gitignore", Type: "blob"},   // skip
		{Path: "internal/foo.go", Type: "blob"}, // nested, skip
		{Path: "cmd", Type: "tree"},          // tree, skip
	}
	got := pickInterestingFiles(tree)

	// Expectations:
	// - .md files in priority order: SPEC, BUSINESS, PHASES, DECISIONS, FUTURE, CLAUDE, TESTING_LOG
	//   (per mdPriority: spec=1, business=2, phase=4, decision=5, future=6, claude=7, log=9)
	// - Manifests appended in their declaration order: go.mod, Cargo.toml
	wantOrder := []string{
		"KOVA_SPEC.md", "KOVA_BUSINESS.md", "PHASES.md", "DECISIONS.md",
		"FUTURE.md", "CLAUDE.md", "TESTING_LOG.md",
		"go.mod", "Cargo.toml",
	}
	if len(got) != len(wantOrder) {
		t.Fatalf("len mismatch: got %d (%v), want %d (%v)", len(got), got, len(wantOrder), wantOrder)
	}
	for i := range got {
		if got[i] != wantOrder[i] {
			t.Errorf("position %d: got %q, want %q", i, got[i], wantOrder[i])
		}
	}
}

func TestPickInterestingFilesNoNoise(t *testing.T) {
	// No .md, no manifests — should return empty.
	tree := []githubTreeEntry{
		{Path: "src", Type: "tree"},
		{Path: ".gitignore", Type: "blob"},
		{Path: ".github/workflows/ci.yml", Type: "blob"},
	}
	if got := pickInterestingFiles(tree); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSynthesizeRepoCorpusOmitsEmptyFields(t *testing.T) {
	meta := &githubRepoMeta{FullName: "x/y", DefaultBranch: "main"}
	corpus := synthesizeRepoCorpus(meta, map[string]string{}, nil)
	// No description / topics / language / stars → those lines absent.
	for _, absent := range []string{"Description:", "Primary language:", "Topics:", "Stars:"} {
		if strings.Contains(corpus, absent) {
			t.Errorf("corpus should not include %q when meta lacks it", absent)
		}
	}
	// Default branch always present.
	if !strings.Contains(corpus, "Default branch: main") {
		t.Errorf("default branch should be in corpus")
	}
}
