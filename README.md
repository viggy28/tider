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
- `github.com/spf13/viper` — config
- Official Anthropic Go SDK
- Official OpenAI Go SDK
- `github.com/yuin/goldmark` — markdown rendering
- Stdlib for HTTP, JSON, caching, cache management

No Reddit SDK. Public JSON endpoints via stdlib `net/http` only.

---

## Architecture

```
cmd/tider/               CLI entry (cobra)
intake/                  URL | topic | file → Brief
research/                Layered: subreddits.yaml + tiered cache + Reddit JSON
draft/                   Brief + research → structured Draft (JSON)
                         Renderer: Draft → reviewable markdown
suggest/                 Brief → candidate subs
engage/                  watch (triage) + reply (drafter), read-only
internal/reddit/         JSON client, User-Agent, backoff, tiered cache
internal/llm/            Provider-agnostic interface + Anthropic + OpenAI
internal/types/          Brief, Subreddit, Post, Rule, Draft, Reply, Session
prompts/                 Versioned prompt templates
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

`tider research <sub>` also stores the assembled raw research bundle at:

```
~/.tider/cache/research/{name}.json
```

That assembled bundle is reused for a short window so the pain-point
insight step can be rerun without another Reddit fetch. Use `--refresh`
to force a new Reddit fetch, or `--raw` to print the raw bundle without
LLM-generated insights.

By default, `research` prints a concise human report focused on pain-point
clusters, repeated asks, opportunity signals, community language, confidence,
and 3-5 evidence posts. Use `--render=json` for raw + insights JSON, or
`--render=insights-json` for only the structured insight report.
The insight step uses the configured `research` LLM task, which defaults to
`gpt-5` with a larger completion budget for reasoning-model headroom.

---

## Context bank

Reusable project and positioning context lives in:

```
~/.tider/contexts/
  kova.md
  streambed.md
  profile.md
```

Use it for durable notes you expect to reuse across thread engagement and
drafting workflows. Context files are plain Markdown so they can hold product
positioning, current audience, constraints, and "do not say" guidance.

Commands:

```bash
tider context import kova ./kova.md
tider context list
tider context show kova
tider context edit kova
```

`tider context show` also accepts direct file paths for ad hoc context review.
`tider context edit` opens `$EDITOR` on the saved context file, creating it if
needed.

---

## Session layout

```
~/.tider/projects/{project}/sessions/{date}-{slug}/
  brief.md
  research/{sub}.json
  drafts/{sub}.json    # structured
  drafts/{sub}.md      # rendered for review
  history.jsonl        # every regen with prompt + diff
```

`{project}` from config `projects.default` (default: `streambed`) or `--project` flag.
`{date}` auto-prepended to `--session=launch` → `2026-04-26-launch`.

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

Provider implementations live alongside: `anthropic.go`, `openai.go`. Factory in `factory.go` reads config and returns the right provider.

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
