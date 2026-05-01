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

Most of that advice is plausible, but the result reads like a generic marketing consultant checklist. It repeats what the thread already contains, especially "pick one platform" and "repurpose content." It also produced a `detailed` variant that was longer rather than more differentiated.

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

Replace the default `detailed` variant with angle-based variants.

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
best
short
thread-aware
personal-story, if supported
question-first, if needed
detailed, only if the OP explicitly asks for a plan, diagnosis, technical steps, or multi-part answer
```

The model does not have to output every variant. Three strong variants are better than four padded ones.

## Variant definitions

### `best`

The recommended reply. It should be the comment the user is most likely to post.

Properties:

- concise
- specific
- directly useful to the OP
- grounded in OP + top comments + supplied context
- usually 120-220 words for business/community subs
- can be shorter if the answer is obvious

For the Shopify burnout example, `best` should focus on one sharp frame:

> Build a repeatable proof-capture loop, not a bigger content calendar.

### `short`

The shortest viable reply.

Properties:

- one paragraph or a few bullets
- 40-90 words in most non-technical threads
- no setup, no throat-clearing
- preserves the core advice from `best`

### `thread-aware`

Reacts to something important already present in comments.

This is the replacement for most `detailed` output in Reddit-style business/trade/community subs.

Purpose:

- add a missing nuance
- resolve tension between top comments
- push back on advice that is directionally right but incomplete
- avoid repeating the same consensus comment

For the Shopify burnout example, the useful counter-comment was that batching can fail because it still requires creative energy on demand. A good `thread-aware` variant should engage that:

```text
I agree with the "pick one platform" advice, but I would be careful with generic batching. Batching only works if the task is capture, not creativity.

Instead of sitting down to invent content, keep a repeatable proof-shot list: product in hand for scale, close-up texture, quick use/demo, packing an order, and one process clip. Film those whenever you already have the product out. Then on busy weeks you are assembling from a small clip library, not trying to be creative from zero.

That is the difference between a content calendar and content inventory.
```

### `personal-story`

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

### `question-first`

Use only when more information is required before useful advice is possible.

Good uses:

- OP asks a broad question but omits product/category/audience/platform performance
- OP asks for diagnosis but gives no data
- the safest reply is to ask for one missing fact

Bad uses:

- OP is venting and already has enough context for a useful lightweight answer
- the question is a disguised invitation to write a mini-consulting plan

### `detailed`

Not a default variant.

Use only when the OP explicitly asks for:

- step-by-step plan
- technical troubleshooting
- implementation details
- multi-part critique
- code/config/database/API diagnosis
- structured store review

If produced, `detailed` must still be concise for the subreddit. It should not be a 10-step playbook unless the OP asked for one.

## Subreddit-aware behavior

The prompt should infer thread/subreddit style and choose variants accordingly.

### Signals available to the drafter

The drafter prompt should classify thread style from three signals on the OP, all of which are already fetched and stored in `thread.json`:

- **Subreddit name** — already passed through to the drafter prompt as `Subreddit: r/{{.Subreddit}}`.
- **Flair** — currently fetched and stored in `thread.json` (e.g. `"flair": "Marketing"`) and passed to the *mode classifier*, but **not threaded into the drafter prompt today**. This needs fixing as part of this spec: the drafter often has to disambiguate similar subs (`r/Entrepreneur` with flair `Technical Question` vs `Marketing` vs `Personal`) and flair is the fastest signal for that.
- **OP body** — already in the prompt.

Flair must be added to the drafter template inputs so the model can use it for both category inference and tone calibration. When flair is empty or absent, the drafter falls back to subreddit + body.

### Business, trade, marketing, ecommerce, founder, community subs

Examples:

- `r/shopify`
- `r/EtsySellers`
- `r/ecommerce`
- `r/Entrepreneur`
- `r/marketing`

Default behavior:

- produce `best`
- produce `short`
- prefer `thread-aware`
- produce `personal-story` only if supported and relevant
- skip `detailed` unless OP explicitly asks for a plan

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

- `detailed` may be useful
- answer with concrete steps, tradeoffs, commands, configs, or diagnostics
- `thread-aware` is still useful when top comments reveal a misconception
- avoid personal-story unless credibility matters and is concise

### Review/feedback posts

Handled by review mode, not normal reply mode.

Default behavior:

- inspect the linked site/shop/listing
- produce structured review observations
- no generic reply if inspection fails
- do not trigger review mode from comments

This spec does not change review-mode implementation.

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

Example:

- config says Streambed
- command passes `--context=kova`

The reply must not mention Streambed unless the Reddit thread is about Streambed-like work.

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

The reply prompt should add or strengthen these rules.

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

### Word-count guidance

Default caps for non-technical reply mode:

- `best`: 120-220 words
- `short`: 40-90 words
- `thread-aware`: 90-180 words
- `personal-story`: 90-180 words
- `question-first`: 30-80 words
- `detailed`: skip unless needed; if included, 250-400 words max unless OP explicitly asked for more

Technical/troubleshooting threads may exceed these caps when concrete detail is necessary.

## Output rendering

Current renderer sections:

```text
Best Pick
Alternatives
```

This can stay, but labels should become meaningful:

- `Short`
- `Thread-Aware`
- `Personal Story`
- `Question First`
- `Detailed`

`Detailed` should not appear unless generated.

Alternative reasoning should describe the angle, not the length:

Good:

```text
Engages the batching pushback in the comments and reframes the work as capture inventory.
```

Bad:

```text
Provides a step-by-step operating model with concrete tools.
```

## Ideal output for the Shopify example

Given:

```text
tider reply --url=<shopify marketing burnout thread> --context=kova --context=personal
```

The recommended `best` reply should resemble:

```text
I would make the content system smaller than "be active on every platform."

For a solo store, the hard part is not just scheduling posts. It is having repeatable raw material that proves the product is real and worth buying. I would pick one source channel, then build a simple shot list you can reuse every time: product in hand for scale, texture close-up, quick use/demo, packing an order, and one process/maker clip.

That gives you enough footage to repurpose into reels, pins, product-page clips, and posts without inventing new ideas every day.

Also, do not over-polish it. If the product is clear, garage/process footage can work because buyers are often looking for trust: is this real, does it look like the photos, who is behind it?

So I would optimize for a repeatable proof-capture loop, not a bigger content calendar.
```

The `thread-aware` variant should resemble:

```text
I agree with the "pick one platform" advice, but I would be careful with generic batching. Batching only works if the task is capture, not creativity.

Instead of sitting down to invent content, keep a repeatable proof-shot list: product in hand for scale, close-up texture, quick use/demo, packing an order, and one process clip. Film those whenever you already have the product out. Then on busy weeks you are assembling from a small clip library, not trying to be creative from zero.

That is the difference between a content calendar and content inventory.
```

The `personal-story` variant, if produced, should resemble:

```text
I have seen this up close with a one-person handmade shop: the content burden gets heavy fast, but the useful clips usually were not polished. They were simple proof that the product was real: product in hand for scale, texture, packing, and process.

I would not try to run four separate content calendars. I would build one small capture loop: film the same five proof shots whenever the product is already out, then reuse those clips across whichever channel is actually bringing buyers.

That keeps the work closer to running the shop instead of becoming a full-time creator.
```

## Implementation plan

### 1. Update reply prompt

Files:

```text
prompts/reply.tmpl
reply/drafter.go
```

Changes to `prompts/reply.tmpl`:

- replace the variant list with the new angle-based model
- make `detailed` conditional
- add subreddit-aware guidance
- reference `{{.Flair}}` alongside `{{.Subreddit}}` in the thread header (rendered conditionally so empty flair doesn't print "Flair: ")
- add thread-aware instructions based on top comments
- add personal-story rules
- add stronger first-person claim ban
- add word-count guidance
- clarify `author_context` vs context-bank roles

Changes to `reply/drafter.go`:

- add `Flair string` to the anonymous struct passed to `replyTmpl.Execute` in `renderReplyPrompt`
- populate it from `input.Thread.Flair` (the field already exists on `types.Thread`)

This is the gap noted in *Signals available to the drafter*: the data is already on the type, just not threaded through to the drafter template.

### 2. Update tests

Files:

```text
reply/drafter_test.go
reply/render_test.go
```

Changes:

- update happy-path fake response to include `thread-aware`
- add prompt-render test assertions for:
  - `thread-aware`
  - `personal-story`
  - `detailed` conditionality
  - unsupported first-person claim ban
  - context-as-lens guidance
  - `Flair: <value>` line appears in the rendered prompt when `Thread.Flair` is non-empty, and is omitted otherwise
- ensure renderer title-cases labels like `thread-aware` and `personal-story`

### 3. Update type comments

File:

```text
internal/types/types.go
```

Change the `ReplyDraft.Label` comment from:

```go
// "best" | "short" | "detailed" | "question-first"
```

to:

```go
// Common labels: "best", "short", "thread-aware", "personal-story",
// "question-first", "detailed".
```

The data model does not need a schema change because IDs and labels are already strings.

### 4. Manual verification

Run:

```text
go test ./...
```

Then re-run a known thread:

```text
./tider reply --url=https://www.reddit.com/r/shopify/comments/1sz012f/struggling_on_marketing_my_shopify_store/ --context=kova --context=personal
```

Expected:

- no generic 10-step `Detailed` default
- `Best Pick` is concise and Kova-shaped
- `Thread-Aware` engages the batching/creative-energy counterpoint
- `Personal Story` appears only if the model can use the one-person handmade shop context naturally
- no "I ran into the same wall" unless directly supported
- no mention of Kova by name
- no mention of Streambed

## Acceptance criteria

1. `tider reply` still returns valid JSON internally and markdown externally.
2. Existing session artifacts continue to be written:
   - `thread.json`
   - `contexts.json`
   - `mode.json`
   - `draft-input.json`
   - `drafts.json`
   - `output.md`
3. The default output for non-technical business/community threads no longer includes a long `Detailed` variant unless OP asked for a plan.
4. At least one generated variant explicitly engages a high-signal comment or repeated thread pattern when such a pattern exists.
5. Personal anecdotes are optional and grounded.
6. `author_context` is treated as voice/background, not a competing project context.
7. Context-bank project material influences advice without naming or pitching the project by default.
8. The Shopify marketing-burnout example produces a sharper reply centered on repeatable product-proof capture.

## Future work

- Add a `--style=concise|balanced|expanded` flag only if manual usage shows the default is too restrictive.
- Add session-based regeneration for a specific angle:

  ```text
  tider reply regen --session=<path> --angle=thread-aware
  ```

- Add lightweight subreddit profiles if prompt-only subreddit awareness is insufficient.
- Add context metadata later if prose conventions become unreliable:

  ```yaml
  allow_naming: false
  allow_personal_story: true
  primary_lens: product trust
  ```

  Do not add this until there is clear evidence prose conventions are failing.
