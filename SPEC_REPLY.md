# Spec: flat command structure + `tider reply`

**Status: implemented.** This document started as a forward spec and was updated as implementation landed. See the **Decisions log** at the bottom for changes made during the work.

## Goal

Two changes that ship together on this branch:

1. **Rename `tider draft` → `tider post`.** Drop the redundant `draft` prefix. Every command in tider produces a draft by design.
2. **Add `tider reply`** for drafting replies inside an existing Reddit thread, with two internal modes (reply, review).

After this lands, the CLI is uniformly flat:

```
tider intake     --url=... | --file=...
tider research   <sub>
tider post       --brief=... --sub=...
tider reply      --url=... --context=...
tider regen      titles ...
tider config     show | init
tider context    import | list | show | edit
```

## Why

- `tider draft post` reads as "draft draft." The no-posting invariant is documented elsewhere and reinforced by behavior; the command path doesn't need to repeat it.
- Three of four existing user-facing commands already drop the prefix (`intake`, `research`, `regen`). `draft` is the inconsistent one.
- `post` and `reply` have different inputs, prompts, outputs, and session shapes; making them peers reflects that.

## Non-goals

- No backward-compatible alias for `tider draft`. Cleanly removed; cobra returns "unknown command" for it.
- No Reddit posting, OAuth, write actions.
- No `--inspect=none|links|full` flag.
- No `--thread-only`, `--save-brief`.
- No manual brief construction for routine reply drafting.
- No use of comments to trigger review mode.
- No frontmatter parser for context files (prose convention only).
- No image/screenshot review inspection in this branch (deferred — return clear "image-based inspection not yet implemented" if the situation calls for it).

---

## Command shapes

### `tider post` (renamed from `tider draft`)

```
tider post --brief=brief.json --sub=databases [--render=...] [--variants=...] [--dry-run]
```

Identical behavior to today's `tider draft`. Pure rename — same package, same flags, same outputs.

### `tider reply`

```
tider reply --url=<reddit-thread-url> --context=kova [--context=...] [--render=...]
```

Fetches the thread, detects mode (reply or review) from OP only, loads contexts, drafts reply variants, persists a session.

Flags (as implemented):

- `--url` — required
- `--context` — repeatable; resolved via `contextbank.Load` (id from bank, or direct file path)
- `--render=json|markdown` — TTY-aware default (markdown in TTY, json when piped)
- `--provider` — singular; overrides drafter provider (default from config `tasks.reply`)
- `--model` — singular; overrides drafter model (default from config `tasks.reply`)
- `--max-tokens` — config-backed default

Note: an earlier draft of this spec listed `--providers` (plural) plus `--openai-model` / `--anthropic-model` per the post-command pattern. Reply uses a single LLM call; singular flags reflect that accurately. See decisions log.

Not implemented in v1 (deferred):
- `--refresh` — bypass thread fetch cache. Threads aren't cached today.
- `--session=<id>` — resume/re-render from saved session (mentioned in original plan as a later-pass feature).

---

## Mode detection (`tider reply`)

Two internal modes:

- `reply` — normal discussion thread; draft from OP + selected comments + context.
- `review` — OP asks for review/feedback/critique of a shop/site/listing; draft from OP + inspected target + context.

### Mechanism: LLM-driven classifier

A small focused prompt (`prompts/reply_mode.tmpl`) takes title, flair, body, and outbound URL. Returns:

```json
{
  "mode": "reply" | "review",
  "reason": "short explanation",
  "target_urls": ["https://..."]
}
```

Rationale: trigger phrasings vary infinitely. Pattern matching on "review", "feedback", "critique" is brittle — false-positives on "review of golang generics", false-negatives on "thoughts on my Etsy shop?". ~$0.0005 per call, single round-trip, robust.

Cheaper model (e.g. `gpt-4o-mini`) is sufficient. Configured under `llm.tasks.reply_mode` in config, defaulting to mini.

### Detection inputs (OP only)

- subreddit
- title
- flair
- body / selftext
- outbound URL if link post

**Comments are explicitly not used for detection.** A comment saying "can someone review mine too?" must not flip the mode.

### Review-mode contract

If mode is `review`:

- Inspection of the linked target is **required**.
- If no target URL exists in OP → fail clearly: *"Review request detected, but no shop/site URL was found in the original post."*
- If inspection fails → fail clearly, preserve session.
- Do **not** generate generic review advice as a fallback.

### Target URL extraction

The classifier returns `target_urls` it noticed. We also parse the OP outbound URL + markdown links + raw URLs + obvious bare hostnames (`example.com`) as a fallback signal. If the classifier returned a grounded target, it wins; otherwise take the first non-image URL found.

Bare hostnames are normalized to `https://...` when they look like public domains. This covers common review posts that paste `example.com` without a protocol while still filtering Reddit and image links.

---

## Context bank

Existing `contextbank` package handles both id and path resolution via `Load(dir, ref)`.

```go
entry, err := contextbank.Load(dir, "kova")           // ~/.tider/contexts/kova.md
entry, err := contextbank.Load(dir, "./kova.md")      // direct path
```

Repeatable flag: `--context=kova --context=profile --context=./notes.md`.

Contexts are snapshotted into the session at load time. If `kova.md` changes later, the session remains reproducible.

### `author_context` semantics

`author_context` from `~/.tider/config.yaml` continues to flow into reply prompts, separate from `--context`:

- `author_context` → **voice grounding** ("write like this person")
- `--context` → **project material** (what the user knows about kova / streambed / etc.)

Both governed by the no-pitch rule by default.

### "Naming allowed" convention

Default prompt rule: *"Use context as background. Do not name or pitch the project unless the context body explicitly says naming is allowed."*

Context files communicate naming permission via prose, not frontmatter:

```markdown
[in-thread naming: ok if directly relevant]

Kova is a tool for handmade Etsy sellers...
```

The LLM reads the prose and complies. No new schema.

---

## Session persistence (`tider reply` only)

`tider post` continues to use `lastdraft` (single snapshot per sub). `tider reply` gets full sessions because review threads are long-running and have richer artifacts.

```
~/.tider/sessions/replies/{date}-{subreddit}-{post_id}/
  thread.json          # fetched Reddit thread (post + selected comments)
  contexts.json        # loaded contexts, snapshotted verbatim
  mode.json            # mode detection result with reason
  draft-input.json     # assembled inputs to the draft prompt
  drafts.json          # generated reply variants
  output.md            # rendered output

  # Review-mode-only:
  target.json          # target URL + parsed metadata
  inspection.json      # inspection results (text fetch, parsed sections)
  review-notes.json    # structured review notes derived from inspection
```

Session path is printed to stderr early so the user can find it before drafting completes:

```
session: ~/.tider/sessions/replies/2026-04-30-shopify-1t06474
```

Artifacts are written incrementally as each step completes. Files for not-yet-implemented steps are not created (no empty fakes).

---

## Reddit URL parsing

Support these forms:

```
https://www.reddit.com/r/<sub>/comments/<id>/<slug>/
https://reddit.com/comments/<id>/
https://old.reddit.com/r/<sub>/comments/<id>/...
https://np.reddit.com/r/<sub>/comments/<id>/...
https://redd.it/<id>                                 # short link, requires HEAD to resolve
```

Anything else → clear parse error.

---

## Comment fetching

For reply context (not mode detection):

- Top 20 comments by score, any depth (flatten the tree)
- Stored in `thread.json` as a flat list with `parent_id` preserved

The earlier "hard cap at 50" line was redundant — once we sort by score and take the top 20, additional caps don't add value. Implementation walks the full comment tree, sorts, takes top 20.

---

## Data shapes

Types live in `internal/types/` (matching the existing pattern: `Brief`, `Research`, `Snapshot`, etc. all live there for cross-package use). Names are prefixed with `Reply` to avoid collisions in the shared namespace.

```go
package types

import "time"

type Thread struct {
    URL         string    `json:"url"`
    Subreddit   string    `json:"subreddit"`
    PostID      string    `json:"post_id"`
    Title       string    `json:"title"`
    Body        string    `json:"body"`
    Author      string    `json:"author"`
    Flair       string    `json:"flair,omitempty"`
    OutboundURL string    `json:"outbound_url,omitempty"`
    Comments    []Comment `json:"comments"`
    FetchedAt   time.Time `json:"fetched_at"`
}

type Comment struct {
    ID         string  `json:"id"`
    ParentID   string  `json:"parent_id,omitempty"`
    Author     string  `json:"author"`
    Body       string  `json:"body"`
    Score      int     `json:"score"`
    CreatedUTC float64 `json:"created_utc"`
}

type ReplyMode string

const (
    ReplyModeReply  ReplyMode = "reply"
    ReplyModeReview ReplyMode = "review"
)

type ReplyModeResult struct {
    Mode       ReplyMode `json:"mode"`
    Reason     string    `json:"reason,omitempty"`
    TargetURLs []string  `json:"target_urls,omitempty"`
}

type LoadedReplyContext struct {
    ID     string `json:"id,omitempty"` // empty if loaded by path
    Source string `json:"source"`        // "bank" | "path"
    Path   string `json:"path"`
    Body   string `json:"body"` // verbatim file contents
}

type ReplyDraft struct {
    ID        string `json:"id"`
    Label     string `json:"label"` // "best" | "short" | "detailed" | "question-first"
    Text      string `json:"text"`
    Reasoning string `json:"reasoning,omitempty"`
}

type ReplyBundle struct {
    ThreadURL string       `json:"thread_url"`
    Subreddit string       `json:"subreddit"`
    Mode      ReplyMode    `json:"mode"`
    Drafts    []ReplyDraft `json:"drafts"`
    PickID    string       `json:"pick_id,omitempty"`
    Generated time.Time    `json:"generated"`
}

// Review-mode-only:

type Inspection struct {
    URL             string    `json:"url"`
    Status          int       `json:"status"`
    Title           string    `json:"title,omitempty"`
    MetaDescription string    `json:"meta_description,omitempty"`
    OGTitle         string    `json:"og_title,omitempty"`
    OGDescription   string    `json:"og_description,omitempty"`
    Headings        []Heading `json:"headings,omitempty"`
    Snippets        []string  `json:"snippets,omitempty"`
    FetchedAt       time.Time `json:"fetched_at"`
}

type Heading struct {
    Level int    `json:"level"`
    Text  string `json:"text"`
}

type ReviewNotes struct {
    TargetURL     string    `json:"target_url"`
    Strengths     []string  `json:"strengths,omitempty"`
    Weaknesses    []string  `json:"weaknesses,omitempty"`
    Suggestions   []string  `json:"suggestions,omitempty"`
    OpenQuestions []string  `json:"open_questions,omitempty"`
    Generated     time.Time `json:"generated"`
}
```

File ↔ type mapping:
- `thread.json` ↔ `types.Thread`
- `mode.json` ↔ `types.ReplyModeResult`
- `contexts.json` ↔ `[]types.LoadedReplyContext`
- `draft-input.json` ↔ `reply.DraftInput` (reply mode) or `reply.ReviewDraftInput` (review mode)
- `drafts.json` ↔ `types.ReplyBundle`
- `output.md` ↔ rendered markdown
- `target.json` (review only) ↔ `{url, alternatives, reason}` map
- `inspection.json` (review only) ↔ `types.Inspection`
- `review-notes.json` (review only) ↔ `types.ReviewNotes`

---

## Prompt rules (reply drafting)

Inherits the existing draft anti-tells, plus reply-specific:

- No "Great question!" / "Thanks for sharing!" openers
- No project sales pitches
- No links to user's projects unless context explicitly allows
- No "I built a tool" framing
- Write as practical peer advice
- Cite only things visible in the thread (or inspection, in review mode)
- Avoid generic "improve SEO / run ads / post more" unless inspection supports it
- Naming the project requires explicit permission in the context body

---

## Rendering

Markdown shape:

```markdown
# Reply drafts for r/<sub>

Thread: <thread title>
Mode: reply | review
Session: ~/.tider/sessions/replies/...

## Best Pick

<text>

## Alternatives

### Short
<text>

### Detailed
<text>

### Question-first
<text>
```

TTY-aware: ANSI in terminal, raw markdown when piped. JSON render emits `Bundle`.

---

## Commit plan (as shipped)

The original spec called for 10 commits in a strict order. Implementation collapsed CLI wiring (planned commit 9) into commit 7 to make reply mode end-to-end testable mid-branch. Final order on `feat-reply-and-rename`:

| # | Commit | Notes |
|---|---|---|
| 1 | `spec: tider draft → tider post + add tider reply` | This document. |
| 2 | `rename: tider draft → tider post` | Pure rename. No backward-compat alias. |
| 3 | `reddit: thread fetcher + URL parser` | All 5 URL forms + bonus `new.reddit.com`; thread fetch flattens comments by score. |
| 4 | `reply: LLM-driven mode classifier (reply vs review)` | OP-only inputs; classifier returns `{mode, reason, target_urls}`. URL extraction de-duped against body. |
| 5 | `reply: session manager` | `~/.tider/sessions/replies/<slug>/`; atomic JSON writes via temp+rename. |
| 6 | `reply: context loading with snapshot bodies` | Wraps `contextbank.Load`. |
| 7 | `reply: drafter + render + CLI (reply mode end-to-end)` | Combined commits 7+9 from original plan. Reply mode usable. Review mode errors with "not yet implemented" + saves target.json. |
| 8 | `reply: write draft-input.json + match spec render header` | Spec-alignment fixes from the audit pass. |
| 9 | `reply: review-mode inspection + notes + drafter` | This commit. `golang.org/x/net/html` for HTML parsing, two new prompt templates, full review pipeline wired. |
| 10 | `spec: close-out — match spec to what shipped` | Updates spec to reflect type location, flag names, commit reordering, dep addition. |

Total shipped: ~2400 LOC implementation + ~1200 LOC tests across 10 commits.

---

## Acceptance criteria

### Rename

1. `tider post --brief=brief.json --sub=...` produces drafts identical to former `tider draft`.
2. `tider draft` returns cobra's "unknown command" (no migration message, no alias).
3. `tider post --help` reflects renamed surface.
4. `Claude.md` and `README.md` reference `tider post`, not `tider draft`.

### `tider reply`

1. `tider reply --url=<reddit-thread-url> --context=kova` creates a session directory under `~/.tider/sessions/replies/`.
2. Session path printed to stderr at the start of the run.
3. `thread.json` written after Reddit fetch.
4. `contexts.json` written after context loading; bodies snapshotted verbatim.
5. `mode.json` written after mode detection.
6. Mode detection uses OP only (title/flair/body/outbound URL). Comments do not flip the mode.
7. Review mode with no OP target URL fails clearly without writing fake drafts.
8. Review mode with target URL writes `target.json`, runs text inspection, writes `inspection.json` + `review-notes.json`, produces grounded drafts.
9. Reply mode produces `drafts.json` and `output.md`.
10. Contexts referenced by name (`--context=kova`) resolve via `contextbank.Load`; contexts referenced by path (`--context=./notes.md`) resolve directly.
11. No Reddit auth or write behavior introduced.

### Tests

12. Reddit URL parsing — all five supported forms + clear error on malformed.
13. Mode detection from OP title/body/flair (LLM mocked).
14. Comments do not trigger review mode (corpus-with-review-comment-but-non-review-OP test).
15. Context loading by id and path (mixed list).
16. Session path generation, artifact writes, incremental write order.
17. Review mode missing-target failure preserves session.
18. Review mode inspection success path (text-only).
19. Review mode inspection failure preserves session, no fake drafts.
20. `go test ./... && go vet ./... && go build ./cmd/tider` all pass.

---

## Open questions

(Resolved during this session — left here for context.)

- ~~Mode detection: LLM or heuristic?~~ → LLM-driven (cheaper model, robust).
- ~~Backward compat for `tider draft`?~~ → No alias, hard remove.
- ~~`author_context` vs `--context` separation?~~ → voice vs project material; both flow in.
- ~~"Naming allowed" convention?~~ → prose in context body, no frontmatter.
- ~~Session path?~~ → `~/.tider/sessions/replies/...`.
- ~~Comment fetching depth?~~ → top 20 by score, any depth, max 50.

## Decisions log

Changes made during implementation, with reasoning.

### 2026-04-30: Type location — `internal/types`, not `package reply`

Spec originally placed `Thread`, `Mode`, `ModeResult`, `LoadedContext`, `Draft`, `Bundle` in `package reply`. Implementation puts them in `internal/types/` (with `Reply` prefix on the reply-specific names) for consistency with the existing pattern: `Brief`, `Research`, `Snapshot`, `DraftBundle`, etc. all live in `internal/types/`. Cross-package types in tider are centralized.

Names: `ReplyMode`, `ReplyModeResult`, `LoadedReplyContext`, `ReplyDraft`, `ReplyBundle`. Plus the review-only `Inspection`, `Heading`, `ReviewNotes`. Thread + Comment didn't need prefixes because they're not reply-specific (no other package owns them).

### 2026-04-30: Singular `--provider` and `--model` instead of plural fan-out flags

Spec listed `--providers`, `--openai-model`, `--anthropic-model` (inherited from the post-command pattern designed for fan-out). Reply uses a single LLM call per spec line 368 ("Single LLM call, returns Bundle with 3-4 labeled variants"); singular flag names accurately reflect that. Multi-provider fan-out would add no signal here — the variants come from prompt structure, not from comparing providers.

### 2026-04-30: CLI wiring rolled into commit 7

Original plan put `cmd/tider/reply.go` as commit 9, after review-mode inspection in commit 8. Implementation rolled CLI into commit 7 (with a "review mode not implemented yet" stub) so reply mode could be smoke-tested end-to-end before review-mode landed on top. Commit 8 then replaced the stub with the actual review pipeline.

### 2026-04-30: Drop "hard cap at 50" comment-fetch line

Spec said "top 20 by score, any depth (flatten the tree). Hard cap at 50 to bound prompt size." Once we sort all comments by score and take top 20, the 50 cap adds nothing. Implementation walks the full tree, sorts, takes top 20.

### 2026-04-30: `golang.org/x/net/html` dep added for review inspection

Review-mode inspection (`reply/inspect.go`) needs to extract title, meta tags, headings, and visible-text snippets from arbitrary HTML. Regex-based extraction would be brittle on real-world pages; `golang.org/x/net/html` is stdlib-adjacent (Google maintained, used by `goquery`, used by Go itself for tooling). One transitive dep: `golang.org/x/net`. Acceptable — the project's "no SDKs / minimal deps" stance was about provider SDKs, not `x/*` packages.

### 2026-04-30: Render header reverted to plain `## Best Pick`

A pre-audit version had `## Best Pick — Best` (with the picked variant's label suffix). Audit caught this as drift from the spec line 312 example. Reverted to `## Best Pick` to match.

### 2026-04-30: `draft-input.json` shape varies by mode

Reply mode writes `reply.DraftInput`. Review mode writes `reply.ReviewDraftInput` (which adds `Notes`). Both serialize fine as JSON; the file is a debug artifact rather than a stable contract, so the variance is acceptable. The `mode` field in `mode.json` tells callers which shape to expect.

### 2026-04-30: Firecrawl-backed inspection (Patch 3, partial)

Original spec deferred all of Patch 3 (image/screenshot inspection) to a follow-up branch, citing the headless-browser dep cost. After review, we picked a service-based path instead — Firecrawl (firecrawl.dev) provides markdown extraction + full-page screenshot + image URL list via a single REST call, no Chrome dep needed. Single-binary tider preserved.

Backend dispatch in `reply.Inspect`:
- `FIRECRAWL_API_KEY` in env → `InspectFirecrawl` (POST `/v1/scrape`, formats: markdown + screenshot@fullPage + links). Inspection.Source = "firecrawl"; populates `Markdown`, `ScreenshotURL`, `ImageURLs` alongside the structural fields.
- otherwise → `InspectHTML` (the existing stdlib + x/net/html backend, text-only). Inspection.Source = "html".

CLI behavior in review mode: after `Inspect` returns, if `ScreenshotURL` is non-empty, the screenshot is downloaded to `<session>/screenshots/screenshot-<timestamp>.png` so it persists after Firecrawl's hosted URL expires. Download failure is non-fatal — the URL is still in `inspection.json`.

What's NOT shipped yet (deliberate scope split):
- Vision-LLM consumption of the screenshot/images. The current notes-builder LLM call is text-only — it sees `Markdown` + `Headings` + `Snippets` (Firecrawl's output is materially cleaner than HTML extraction so review notes already improve), but it doesn't actually look at the screenshot. Adding vision requires extending `internal/llm/Message` to support image attachments, which touches both Anthropic and OpenAI provider implementations. That's a separate commit.

So today's contribution is half of the visual story: rich text extraction + persisted visual artifacts. The visual-LLM analysis on top is a follow-up. Even without it, Firecrawl's markdown is strictly better than HTML scraping for downstream prompts, so this still earns its keep when the API key is present.

### 2026-04-30: Inspection types extended

Added to `types.Inspection`: `Source` ("html"|"firecrawl"), `Markdown`, `ScreenshotURL`, `ScreenshotPath`, `ImageURLs`. All optional/zero-friendly — text-only `InspectHTML` doesn't populate them, so `inspection.json` from an HTML run looks unchanged from before.
