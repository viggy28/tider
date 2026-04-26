# tider

A Go CLI co-pilot that drafts Reddit posts and reply suggestions for your projects, grounded in each subreddit's rules and what's worked there. You review, remix, and post manually.

## Core principle

**Bot reads Reddit, you write to Reddit.** Drafting and triage only. No posting, commenting, voting, or authentication anywhere in the binary. The capability is not wired in — by design.

---

## Capabilities

### Three input modes, one internal `Brief`
- `--url=...` — GitHub repo or blog post
- `--topic="..."` — short prompt; bot asks up to 3 clarifying questions interactively, then drafts
- `--file=...` — markdown brief you wrote yourself

### Per-sub research (layered sources)
- Curated `subreddits.yaml` knowledge layer (your notes, observations, never invalidated)
- Reddit JSON fetches with tiered TTL cache:
  - Rules / wiki / flairs: 7 days
  - Top (week, month): 24 hours
  - Hot: 6 hours
  - Stickies: always fresh
- Available flairs (many subs auto-remove unflaired posts)
- `--refresh` flag forces fresh fetch
- `confidence` field on output — small subs degrade gracefully

### Drafting (co-pilot output)
- Default: 2 angles per sub × 3 titles per angle × 2 bodies per angle (~12 artifacts per sub)
- `--variants=full` for 3 × 5 × 3 spread when you want more options
- Modular components (openers, hooks, closes) tagged for mix-and-match
- Per-sub recommendation with reasoning (which angle/title/body fits the sub's culture)
- Risk rating: `low` / `medium` / `high` / `refuse`
  - `refuse` generates nothing and explains why (forces deliberate decision)
- Flair recommendation, marked required vs optional
- Suggested post window (e.g., "Tue–Thu 9–11am ET")
- `media_recommendation` field when exemplars suggest visuals help (you create the asset)
- Posting schedule across subs to avoid cross-post spam filtering
- `--dry-run` shows research output and proposed strategy before generating

### Scoped regeneration (the real co-pilot loop)
```
tider regen titles --sub=dataengineering --angle=2 --note="more provocative"
tider regen body --sub=dataengineering --variant=2.3 --length=150
tider regen opener --sub=dataengineering --angle=2 --seed="The first thing that broke was..."
```

### Sub suggestion
You provide some target subs, bot suggests more from the Brief.

### Engagement (read-only)
- `tider watch <post-url>` — terminal triage of live thread
  - Polls comments, classifies new ones by type (`question` / `pushback` / `praise` / `off_topic` / `hostile`)
  - Priority score per comment
  - Neutral framing for technical pushback
  - Hostile comments surfaced and marked `ignore` (you should know they exist)
- `tider reply <comment-url>` — drafts 2–3 reply variants
  - Variants: acknowledge / push back / ask back
  - Willing to output `skip` when no reply is right
  - Refuses to draft replies to hostile comments

---

## Explicitly out of scope

- Reddit auth, OAuth, write actions of any kind
- Auto-posting, auto-replying, auto-anything
- Screenshot generation
- TUI picker (deferred to v1.5)
- Web UI
- Self-hosted models (clean abstraction left open, not implemented)
- Hacker News, LinkedIn, Twitter (different platforms, different tools)
- Keyword-watching across Reddit (v2)
- Performance tracking / engagement analytics (v3+)

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
internal/llm/            Provider-agnostic interface
                         Implementations: anthropic.go, openai.go
internal/types/          Brief, Subreddit, Post, Rule, Draft, Reply, Session
prompts/                 Versioned prompt templates (draft.tmpl, reply.tmpl, etc.)
```

### LLM abstraction
One `Provider` interface, two implementations (Anthropic, OpenAI). Per-task model selection via config. **No package outside `internal/llm` imports a provider SDK.**

```go
// internal/llm/llm.go
type Message struct {
    Role    string // "system" | "user" | "assistant"
    Content string
}

type Request struct {
    Model       string
    Messages    []Message
    MaxTokens   int
    Temperature float64
    JSONMode    bool   // structured output for draft generation
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

JSON mode handled idiomatically per provider (Anthropic via tool use, OpenAI native) — abstract the *intent*, not the *mechanism*.

### Default LLM
Anthropic, Claude Sonnet 4.7. Configurable per task.

### Drafts as JSON
Drafts are JSON under the hood, markdown rendered from them. Same source of truth for current markdown output and future TUI.

---

## File layout

```
~/.tider/
  config.yaml                              # provider, model, handle, author_context
  subreddits.yaml                          # curated knowledge layer
  cache/
    subs/{name}/
      about.json     rules.json     wiki_rules.md
      stickies.json  top_week.json  top_month.json
      hot.json       flairs.json    _meta.json
  projects/{project}/
    sessions/{date}-{slug}/
      brief.md
      research/{sub}.json
      drafts/{sub}.json
      drafts/{sub}.md
      history.jsonl                        # every regen with prompt + diff
```

**Project identity:** `default_project` in config (`streambed`), `--project=...` to override.

**Session naming:** `--session=launch` flag, date auto-prepended → `2026-04-26-launch`.

---

## Config

```yaml
# ~/.tider/config.yaml
reddit:
  handle: tider28          # baked into User-Agent: "tider/0.1 (by /u/tider28)"

llm:
  provider: anthropic
  model: claude-sonnet-4-7
  tasks:
    draft:   { provider: anthropic, model: claude-sonnet-4-7 }
    triage:  { provider: anthropic, model: claude-sonnet-4-7 }
    suggest: { provider: anthropic, model: claude-sonnet-4-7 }
    reply:   { provider: anthropic, model: claude-sonnet-4-7 }

projects:
  default: streambed

author_context: |
  Software engineering leader and serial founder with deep roots in Postgres
  and distributed systems. Five years at Cloudflare leading the Postgres /
  storage platform team (scaling to 1M+ QPS). Co-founder of Omnigres
  (Postgres-as-a-runtime). Currently building Streambed — a WAL-native CDC
  tool written in Go that pipes Postgres data into Iceberg/Parquet on S3
  and queries it via DuckDB over the Postgres wire protocol, with no
  external catalog dependencies and a single-binary architecture. Go-first
  builder; speaks at QConSF and other technical conferences.
```

**API keys via env vars only** (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`). Config tells `tider` *which* env var to read; never holds the key.

---

## Anti-spam / humane-voice safeguards

- Self-promo ratio analysis from top-post mix; `refuse` rating when sub culture rejects post type
- Anti-tells in prompt: no "🚀", "excited to share", "game-changer", three-bullet feature dumps, "Great question!" reply openers, "Let me know if you have feedback" closes
- Voice grounding via `author_context` so drafts reference real lived experience
- Exemplar bodies (not just titles) included in prompt for tone calibration
- Cross-post schedule recommendation to avoid same-day burst filtering
- Polite User-Agent + exponential backoff + tiered disk cache for Reddit fetches

---

## Build order

Each step ships standalone and is useful on its own.

1. **`internal/reddit/` + `research/`** — fetcher, tiered cache, `subreddits.yaml` reader, `--refresh`. Standalone; useful immediately.
2. **`internal/llm/` interface + Anthropic + OpenAI implementations** — small but needs to be right early.
3. **`intake/`** — URL and file paths first; interactive `--topic` last.
4. **`draft/`** — structured JSON output, markdown renderer, variant generation, versioned prompts, `--dry-run`.
5. **`regen` commands** — scoped regeneration of titles, bodies, components.
6. **`suggest/`** — short, builds on `draft/`.
7. **`engage/watch`** — terminal triage view.
8. **`engage/reply`** — informed by what watching teaches.

---

## Initial test target

Streambed launch push. Subs:
- `r/golang`
- `r/PostgreSQL`
- `r/dataengineering`
- `r/SideProject`
- `r/selfhosted`
- `r/databasedevelopment`

Bot suggests additions; you confirm.

---

## Dependencies

- Go 1.22+
- `github.com/spf13/cobra` (CLI)
- `github.com/spf13/viper` (config)
- Official Anthropic Go SDK
- Official OpenAI Go SDK
- `github.com/yuin/goldmark` (markdown rendering)
- Stdlib for HTTP, JSON, caching

No Reddit SDK needed — public JSON via stdlib `net/http`.

---

## Resolved decisions

| Decision | Choice |
|---|---|
| Project name | tider |
| Reddit handle | tider28 |
| Default provider/model | Anthropic, Claude Sonnet 4.7 |
| Variant default | 2 × 3 × 2 (~12 artifacts), `--variants=full` for more |
| Cache TTL | Tiered (rules 7d, top 24h, hot 6h, stickies always fresh), `--refresh` flag |
| Session naming | Date-prepended slug (`2026-04-26-launch`) |
| Project identity | Config default + `--project` override |
| Topic intake | Hard cap 3 questions |
| Refuse rating | Generates nothing, explains why |
| Hostile comments in `watch` | Surface and mark, not filter |
| Subreddit knowledge | Curated `subreddits.yaml` + tiered cache, layered |
| Reply variants | 2–3 (acknowledge / push back / ask back), can output `skip` |

---

## Design rationale (the why)

### Why no auth / no posting
Reddit's anti-bot machinery is aggressive. Auto-posting risks shadowbanning the account; auto-replying risks reputation damage in small technical communities (Postgres + dataeng worlds are small — one bad LLM-flavored reply and respected peers see it). Keeping the capability un-wired means it can't be turned on by accident or by a clever prompt.

### Why co-pilot output, not single drafts
First draft is rarely right. The actual workflow is "I like angle 2 but the titles are flat" → regenerate titles. Structured JSON + scoped regeneration matches this loop; flat single drafts force re-running the whole generation.

### Why provider-agnostic
Tool will be iterated on for months. Locking to one provider means rewriting `internal/llm/` later. The abstraction is small (couple hundred lines per provider) and pays for itself the first time you swap.

### Why versioned prompt files
The prompt that turns Brief + research into a good draft *is* the IP of this tool. It will be rewritten 30 times. Living in `prompts/draft.tmpl` lets you diff prompt changes against draft quality across runs.

### Why session-based state
Posting Streambed updates over months means same project, many briefs. Session structure builds a corpus, not a pile. `history.jsonl` per session captures every regen — over time, this is the real signal about which prompts consistently improve drafts.

### Why "what's worked" is form-not-content
A post hitting top of r/golang last month might have done so for reasons unrelated to the post itself (timing, who reshared it, news cycle). Exemplars teach *form* (length, structure, tone, link-vs-self) reliably; they teach *content strategy* unreliably. The prompt acknowledges this.

### The discipline
Every "wouldn't it be nice if it auto-posted at the optimal time" / "wouldn't it be nice if it auto-replied to easy questions" idea will be tempting. Each one moves the bot from drafting assistant to engagement bot, which is a category Reddit hates and your reputation shouldn't touch. **The bot's surface area should shrink over time as you learn what's noise, not grow.**
