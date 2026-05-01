# Spec: Firecrawl visual store/shop review for `tider reply`

## Status

Draft spec. This defines how `tider reply` should handle Reddit threads where the OP asks for feedback, criticism, or review of a concrete ecommerce site, Shopify store, Etsy shop, product listing, or similar public page.

Core decision: if a thread is classified as review mode, Tider must perform visual inspection. A text-only scrape is not enough for a store/shop review.

## Problem

Current review mode can classify a website-review thread correctly and fetch the target page, but it may silently fall back to HTML-only inspection when `FIRECRAWL_API_KEY` is not set.

That produces plausible but incomplete feedback:

- title/meta review
- heading/copy review
- category/pricing/CTA observations based on text
- open questions about images or product gallery

The user-facing output still looks like a review, but it has not inspected:

- homepage visual hierarchy
- above-the-fold composition
- product photography
- trust signals visible in the design
- image quality and consistency
- whether product scale, texture, material, or function are clear
- whether the site feels real, handmade, generic, AI-ish, or dropshipped
- mobile layout risks

For ecommerce and store-review posts, visual evidence is often the highest-signal part of the review. A text-only review is a partial review and should not be presented as complete.

## Product position

Review mode has a stronger contract than normal reply mode:

```text
review = OP asks for feedback on a concrete site/shop/listing/resource.
review requires target inspection + visual analysis before drafting.
```

If Tider cannot visually inspect the target, it should fail clearly and preserve the session. It should not silently downgrade to text-only review.

## Goals

1. Use Firecrawl as the required capture provider for review-mode site/shop/listing inspection.
2. Capture and persist a full-page screenshot for every review-mode target.
3. Discover and optionally persist relevant product/page images.
4. Run a visual analysis step over the screenshot and selected images.
5. Merge visual findings with text findings before drafting.
6. Make review output honest about what was inspected.
7. Make failure modes explicit and recoverable.
8. Use Kova/project context as a lens for visual feedback without naming or pitching the project by default.

## Non-goals

- No Reddit posting.
- No `--text-only` flag for review mode.
- No silent fallback from Firecrawl to HTML-only review.
- No broad crawling of the whole store in v1.
- No checkout flow testing.
- No performance audit, Lighthouse score, SEO crawl, or accessibility audit in v1.
- No claim that the review covered pages or images Tider did not inspect.
- No image generation or image editing.
- No Kova pitch unless context explicitly allows naming.

## Trigger behavior

Mode detection remains OP-only.

Inputs:

- subreddit
- title
- flair
- OP body/selftext
- OP outbound URL
- URLs in OP body

Do not use comments to trigger review mode. A comment saying "can you review my shop too?" must not flip the thread into review mode.

If mode is `reply`, do not inspect external links.

If mode is `review`, visual inspection is required.

## CLI behavior

Command shape remains:

```text
tider reply --url=<reddit-thread-url> --context=personal
```

No additional flag should be required to analyze images. The user's intent comes from the Reddit post asking for review/feedback.

`--text-only` is intentionally not added in v1. There is no degraded text-only review mode — Firecrawl visual is required when mode is `review`.

`--mode=reply` IS in v1 as a recoverable escape hatch. Use it when:

- the classifier mistakes a discussion thread for a review request
- you don't want to spend the Firecrawl + vision budget on a thread that doesn't warrant it
- `FIRECRAWL_API_KEY` isn't set and you want to draft an ordinary reply anyway

```text
tider reply --url=<reddit-thread-url> --mode=reply
```

`--mode=reply` short-circuits the OP-only mode classifier and runs the normal reply pipeline. It does not produce a degraded review.

## Required environment

Review mode requires:

```text
FIRECRAWL_API_KEY
```

Review visual analysis also requires a vision-capable LLM provider/model configured for a new task:

```yaml
tasks:
  review_visual:
    provider: openai
    model: <vision-capable-model>
    max_tokens: 4096
```

The exact model should be configurable and should not be hardcoded into the review pipeline. If `tasks.review_visual` is missing, fall back to `tasks.reply` only if that configured model supports image inputs. Otherwise fail clearly.

## Pipeline

Review mode should run:

```text
[Reddit thread]
      |
      v
[OP-only mode detection]
      |
      v
[Target URL extraction]
      |
      v
[Firecrawl target capture]
      |
      v
[Persist screenshot + selected images]
      |
      v
[Text extraction notes]
      |
      v
[Visual analysis notes]
      |
      v
[Merged review notes]
      |
      v
[Reply drafting]
      |
      v
[Rendered output + session artifacts]
```

### Step 1: Target extraction

Target URL candidates:

1. OP outbound URL for link posts
2. markdown links in OP body
3. raw URLs in OP body
4. mode-classifier `target_urls`

Skip:

- Reddit URLs
- image-only URLs posted as examples unless the OP explicitly asks for image feedback
- bare hostnames without protocol in v1

If multiple candidates exist, choose the first candidate that looks like a shop/site/listing. Persist alternatives in `target.json`.

### Step 2: Firecrawl capture

Firecrawl is required for review mode.

If `FIRECRAWL_API_KEY` is missing:

```text
Error: review mode requires visual site inspection, but FIRECRAWL_API_KEY is not set.
Set FIRECRAWL_API_KEY and rerun. Session preserved at <session path>.
```

Firecrawl should request, at minimum:

- markdown or equivalent clean text
- full-page screenshot
- links
- image references, if available
- enough metadata to preserve source URL and status

The current Firecrawl-backed `Inspection` shape already contains:

```go
Markdown
ScreenshotURL
ScreenshotPath
ImageURLs
Title
MetaDescription
Headings
Snippets
```

For this spec, review mode must require `ScreenshotURL` and a locally persisted `ScreenshotPath`.

If Firecrawl returns no screenshot:

```text
Error: review mode requires a screenshot, but Firecrawl did not return one.
Session preserved at <session path>.
```

If screenshot download fails:

```text
Error: review mode could not persist the Firecrawl screenshot.
Session preserved at <session path>.
```

Do not continue to draft a review without a screenshot.

### Step 3: Image selection

Screenshot is mandatory. Additional image URLs are optional but useful.

Select up to 8 page images for visual analysis.

Selection priority:

1. product images
2. hero images
3. collection/category images
4. process/maker images
5. trust imagery: packaging, certificates, team, workshop, store, materials

Filter out likely low-signal images:

- logos
- favicons
- SVG icons
- payment badges
- social icons
- tracking pixels
- tiny images
- repeated decorative backgrounds

Implementation can start with simple heuristics:

- reject URLs containing `logo`, `favicon`, `icon`, `sprite`, `payment`, `badge`, `tracking`, `pixel`
- reject non-image extensions unless Firecrawl explicitly identifies them as images
- cap by URL count
- dedupe by normalized URL

If no image URLs survive, proceed with screenshot-only analysis and record a limitation:

```text
No separate product image URLs were available; visual review is based on the captured page screenshot.
```

### Step 4: Persist visual inputs

Session layout should include:

```text
~/.tider/sessions/replies/{date}-{subreddit}-{post_id}/
  target.json
  inspection.json
  screenshots/
    homepage.png
  images/
    image-001.jpg
    image-002.png
  visual-input.json
  visual-notes.json
  review-notes.json
  draft-input.json
  drafts.json
  output.md
```

`images/` is optional in v1 if the vision provider can consume remote image URLs reliably. Prefer local persistence for reproducibility when practical.

`visual-input.json` should record exactly what was sent to the visual analyzer:

```json
{
  "target_url": "https://example.com/",
  "screenshot_path": ".../screenshots/homepage.png",
  "screenshot_source_url": "https://...",
  "image_refs": [
    {
      "url": "https://example.com/product.jpg",
      "local_path": ".../images/image-001.jpg",
      "alt": "optional if available",
      "reason": "product image candidate"
    }
  ],
  "page_title": "...",
  "context_ids": ["personal", "kova"],
  "generated": "..."
}
```

### Step 5: Visual analysis

Add a new visual analysis step before review-note drafting.

Suggested package/file:

```text
reply/visual.go
prompts/review_visual.tmpl
```

The visual analyzer should receive:

- full-page screenshot
- selected product/page images, if available
- target URL
- page title/meta/headings/snippets for grounding
- context-bank material, if supplied

It should return strict JSON.

Proposed type:

```go
type VisualReviewNotes struct {
    TargetURL    string              `json:"target_url"`
    ShopType     string              `json:"shop_type"`             // handmade|boutique|dropship|b2b_industrial|saas|services|portfolio|unclear
    Summary      string              `json:"summary"`
    Observations []VisualObservation `json:"observations"`
    KovaSignals  []string            `json:"kova_signals,omitempty"` // populated only when ShopType ∈ {handmade, boutique}
    Questions    []string            `json:"questions,omitempty"`
    Limitations  []string            `json:"limitations,omitempty"`
    Generated    time.Time           `json:"generated"`
}

type VisualObservation struct {
    Area           string `json:"area"`            // above_fold, product_images, trust, navigation, pricing_cta, mobile_risk
    Finding        string `json:"finding"`
    Evidence       string `json:"evidence"`
    Severity       string `json:"severity"`        // high, medium, low
    Recommendation string `json:"recommendation"`
}
```

The prompt should ask for observations in these areas:

- above-the-fold clarity
- product or offer clarity
- visual hierarchy
- CTA visibility
- product image quality
- product scale/material/texture/function visibility
- trust/authenticity signals
- pricing or quote-path visibility
- category navigation visibility
- mobile readability risks visible in the screenshot
- brand consistency
- generic/dropship/AI-looking risk when visually supported

It must not infer:

- checkout behavior
- inventory depth
- prices not visible
- conversion rates
- mobile behavior not visible from the screenshot
- internal page quality unless those pages were inspected

### Kova lens

When Kova context is present, use it as a lens for visual feedback:

- Does the page show real product proof?
- Are scale, texture, material, function, and process visible?
- Would a buyer trust that the product is real and accurately represented?
- Is a short product/process video an obvious missing trust signal?

Do not mention Kova by name unless context explicitly allows naming.

#### Shop-type classification gates `kova_signals`

Not every store-review thread is Kova-relevant. r/shopify spans dropshippers, B2B equipment suppliers, SaaS, services, and agencies — the Kova thesis (handmade trust) only applies to a subset.

The visual analyzer must classify the inspected page and only populate `kova_signals` when the classification is Kova-adjacent:

```
shop_type ∈ {
  handmade,           // ← kova_signals applies
  boutique,           // ← kova_signals applies  
  dropship,           // kova_signals empty
  b2b_industrial,     // kova_signals empty
  saas,               // kova_signals empty
  services,           // kova_signals empty
  portfolio,          // kova_signals empty
  unclear,            // kova_signals empty
}
```

The classification itself is recorded in `VisualReviewNotes.ShopType` so downstream rendering can show it.

Without this gate, recommending "show your maker process" to a B2B industrial supplier or SaaS landing page produces dubious output. Acceptance criterion 9 (PND Industrial Suppliers) is explicitly the regression target.

Good:

```text
Your product images should prove scale and material faster. A short real product/process clip would probably build more trust than another generic banner.
```

Bad:

```text
Use Kova to make listing videos.
```

## Merged review notes

Keep text and visual notes separate in artifacts, then merge into the review drafter input.

Artifacts:

- `review-notes.json`: text/content/structure observations
- `visual-notes.json`: screenshot/image observations
- `draft-input.json`: includes both

Suggested `ReviewDraftInput` extension:

```go
type ReviewDraftInput struct {
    Thread        *types.Thread
    Mode          *types.ReplyModeResult
    Notes         *types.ReviewNotes
    VisualNotes   *types.VisualReviewNotes
    Contexts      []types.LoadedReplyContext
}
```

The final drafter should prioritize high-confidence visual findings over generic text findings. If the visual notes say the CTA is visible but text notes say CTA is unclear, the drafter should phrase the issue carefully:

```text
The CTA exists, but visually it does not dominate the first screen.
```

## Review draft variants

Review mode should use review-specific variants, not the normal reply variants.

Recommended:

```text
best
short
structured-review
question-first, only if critical information is missing
```

### `best`

The comment the user should probably post.

Shape:

- acknowledge one or two strengths
- give 2-4 concrete fixes
- include at least one visual observation when available
- avoid sounding like an agency audit
- usually 150-300 words

### `short`

The shortest useful review.

Shape:

- one strength
- one or two highest-leverage fixes
- 60-130 words

### `structured-review`

Use when OP explicitly asks for critique/review and the findings justify structure.

Shape:

```text
What works:
- ...

What I would fix:
- ...

Biggest priority:
- ...
```

Avoid huge audits. Keep it postable as a Reddit comment.

### `question-first`

Only if the review cannot be usefully completed without one or two facts, such as:

- quote-only vs fixed pricing
- intended customer segment
- whether the linked page is a placeholder

Do not use `question-first` as an excuse to avoid giving visible-page feedback.

## Output rendering

Rendered markdown should make inspection depth obvious before the draft:

```text
Reply drafts for r/ecommerce

Thread: Looking for Website Review/Criticism
Mode: review
Session: /Users/.../.tider/sessions/replies/...
Inspection: Firecrawl visual
Screenshot: saved
Images analyzed: 4
Limitations: homepage only; checkout not inspected
```

If visual inspection is not available, there should be no draft output because the command should fail before drafting.

## Progress UX

Review mode should print compact progress to stderr:

```text
session: ~/.tider/sessions/replies/2026-05-01-ecommerce-1t06474
[1/8] fetching Reddit thread...
[2/8] loading contexts: personal
[3/8] classifying thread... review
[4/8] inspecting target with Firecrawl...
[5/8] saving screenshot and images...
[6/8] analyzing screenshot and product images...
[7/8] drafting review reply...
[8/8] saved session artifacts
done in 1m42s
```

Keep final drafts on stdout. Keep progress on stderr.

## Failure behavior

Review mode should fail before drafting when:

- target URL is missing
- `FIRECRAWL_API_KEY` is missing
- Firecrawl request fails
- Firecrawl returns no screenshot
- screenshot cannot be persisted
- vision-capable review model is not configured
- visual analysis fails

Each error should include the session path:

```text
Error: review mode requires visual inspection, but FIRECRAWL_API_KEY is not set.
Session preserved at /Users/.../.tider/sessions/replies/...
```

Do not write `drafts.json` or `output.md` on failed review visual inspection. Earlier artifacts should remain.

## Implementation plan

### 1. Make Firecrawl mandatory for review mode

File:

```text
cmd/tider/reply.go
reply/inspect.go
```

Change review-mode path so it does not call HTML fallback. Either:

- add `reply.InspectReviewTarget(...)` that requires Firecrawl, or
- add a `RequireVisual bool` option to `Inspect`

Preferred:

```go
inspection, err := reply.InspectReviewTarget(ctx, httpClient, targetURL)
```

`InspectHTML` can remain for tests or future non-review metadata extraction, but review mode should not use it.

### 2. Require screenshot persistence

File:

```text
cmd/tider/reply.go
reply/firecrawl.go
```

Make screenshot download fatal in review mode. Persist the screenshot path into `inspection.json`.

### 3. Select and persist image inputs

New or existing files:

```text
reply/images.go
reply/firecrawl.go
```

Implement:

- image URL normalization
- low-signal image filtering
- cap selected images
- optional image download into `images/`
- `visual-input.json`

### 4. Add visual LLM support

Files:

```text
internal/llm/llm.go
internal/llm/openai.go
reply/visual.go
prompts/review_visual.tmpl
```

The existing `llm.Request` needs to support image inputs. Keep the abstraction narrow:

```go
type ImageInput struct {
    Path string // local path; preferred when set
    URL  string // remote URL; fallback when Path is empty
    MIME string // optional override; inferred from extension if empty
}

type Request struct {
    Model     string
    Messages  []Message
    Images    []ImageInput
    JSONMode  bool
    MaxTokens int
}
```

**v1 scope:**

- OpenAI vision only. Anthropic vision is deferred to v1.5; the Anthropic provider should return a clear "model does not support image inputs" error if `Images` is non-empty.
- Vision-capable model detection: hardcoded allowlist in `internal/llm/` (e.g. `gpt-4o`, `gpt-4o-mini`, `gpt-4-turbo`). Easy to extend; we don't need a Provider.Capabilities() abstraction yet.
- Local image persistence is deferred. v1 passes Firecrawl image URLs directly to OpenAI's `image_url` content blocks. The screenshot, however, IS persisted (we already do that) and IS sent base64-encoded since signed Firecrawl URLs may expire.

Provider implementations that do not support images should return a clear error:

```text
provider anthropic/model x does not support image inputs for review_visual
```

### 5. Add visual notes type

File:

```text
internal/types/types.go
```

Add `VisualReviewNotes`, `VisualObservation`, and `VisualReviewInput` types.

### 6. Merge notes into review drafter

Files:

```text
reply/review.go
prompts/review_reply.tmpl
```

Update review drafting prompt to include:

- text notes
- visual notes
- visual limitations
- context-bank material
- top-level OP request

Prompt rule:

```text
Do not claim you reviewed visual elements unless they appear in visual_notes.
```

### 7. Update renderer

File:

```text
reply/render.go
```

Add inspection metadata to `ReplyBundle` or pass a render context so output can show:

- inspection source
- screenshot saved/not saved
- images analyzed count
- limitations

Avoid adding noisy raw paths to the final output unless useful. Session path is already printed.

### 8. Tests

Add or update tests:

```text
reply/firecrawl_test.go
reply/inspect_test.go
reply/visual_test.go
reply/review_test.go
cmd/tider/reply_test.go
reply/render_test.go
```

Test cases:

- review mode with missing `FIRECRAWL_API_KEY` fails before drafting
- Firecrawl no screenshot fails before drafting
- screenshot download failure fails before drafting
- screenshot + no image URLs succeeds with screenshot-only limitation
- image URL filtering removes logos/icons/payment badges
- visual analyzer receives screenshot input
- visual notes JSON parse errors fail clearly
- review drafter receives both text and visual notes
- renderer shows inspection depth
- no `drafts.json` on failed visual inspection

## Acceptance criteria

1. A website-review Reddit post triggers `mode=review`.
2. Review mode requires Firecrawl and fails if `FIRECRAWL_API_KEY` is missing.
3. Review mode persists a full-page screenshot in the session.
4. Review mode runs a visual analysis step before drafting.
5. Review drafts include at least one visual observation when visual notes exist.
6. Review drafts do not mention visual/layout/image feedback unless visual notes support it.
7. Text-only HTML fallback is not used for review-mode drafting.
8. The output header shows inspection depth and limitations.
9. The PND Industrial Suppliers example would not produce a draft without Firecrawl visual inspection.
10. With Firecrawl and visual analysis enabled, the PND example should critique visible page/product/CTA/category imagery, not only title/meta/headings.

## Future work

- Add optional second-page inspection for top nav/category/product pages.
- Add mobile screenshot capture if Firecrawl supports viewport options cleanly.
- Add visual diff/regeneration from saved session.
- Add `--mode=reply` only if review false positives become common.
- Add richer context metadata for Kova-style visual lenses if prose context becomes unreliable.
