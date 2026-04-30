# Spec: flat command structure + `tider reply`

Living document. Updated as implementation progresses.

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

Flags:

- `--url` — required
- `--context` — repeatable; resolved via `contextbank.Load` (id from bank, or direct file path)
- `--render=json|markdown` — TTY-aware default (markdown in TTY, json when piped)
- `--providers` — config-backed default
- `--openai-model`, `--anthropic-model` — config-backed defaults
- `--max-tokens` — config-backed default
- `--refresh` — bypass any thread fetch cache (later, may not be needed for v1)

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

The classifier returns `target_urls` it noticed. We also parse the OP outbound URL + markdown links + raw URLs as a fallback signal. If the classifier returned a target, it wins; otherwise take the first non-image URL found.

Bare hostnames (`my shop is example.com`, no protocol) are not extracted — too ambiguous.

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
- Hard cap at 50 to bound prompt size
- Stored in `thread.json` as a flat list with `parent_id` preserved

---

## Data shapes

```go
package reply

import "time"

type Thread struct {
    URL         string
    Subreddit   string
    PostID      string
    Title       string
    Body        string
    Author      string
    Flair       string
    OutboundURL string
    Comments    []Comment
    FetchedAt   time.Time
}

type Comment struct {
    ID         string
    ParentID   string
    Author     string
    Body       string
    Score      int
    CreatedUTC float64
}

type Mode string

const (
    ModeReply  Mode = "reply"
    ModeReview Mode = "review"
)

type ModeResult struct {
    Mode       Mode
    Reason     string
    TargetURLs []string
}

type LoadedContext struct {
    ID     string  // empty if loaded by path
    Source string  // "bank" | "path"
    Path   string
    Body   string  // verbatim file contents
}

type Draft struct {
    ID        string
    Label     string  // "best" | "short" | "detailed" | "question-first"
    Text      string
    Reasoning string
}

type Bundle struct {
    ThreadURL   string
    Subreddit   string
    Mode        Mode
    Drafts      []Draft
    PickID      string
    Generated   time.Time
}
```

`thread.json` ↔ `Thread`. `mode.json` ↔ `ModeResult`. `drafts.json` ↔ `Bundle`. `contexts.json` ↔ `[]LoadedContext`. `target.json`, `inspection.json`, `review-notes.json` are review-mode-only.

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

## Patches / commit plan

Implementation lands in this order on `feat-reply-and-rename`:

**Commit 1: spec**
- `SPEC_REPLY.md` (this file).

**Commit 2: rename `tider draft` → `tider post`**
- Move `cmd/tider/draft.go` → `cmd/tider/post.go`.
- Rename `draftCmd` → `postCmd`, all `draft*` flag vars → `post*`.
- Update `cmd/tider/main.go` registration.
- Update `regen` cross-references and any other internal usage.
- Update `Claude.md`, `README.md`, help text.
- No backward-compatible alias — `tider draft` returns cobra's "unknown command" error.

**Commit 3: Reddit thread fetcher + URL parser**
- `internal/reddit/url.go` — parse all five supported URL forms; expose `ParseThreadURL(raw) (sub, postID string, err error)`.
- `internal/reddit/thread.go` — `FetchThread(ctx, sub, postID, opts) (*Thread, error)`. Hits `/r/<sub>/comments/<post_id>.json`. Selects top 20 comments by score across the tree, capped at 50.
- Tests with httptest fixtures.

**Commit 4: mode detector**
- `prompts/reply_mode.tmpl`.
- `reply/mode.go` — `Detect(ctx, llm, thread) (*ModeResult, error)`. JSON-mode call, parses classifier output, also runs a fallback URL-extraction pass against OP body.
- Tests with fake LLM provider.

**Commit 5: session manager**
- `reply/session.go` — `New(root, thread) (*Session, error)`, `Path() string`, `WriteJSON(name string, v any) error`, `WriteMarkdown(name, body string) error`.
- Slug from date + subreddit + post id.
- Tests for path generation, atomic write, missing-dir handling.

**Commit 6: context loading + snapshot**
- `reply/contexts.go` — wraps `contextbank.Load` for repeatable `--context` flags. Returns `[]LoadedContext`. Snapshots into session via `WriteJSON("contexts.json", ...)`.
- Tests for id+path mixing, missing entries, session snapshot.

**Commit 7: reply drafter (reply mode end-to-end)**
- `prompts/reply.tmpl`.
- `reply/drafter.go` — `Generate(ctx, llm, input) (*Bundle, error)`. Single LLM call, returns Bundle with 3-4 labeled variants.
- `reply/render.go` — markdown render of Bundle.
- Tests with fake provider.

**Commit 8: review-mode inspection + drafter**
- `reply/inspect.go` — `Inspect(ctx, http, target) (*Inspection, error)`. Fetches HTML, extracts title, meta description, headings (h1/h2/h3), visible text snippets via `golang.org/x/net/html` or simple regex. Saves `inspection.json`.
- `prompts/review_notes.tmpl` — turns inspection into structured review notes.
- `reply/notes.go` — `BuildNotes(ctx, llm, inspection) (*ReviewNotes, error)`.
- `prompts/review.tmpl` — review-mode draft prompt grounded in notes.
- Tests with httptest target sites + fake LLM.

**Commit 9: CLI wiring**
- `cmd/tider/reply.go` — full command. Loads config, parses URL, prints session path early, runs detector, persists artifacts incrementally, branches on mode.
- Register in `cmd/tider/main.go`.
- Help text.

**Commit 10: spec close-out**
- Update `SPEC_REPLY.md` with any decisions changed during implementation. Mark "implemented" sections.

Estimated total: ~1500 LOC implementation + ~600 LOC tests. Day of focused work.

If this turns out to need to ship as two PRs (rename separately, reply separately), split at commit 2 / 3 boundary. Default plan is one PR for both.

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

## Decisions log (filled as work progresses)

(Empty until implementation begins.)
