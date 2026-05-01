# Spec: `tider reply` quality refinement

## Status

Draft spec. This refines the already implemented `tider reply` command without changing the core read-only invariant: Tider drafts replies, persists session artifacts, and never posts to Reddit.

## Problem

The current reply output is structurally useful but not sharp enough for real Reddit engagement.

Observed example: a Shopify thread where the OP is a solo store owner burned out by Instagram, TikTok, Facebook, Pinterest, captions, comments, trends, and filming content. With `--context=kova --context=personal`, Tider produced a long operating plan:

- pick one source channel
- batch content
- use Meta Business Suite
- reuse clips across TikTok/Pinterest
- use caption templates
- respond for 15 minutes/day
- run small paid tests
- track funnel metrics
- hire a part-time editor

Most of that advice is plausible, but the result reads like a generic marketing consultant checklist. It repeats what the thread already contains, especially "pick one platform" and "repurpose content." It also produced too many bullets and a `detailed` variant that was longer rather than more differentiated.

The strongest context-specific insight from Kova was weaker than it should have been:

> Do not solve content burnout by becoming a full-time creator. Build a repeatable way to capture real product proof: scale, texture, function, packing, and process footage.

That is the kind of angle Tider should surface.

## Goals

1. Make `tider reply` produce comments a Reddit user would realistically post.
2. Prefer differentiated angles over longer versions of the same advice.
3. Use project context as a lens, not as a pitch.
4. Use personal context only when it makes the reply more credible or human.
5. Engage the existing thread when comments reveal a useful counterpoint.
6. Keep default replies concise unless the OP explicitly asks for a step-by-step plan.
7. Prevent unsupported first-person claims such as "I ran into the same wall."

## Non-goals

- No Reddit write actions.
- No automatic posting.
- No project pitching by default.
- No forcing personal anecdotes into every reply.
- No hardcoded one-off behavior for only `r/shopify`.
- No removal of session persistence.
- No change to review-mode inspection in this spec.
- No solving context-bank editing UX here, except where context semantics affect reply quality.

## Current command shape

`tider reply` remains the command:

```text
tider reply --url=<reddit-thread-url> --context=kova --context=personal
```

The current trailing positional `comment` seen in usage examples is not part of the preferred UX. The refined vocabulary is:

- `tider post` for drafting a standalone Reddit submission
- `tider reply` for drafting a reply inside an existing Reddit thread

No `draft comment` phrase should appear in user-facing product copy.

## Core design shift

Replace the default `detailed` variant with a compact postable format:

```text
Best Pick

<ready-to-post comment>

Alternative Picks

Shorter

<shorter ready-to-post comment>

Counterpoint

<contrarian/minority/thread-aware angle, if useful>

Warmer / Personal

<personal-context version, only if relevant>

Question

<question-first version, only if needed>
```

No reasoning or editing notes should render by default. The output should feel like a set of comments the user can quickly choose from and edit, not a report about the comments. Reasoning is still recorded in `drafts.json` for audit/debug.

### Old variant model

```text
best
short
detailed
question-first
```

The problem: `detailed` is a length variant. It often creates a longer checklist without adding a new reason to post.

### New variant model

```text
best               -> rendered as "Best Pick"
shorter            -> rendered as "Shorter"
counterpoint       -> rendered as "Counterpoint"
warmer-personal    -> rendered as "Warmer / Personal", only when supported
question           -> rendered as "Question", only when needed
```

The model does not have to output every variant. Two to four strong drafts are better than a full menu. `Detailed` is removed entirely — there is no `detailed` slot and no `Technical Steps` slot in v1. Technical/troubleshooting threads can make `best` longer when concrete detail is necessary.

## Variant definitions

### `best` / `Best Pick`

The recommended reply. It should be the comment the user is most likely to post.

Properties:

- concise
- specific
- directly useful to the OP
- grounded in OP + top comments + supplied context
- usually 120-180 words for business/community subs
- can be shorter if the answer is obvious
- defaults to short paragraphs, not bullets
- can be a counterpoint-style reply if that is the strongest version

For the Shopify burnout example, `best` should focus on one sharp frame:

> Build a repeatable proof-capture loop, not a bigger content calendar.

### `shorter` / `Shorter`

The shortest viable reply.

Properties:

- usually one paragraph
- 40-90 words in most non-technical threads
- no setup, no throat-clearing
- preserves the core advice from `best`
- no bullet list unless one or two bullets materially improve readability

### `counterpoint` / `Counterpoint`

Offers a contrarian, minority, or thread-aware angle when useful.

This is the replacement for most `detailed` output in Reddit-style business/trade/community subs.

Purpose:

- add a missing nuance
- resolve tension between top comments
- push back on advice that is directionally right but incomplete
- represent a useful minority POV
- avoid repeating the same consensus comment

**Distinct-frame rule.** If `best` is itself counterpoint-flavored, `counterpoint` MUST take a different contrarian/minority angle. If no different angle exists, skip the slot — do not duplicate the `best` frame.

For the Shopify burnout example, the useful counter-comment was that batching can fail because it still requires creative energy on demand. A good `counterpoint` variant should engage that:

```text
I would be careful with the idea that the answer is just "be more consistent."

Consistency matters, but if the system depends on you constantly inventing content, it will break every time the shop gets busy. I would optimize for reusable proof instead: film simple clips while the work is already happening — scale, texture, demo, packing, process — and build a small library you can pull from.
```

### `warmer-personal` / `Warmer / Personal`

Uses a personal anecdote from `author_context` or supplied contexts only when it clearly fits.

Purpose:

- make the reply feel more human
- establish why the user understands the pain
- avoid sounding like a generic consultant

Rules:

- Do not invent lived experience.
- Do not use family details unless the supplied context contains them.
- Do not mention private family context unless it is genuinely relevant and the user explicitly supplied that context.
- Prefer light phrasing over oversharing.

For the Kova/personal context, the mom story is available:

> Kova was inspired by watching the user's mom struggle with a one-person Etsy/Shopify handmade shop.

The reply should not default to "my mom..." every time. Better default phrasing:

```text
I have seen this up close with a one-person handmade shop: the useful content usually was not polished. It was simple proof that the product was real: scale, texture, packing, and process.
```

Use "my mom" only if the context explicitly allows personal/family framing or the user requests a personal version.

### `question` / `Question`

Use only when more information is required before useful advice is possible.

Good uses:

- OP asks a broad question but omits product/category/audience/platform performance
- OP asks for diagnosis but gives no data
- the safest reply is to ask for one missing fact

Bad uses:

- OP is venting and already has enough context for a useful lightweight answer
- the question is a disguised invitation to write a mini-consulting plan

### No `detailed` by default

Do not generate a `detailed` slot. The variant is removed from the prompt's variant list entirely.

For technical/troubleshooting threads, allow more detail inside `Best Pick` when needed. If a separate technical variant is needed in the future, a specific label such as `Technical Steps` can be added then — not a generic `Detailed`.

## Subreddit-aware behavior

The prompt should infer thread/subreddit style and choose variants accordingly.

### Signals available to the drafter

The drafter prompt should classify thread style from three signals on the OP, all already fetched and stored in `thread.json`:

- **Subreddit name** — passed through to the drafter prompt as `Subreddit: r/{{.Subreddit}}`.
- **Flair** — already threaded through to the drafter (PR #19) and rendered as `Flair: {{.Flair}}` when non-empty.
- **OP body** — already in the prompt.

### Business, trade, marketing, ecommerce, founder, community subs

Examples:

- `r/shopify`
- `r/EtsySellers`
- `r/ecommerce`
- `r/Entrepreneur`
- `r/marketing`

Default behavior:

- produce `best`
- produce `shorter`
- prefer `counterpoint` when top comments reveal a repeated pattern, missing nuance, or useful disagreement
- produce `warmer-personal` only if supported and relevant
- skip generic `detailed`

Target tone:

- practical peer advice
- one useful frame
- fewer tools and metrics unless directly asked
- avoid consultant voice

### Technical/troubleshooting subs

Examples:

- `r/golang`
- `r/PostgreSQL`
- `r/kubernetes`
- `r/webdev`
- `r/sysadmin`

Default behavior:

- answer with concrete steps, tradeoffs, commands, configs, or diagnostics
- allow `Best Pick` to be longer when concrete detail is necessary
- `counterpoint` is still useful when top comments reveal a misconception
- avoid `warmer-personal` unless credibility matters and is concise

### Review/feedback posts

Handled by review mode, not normal reply mode.

Default behavior:

- inspect the linked site/shop/listing
- produce structured review observations
- no generic reply if inspection fails
- do not trigger review mode from comments

This spec does not change review-mode inspection. **It does** align review-mode variant labels for consistency: `short` → `shorter`, `question-first` → `question`. Review mode keeps its review-specific slots:

```text
best
shorter
structured-review
question
```

No `counterpoint` or `warmer-personal` for review mode by default.

## Context semantics

### `author_context`

`author_context` in `~/.tider/config.yaml` should be stable voice/background, not active project material.

Recommended shape:

```yaml
author_context: |
  Vignesh writes concise, direct, practical replies. He is a founder and engineering leader.
  Use this only for tone and judgment. Do not mention projects, credentials, employers,
  family stories, or personal background unless directly relevant to the thread and supplied
  context explicitly allows it.
```

`author_context` should not say "currently building Streambed" if `--context=kova` might be passed in the same command. Active project context belongs in the context bank.

### Context bank files

Context bank files are reusable project/background material:

```text
~/.tider/contexts/kova.md
~/.tider/contexts/streambed.md
~/.tider/contexts/personal.md
```

Use:

- `--context=kova` when the reply should use Kova as a lens
- `--context=streambed` for database/CDC/data infra threads
- `--context=personal` only when personal background is relevant

### Collision rule

If `author_context` and `--context` disagree about the user's current project, `--context` wins for subject matter and `author_context` remains voice-only.

### Mom story rule

The mom story is valuable, but should be optional.

Recommended split:

- `kova.md`: product thesis and audience
- `personal.md`: broad personal/professional background
- optional `kova-origin.md`: personal origin story, including the mom/shop anecdote

If the mom story remains in `personal.md`, the prompt must treat it as origin/background, not default reply content.

Use it when:

- OP is a solo handmade seller
- OP is struggling with one-person shop operations
- authenticity or trust is central
- the user wants a warmer/personal reply

Avoid it when:

- the thread is technical
- the thread is not about handmade/small-store pain
- it would make the reply about the user instead of OP
- the reply already works without personal framing

## Prompt rules

### Use context as a lens

Context should shape the insight, not become the topic.

For Kova:

- good: "buyers need clear proof that the product is real"
- good: "capture product-in-hand, texture, function, process"
- bad: "I am building Kova"
- bad: "Kova can solve this"
- bad: turning every Shopify thread into Etsy listing video advice

### Prefer one differentiated point

A good Reddit reply often wins by saying one useful thing clearly.

Avoid:

- full operating manuals by default
- generic tool lists
- repeating every good comment in the thread
- adding ads/SEO/funnel metrics unless the OP asked

### Respect top comments

The drafter should inspect top comments as context and ask:

1. What advice has already been repeated?
2. What counterpoint or nuance is missing?
3. Can we build on the best comment instead of duplicating it?

For the Shopify example:

- repeated: pick one platform, repurpose content
- useful counterpoint: batching still requires creative energy
- sharper reply: capture inventory, not content calendar

### Counterpoint slot rule

If top comments contain a repeated pattern, missing nuance, or contrarian POV worth surfacing, generate `counterpoint` to engage it; otherwise skip `counterpoint`.

If `best` is itself counterpoint-flavored, `counterpoint` MUST take a different contrarian/minority angle. No duplicate frame.

### Ban unsupported first-person claims

Never write:

```text
I ran into the same wall.
What fixed it for me was...
When I ran my store...
```

unless the supplied context explicitly supports that exact experience.

Allowed:

```text
I have seen this up close with a one-person handmade shop...
```

only if the context includes that story and the thread fits.

### Bullet rule

Default to short paragraphs, not bullets. Use bullets only when they materially improve readability — typically when listing 3+ short, parallel items the reader would scan rather than read.

Single-bullet "lists" are anti-patterns. Bulleted operating-manual-style replies are anti-patterns.

### Word-count guidance

Default caps for non-technical reply mode:

- `best`: 120-180 words
- `shorter`: 40-90 words
- `counterpoint`: 80-160 words
- `warmer-personal`: 70-140 words
- `question`: 30-80 words

Technical/troubleshooting threads may exceed these caps when concrete detail is necessary.

## Output rendering

Target rendered markdown:

```text
# Reply drafts for r/<subreddit>

Thread: <thread title>
Mode: <reply|review>
Session: <session path>

## Best Pick

<ready-to-post comment>

## Alternative Picks

### Shorter

<shorter ready-to-post comment>

### Counterpoint

<contrarian/minority/thread-aware angle, if useful>

### Warmer / Personal

<personal-context version, only if relevant>

### Question

<question-first version, only if needed>
```

Rendering rules:

- No `Why this works`.
- No reasoning under variants in markdown. Reasoning is preserved in `drafts.json` for audit only.
- No `Editing Notes`.
- No `Detailed` by default.
- Use `## Alternative Picks`, not `## Alternatives`.
- Render only variants that exist.
- Keep default output to 2-4 drafts total.
- Use bullets only when they make the comment easier to read; default to short paragraphs.

## Ideal output for the Shopify example

Given:

```text
tider reply --url=<shopify marketing burnout thread> --context=kova --context=personal
```

The recommended `best` reply should resemble:

```text
I agree with the "pick one platform" advice, but I would separate capture from creativity.

Batching content can still burn you out if it means sitting down and inventing ideas on demand. What works better is building a small footage library while you are already doing normal shop work: product in hand for scale, texture close-up, quick demo, packing an order, process shot, maybe a common question answered with text overlay.

Then on busy weeks, you are assembling from real clips instead of starting from zero.

Also, do not over-worry about the garage look. If the product is clear and the footage feels real, that can build more trust than a polished studio-style post.
```

The `shorter` variant should resemble:

```text
Do not try to become a full-time creator. Build a small footage library while doing normal shop work: scale, texture, demo, packing, process. Then reuse those clips on one main channel and repost elsewhere. The goal is to stop inventing content from zero every week.
```

The `counterpoint` variant should resemble:

```text
I would be careful with the idea that the answer is just "be more consistent."

Consistency matters, but if the system depends on you constantly inventing content, it will break every time the shop gets busy. I would optimize for reusable proof instead: film simple clips while the work is already happening — scale, texture, demo, packing, process — and build a small library you can pull from.
```

The `warmer-personal` variant, if produced, should resemble:

```text
I have seen this up close with a one-person handmade shop. The useful content usually was not polished; it was simple proof that the product was real: scale, texture, packing, process. I would build around that instead of trying to run four separate content calendars.
```

## Implementation plan

### 1. Update reply prompt

Files:

```text
prompts/reply.tmpl
```

Changes:

- replace the variant list with the new angle-based model (best, shorter, counterpoint, warmer-personal, question)
- remove the `detailed` slot entirely from the JSON output schema
- add the bullet-soft-rule
- add the counterpoint distinct-frame rule
- update word-count guidance
- preserve existing rules: first-person ban, context-as-lens, voice-only author_context, anti-tells
- update the JSON output example with new variant IDs

### 2. Update review prompt for label alignment

File:

```text
prompts/review.tmpl
```

Rename `short` → `shorter` and `question-first` → `question` so review-mode and reply-mode share the same labels for the analogous slots. Review mode keeps `structured-review` as its review-specific slot.

### 3. Update renderer

File:

```text
reply/render.go
```

- displayLabel map: add new IDs (shorter, counterpoint, warmer-personal, question) with their spec-mandated display forms
- Section header: `## Alternatives` → `## Alternative Picks`
- Drop the `> reasoning` blockquote on best-pick rendering
- Drop the `*reasoning*` italic on alternative rendering
- Keep markdown heading hierarchy (`# `, `## `, `### `)
- Old IDs (short, thread-aware, personal-story, question-first, detailed) fall through to the kebab-fallback path; no special backward-compat handling

### 4. Update type comments

File:

```text
internal/types/types.go
```

Update the `ReplyDraft.Label` comment to reflect the new common label set.

### 5. Update tests

Files:

```text
reply/drafter_test.go
reply/render_test.go
reply/review_test.go
```

- drafter_test: update happy-path response to use new IDs (best, shorter, counterpoint)
- drafter_test: prompt-render assertions for counterpoint / warmer-personal slot language, no-detailed, distinct-frame rule
- render_test: sample bundle uses new IDs, assert `## Alternative Picks`, assert `### Shorter / Counterpoint / Warmer / Personal / Question`, assert reasoning is NOT present in markdown output
- review_test: update happy-path response and any prompt assertions for the renamed `shorter` / `question` review variants

### 6. Manual verification

```text
go test ./...
```

Then re-run the Shopify burnout thread:

```text
./tider reply --url=https://www.reddit.com/r/shopify/comments/1sz012f/struggling_on_marketing_my_shopify_store/ --context=kova --context=personal
```

Expected:

- output sections are `## Best Pick` and `## Alternative Picks`
- no `> reasoning` blockquote, no `*reasoning*` italic
- no `Why this works` / `Editing Notes`
- no `Detailed` slot
- `Best Pick` is concise (<=180 words), Kova-shaped, postable
- `Counterpoint` engages the batching/creative-energy counterpoint when present
- `Warmer / Personal` appears only if the model can use the one-person handmade shop context naturally
- no "I ran into the same wall" unless directly supported
- no mention of Kova by name
- bullet usage limited to genuinely list-shaped material

## Acceptance criteria

1. `tider reply` still returns valid JSON internally and markdown externally.
2. Existing session artifacts continue to be written: `thread.json`, `contexts.json`, `mode.json`, `draft-input.json`, `drafts.json`, `output.md`.
3. The rendered markdown uses `## Best Pick` and `## Alternative Picks`.
4. The rendered markdown does not include `> reasoning`, `*reasoning*`, `Why this works`, per-variant reasoning, or `Editing Notes`.
5. The default output for non-technical business/community threads no longer includes a `Detailed` variant.
6. Aside from `Shorter`, alternatives represent distinct POVs, not only longer/shorter rewrites.
7. If top comments contain a repeated pattern, missing nuance, or contrarian POV worth surfacing, `Counterpoint` engages it; otherwise `Counterpoint` is skipped.
8. Personal anecdotes are optional and grounded.
9. `author_context` is treated as voice/background, not a competing project context.
10. Context-bank project material influences advice without naming or pitching the project by default.
11. The Shopify marketing-burnout example produces a sharper reply centered on repeatable product-proof capture.

## Future work

- Add a `--style=concise|balanced|expanded` flag only if manual usage shows the default is too restrictive.
- Add session-based regeneration for a specific angle:

  ```text
  tider reply regen --session=<path> --angle=counterpoint
  ```

- Add lightweight subreddit profiles if prompt-only subreddit awareness is insufficient.
- Add a `Technical Steps` review-mode-style slot for technical subs only if the longer-best-pick path proves insufficient.
- Add context metadata later if prose conventions become unreliable:

  ```yaml
  allow_naming: false
  allow_personal_story: true
  primary_lens: product trust
  ```

  Do not add this until there is clear evidence prose conventions are failing.
