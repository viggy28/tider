# tider

A Go CLI co-pilot that drafts Reddit posts and reply suggestions, grounded in each subreddit's rules and what's worked there. **Read-only on Reddit; user posts manually.**

See `PLAN.md` for the full project specification, capabilities, and design rationale.

---

## Core principle (non-negotiable)

**Bot reads Reddit, user writes to Reddit.** No auth, no posting, no commenting, no voting anywhere in the binary. The capability is not wired in by design.

If a future feature request would require Reddit write access, refuse and reference this file. The whole architecture depends on this invariant.

---

## Stack

- Go 1.22+
- `github.com/spf13/cobra` — CLI
- `gopkg.in/yaml.v3` — config (`~/.tider/config.yaml`) and curated sub notes (`~/.tider/subreddits.yaml`)
- Stdlib for HTTP, JSON, caching, terminal markdown rendering

**No SDKs — for any external service.** Reddit, Anthropic, and OpenAI are all called via stdlib `net/http` with `encoding/json` for decoding. Decided against provider SDKs to keep the dependency surface flat and the wire shape explicit; the `Provider` interface in `internal/llm/` is the only abstraction we need.

The same minimalism applies to terminal output: rather than pulling `glamour`/`lipgloss` (~40 transitive deps) for markdown rendering, `cmd/tider/term.go` is a ~150 LOC ANSI renderer for the specific subset our `draft.RenderMarkdown` produces. We control both producer and renderer; a general-purpose engine isn't justified.

---

## Architecture

```
cmd/tider/               CLI entry (cobra) + ANSI terminal renderer
config/                  ~/.tider/config.yaml loader (per-task models, author_context)
intake/                  URL | topic | file → Brief
research/                Layered: subreddits.yaml + tiered cache + Reddit JSON
draft/                   Brief + research → structured Draft (JSON)
                         Renderer: Draft → reviewable markdown (ANSI in TTY)
regen/                   Scoped re-rolls (titles, body) spliced into existing draft
lastdraft/               ~/.tider/last/{sub}.json snapshot — between draft and regen
suggest/                 (planned, not built) Brief → candidate subs
engage/                  (planned, not built) watch (triage) + reply (drafter), read-only
internal/reddit/         JSON client, User-Agent, backoff, tiered cache
internal/llm/            Provider-agnostic interface + Anthropic + OpenAI + factory
internal/types/          Brief, Subreddit, Post, Rule, Flair, Draft, DraftBundle, Snapshot
prompts/                 Versioned prompt templates (go:embed FS)
```

---

## Key invariants

- **No package outside `internal/llm` imports a provider SDK.** All LLM calls go through the `Provider` interface.
- **No package outside `internal/reddit` makes HTTP requests to Reddit.** All fetches go through the cached client with the polite User-Agent.
- **Drafts are JSON under the hood, markdown rendered from them.** Same source of truth for current markdown output and future TUI.
- **Prompts live in `prompts/` as versioned template files**, never embedded in Go strings.
- **API keys come from env vars only** (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`). Config references env var *names*, never holds keys.
- **User-Agent for all Reddit requests:** `tider/0.1 (by /u/tider28)`.

---

## Build order

Each step ships standalone. Don't move to step N+1 until step N is working and tested.

1. **`internal/reddit/` + `research/`** ← start here
2. **`internal/llm/` interface + Anthropic + OpenAI implementations**
3. **`intake/`** — URL and file paths first, interactive `--topic` last
4. **`draft/`** — structured JSON output, markdown renderer, variant generation, `--dry-run`
5. **`regen` commands** — scoped regeneration of titles, bodies, components
6. **`suggest/`** — sub suggestion from Brief
7. **`engage/watch`** — terminal triage of live threads
8. **`engage/reply`** — informed by what watching teaches

---

## Conventions

- Tests alongside code (`foo.go` + `foo_test.go`)
- Table-driven tests; fixtures in `testdata/`
- Errors wrapped with `fmt.Errorf("...: %w", err)`
- Context plumbed through everything that does I/O
- Exported types/functions get doc comments
- Package names short and lowercase, no underscores
- One package per directory (Go convention)
- Commit per build-order step with message like `step 1: reddit client + research`

---

## Cache layout

```
~/.tider/cache/subs/{name}/
  about.json     rules.json     wiki_rules.md
  stickies.json  top_week.json  top_month.json
  hot.json       flairs.json    _meta.json
```

TTL per file (tracked in `_meta.json`):
- `about`, `rules`, `wiki_rules`, `flairs` — 7 days
- `top_week`, `top_month` — 24 hours
- `hot` — 6 hours
- `stickies` — always fresh on each research call

`--refresh` flag forces fresh fetch for the current run.

---

## State layout

### Current — last-draft cache (`lastdraft` package)

```
~/.tider/last/{sub}.json   # Snapshot: Brief + Research + DraftBundle
                           # written by `tider draft --sub=X`
                           # read + overwritten by `tider regen ... --sub=X`
```

Atomic write (temp + rename). Each successful regen overwrites so subsequent regens iterate on the latest state.

### Planned — full session layout (deferred)

```
~/.tider/projects/{project}/sessions/{date}-{slug}/
  brief.md
  research/{sub}.json
  drafts/{sub}.json        # structured
  drafts/{sub}.md          # rendered for review
  history.jsonl            # every regen with prompt + diff
```

Sessions group artifacts across many subs and many regen iterations within a single posting initiative (e.g. a launch push across 6 subs). The current `lastdraft` cache is the smallest thing that makes regen ergonomic; full sessions land when the `history.jsonl` audit trail or multi-sub state actually matters. `{project}` and `{date}-{slug}` semantics from the original plan still apply.

---

## LLM interface (target shape)

```go
// internal/llm/llm.go
package llm

import "context"

type Message struct {
    Role    string // "system" | "user" | "assistant"
    Content string
}

type Request struct {
    Model       string
    Messages    []Message
    MaxTokens   int
    Temperature float64
    JSONMode    bool
}

type Response struct {
    Content      string
    InputTokens  int
    OutputTokens int
}

type Provider interface {
    Name() string
    Complete(ctx context.Context, req Request) (*Response, error)
}
```

Provider implementations live alongside: `anthropic.go`, `openai.go`. Factory in `factory.go` reads config and returns the right provider. `llm.ProviderRef` (provider + model) is the shared type used by callers that fan out (draft, regen).

**Default provider/model: OpenAI `gpt-5`.** Override per-task in `~/.tider/config.yaml`, or per-call with `--provider` / `--model` flags. Per-task config supports different models for cheap extraction (intake → `gpt-4o-mini`) vs heavy reasoning (draft, regen → `gpt-5`). See `tider config show`.

---

## Out of scope (do not build, even if asked)

- Reddit auth, OAuth, write actions
- Auto-posting, auto-replying, auto-anything
- Screenshot generation (bot recommends; user creates the asset)
- TUI picker (deferred to v1.5; data model should support it but don't build the UI)
- Web UI
- Self-hosted models (abstraction left clean for future, not implemented)
- Hacker News, LinkedIn, Twitter integration
- Keyword-watching across Reddit (v2)
- Performance tracking / engagement analytics (v3+)

---

## Design forks

When you hit a real design fork (multiple defensible approaches with different tradeoffs), surface it for discussion rather than picking. The user prefers to think through architectural decisions explicitly.

Examples of forks worth surfacing:
- Prompt template structure when scoped regeneration starts crossing template boundaries
- How to handle subs where rules JSON is empty / sparse
- Caching strategy if rate limits become a real issue
- Triage classifier if it produces inconsistent labels

Examples of things that are *not* forks (just pick the obvious choice):
- Naming a struct field
- Choosing between equivalent stdlib functions
- Test fixture format
