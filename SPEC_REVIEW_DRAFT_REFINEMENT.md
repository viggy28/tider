# Spec: visual review draft refinement for `tider reply`

## Status

Draft spec. This refines the review-mode drafting step after Firecrawl visual inspection has already succeeded.

This spec is about the final Reddit reply quality, not about target capture. Firecrawl screenshot capture, image selection, and visual-note generation are covered by `SPEC_REVIEW_VISUAL_FIRECRAWL.md`.

## Reference session

Use this real session as the primary implementation fixture:

```text
/Users/viggy28/.tider/sessions/replies/2026-05-01-ecommerce-1t06474-2
```

Command that produced it:

```text
./tider reply --url=https://www.reddit.com/r/ecommerce/comments/1t06474/looking_for_website_reviewcriticism/ --context=kova --context=personal
```

Important artifacts:

```text
thread.json           # Reddit OP + comments
contexts.json         # kova + personal context snapshots
mode.json             # mode=review
inspection.json       # Firecrawl result; screenshot + image URLs
visual-input.json     # exact screenshot/images sent to visual analyzer
visual-notes.json     # visual observations generated from screenshot/images
review-notes.json     # text/content observations
draft-input.json      # combined review drafter input
drafts.json           # generated drafts with reasoning for audit
output.md             # rendered terminal output
screenshots/
  screenshot-20260501-205322.png
```

The screenshot is a full-page homepage capture. The selected image refs are category/product images. `visual-notes.json` classifies the site as:

```json
"shop_type": "b2b_industrial"
```

The output header correctly showed:

```text
Inspection: Firecrawl visual
Screenshot: saved
Images analyzed: 8
Shop type: b2b_industrial
```

So the capture/visual-analysis pipeline worked. The remaining problem is the final review draft.

## Problem

The current review reply is too much like an audit.

Observed output problems:

- Too many bullets and too many fixes.
- The reply reads like an agency checklist, not a Reddit comment.
- Reasoning lines are rendered in terminal output.
- The output uses `Alternatives` / `Short` instead of the refined `Alternative Picks` / `Shorter` style.
- Pricing/policy advice gets too much attention for a B2B industrial supplier where quote-driven purchasing may be normal.
- The draft underuses the Kova-relevant angle: visual proof, real usage imagery, buyer trust, and reusable organic content.
- Some visual claims are overconfident or loosely grounded:
  - "header overwhelm" is not strongly supported by the screenshot.
  - "images are isolated on plain backgrounds" is partly true, but the page also contains contextual category images.
  - mobile risk is listed as high even though mobile was not inspected.

The right output should be digestible: 2-3 high-leverage suggestions, centered on what the visual inspection actually supports.

## Product stance

For store/shop review threads, especially when `--context=kova` is supplied, Tider should not produce a generic website audit.

It should use visual inspection to answer:

- Does the site show enough real product proof?
- Do the images help a buyer understand scale, material, use, category, quality, and trust?
- Are the visuals mostly catalog/category imagery, or do they show real use/context?
- What visual assets could improve both the website and organic content?
- Can simple product/process/use-case clips make the page feel more trustworthy?

The draft should help the Reddit OP and still align with why the user is engaging: learning and contributing around Kova's thesis without naming or pitching Kova.

## Goals

1. Make review-mode replies digestible and postable.
2. Limit `Best Pick` to 2-3 strongest fixes.
3. Prefer visual/product-proof feedback over generic website-audit items when visual inspection exists.
4. Use Kova context as a lens: real product proof, usage imagery, process clips, buyer trust, and organic reuse.
5. Remove reasoning from rendered markdown output.
6. Avoid pricing/policy tangents unless they are the clearest blocker or OP explicitly asks about them.
7. Ground every visual claim in `visual-notes.json`, `inspection.json`, or the screenshot.
8. Keep `drafts.json` useful for audit/debug without overloading terminal output.

## Non-goals

- No Reddit posting.
- No changes to Firecrawl capture requirements.
- No new CLI flags.
- No `--text-only`.
- No Kova naming or product pitch by default.
- No full conversion-rate audit.
- No Lighthouse, SEO, accessibility, or performance audit.
- No broad multi-page crawl in this spec.

## Target output format

Review mode should follow the same rendering philosophy as reply mode: postable comments first, no explanatory report.

```text
Reply drafts for r/<subreddit>

Thread: <thread title>
Mode: review
Session: <session path>
Inspection: Firecrawl visual
Screenshot: saved
Images analyzed: <n>
Shop type: <shop_type>
Limitations: <limitations>

Best Pick

<ready-to-post review comment>

Alternative Picks

Shorter

<shorter ready-to-post review comment>

Visual / Organic Angle

<image/product-proof/content-reuse angle, if Best Pick did not already center it>

Structured Review

<skimmable structured version, only if useful>

Question

<question-first version, only if critical context is missing>
```

Rendering rules:

- No `Why this works`.
- No reasoning under variants.
- No `Editing Notes`.
- Use `Alternative Picks`, not `Alternatives`.
- Use `Shorter`, not `Short`.
- Render only variants that exist.
- Keep default output to 2-3 drafts total.
- `Structured Review` is optional and should not be a full audit.

`drafts.json` may still keep `reasoning` for audit/debug. The markdown renderer should not print it.

## Variant model

### `best` / `Best Pick`

The comment the user is most likely to post.

Default shape:

- one quick strength
- 2-3 strongest fixes
- one concrete "thing I would change first" or "one thing I did not like"
- 150-260 words for review mode
- short paragraphs by default
- bullets only if they make the comment easier to scan

When Kova context is present and visual notes support it, `Best Pick` should usually include the visual/product-proof angle.

### `shorter` / `Shorter`

Compressed version.

Default shape:

- one strength
- 1-2 strongest fixes
- 70-140 words
- no audit framing

### `visual-organic` / `Visual / Organic Angle`

Use when the main `Best Pick` did not already center image/product-proof feedback.

Purpose:

- connect visual proof on the site to reusable organic content
- suggest real use/process/category imagery
- point out where stock/category visuals are weaker than real proof

Skip this variant if `Best Pick` already uses this frame strongly.

### `structured-review` / `Structured Review`

Optional. Use only if the OP clearly asked for critique and the findings benefit from structure.

Shape:

```text
What works:
- ...

What I would fix first:
- ...
- ...

Biggest priority:
- ...
```

Hard limits:

- max 3 sections
- max 5 total bullets
- no pricing/policy section by default

### `question` / `Question`

Only if a critical missing fact blocks useful feedback.

Examples:

- the site is password-gated
- the screenshot does not show the relevant page
- OP asks about conversion but no target customer/business model is clear

Do not use `question` as a way to avoid giving visible-page feedback.

## Kova lens

When `--context=kova` is present, review drafts should bias toward visual proof, not generic web critique.

Good Kova-shaped observations:

- "The site is clear, but the visuals feel more like category/catalog imagery than proof of the products in use."
- "For PPE/safety, real usage shots would build more trust: workers wearing gear, tools on-site, material closeups, certification labels, packaging, delivery/customer examples."
- "Those same assets can become organic posts, LinkedIn updates, WhatsApp sales collateral, or short product/process clips."
- "A short real product/use-case clip would probably do more for trust than another generic hero image."

Bad Kova usage:

- "Use Kova."
- "I built a tool for this."
- "Etsy listing videos would solve this" when the site is B2B industrial.
- Turning every review into a video pitch.

The Kova lens should shape the advice, not become the topic.

## Pricing and policy guidance

Do not default to pricing as a top fix.

For B2B, industrial, wholesale, custom, quote-driven, or service-heavy sites:

- exact prices may not be expected
- "request quote" may be the correct conversion path
- useful feedback is quote-path clarity, not "publish pricing"

Allowed if grounded:

- "If this is quote-only, make that explicit."
- "Add MOQ/lead-time/spec-sheet expectations near relevant categories."
- "Make the quote CTA appear at the decision point."

Avoid unless OP asks or the page is clearly consumer ecommerce:

- "Publish prices."
- "Show exact pricing."
- "Pricing is the biggest issue."
- "Return policy is a top priority."

For the PND session, pricing/policy should not be one of the top 2-3 fixes. The stronger suggestions are visual proof, trust near categories, and quote path at the product/category decision point.

## Grounding rules

The review drafter must not overclaim.

Rules:

- Do not say "header is crowded" unless the screenshot clearly shows crowding.
- Do not say images are all plain-background if there are contextual category images; use precise wording like "some imagery feels catalog/category-like."
- Do not make mobile recommendations from a desktop screenshot as a main fix. Put mobile under limitations unless a mobile screenshot was analyzed.
- Do not claim product pages, checkout, FAQ, or return policy were reviewed unless those pages are in the session artifacts.
- Do not call something "high severity" in the final reply unless it is one of the selected top fixes.

The drafter should rank candidate findings before writing:

1. Is this strongly supported by visual/text artifacts?
2. Is it high leverage for OP?
3. Does it fit the thread/subreddit tone?
4. Does it fit supplied context, especially Kova?

Only the top 2-3 should make `Best Pick`.

## Ideal output for the PND session

Given:

```text
tider reply --url=https://www.reddit.com/r/ecommerce/comments/1t06474/looking_for_website_reviewcriticism/ --context=kova --context=personal
```

With session:

```text
/Users/viggy28/.tider/sessions/replies/2026-05-01-ecommerce-1t06474-2
```

`Best Pick` should resemble:

```text
First impression is solid: I understood quickly that you are an industrial/PPE supplier, and the authorized brand mentions help.

The biggest thing I would improve is the visual proof. A lot of the site feels like category/catalog imagery. For safety/PPE, I would want to see more real-world context: workers wearing the gear, tools being used on-site, closeups of materials/certifications, packaging, and a few customer or industry examples.

That kind of imagery does two jobs: it makes the website feel more trustworthy, and it gives you reusable organic content for LinkedIn, Instagram, WhatsApp, or sales follow-up without inventing separate marketing posts.

I would also move trust signals closer to the product/category sections. If you are an authorized partner or products meet specific standards, show that near the relevant cards, not only in general site copy.

One thing I did not like: the site is clear, but it does not yet show enough real usage proof to make a buyer feel confident quickly.
```

`Shorter` should resemble:

```text
The site is clear, and I understood the PPE/industrial focus quickly.

The main thing I would improve is visual proof. A lot of the imagery feels like category/catalog imagery. For safety products, I would add real usage shots: workers wearing PPE, tools on-site, material/certification closeups, packaging, and customer/industry examples.

Those same visuals can also become organic content for LinkedIn/Instagram/WhatsApp, so it is not just a website improvement.
```

`Structured Review`, if generated, should resemble:

```text
What works:
- Clear positioning as an industrial/PPE supplier.
- Authorized brand mentions help credibility.

What I would fix first:
- Add more real-world product proof: workers wearing gear, tools on-site, material/certification closeups.
- Move trust signals closer to the category/product cards.

Biggest priority:
- Make the visuals feel less like a catalog and more like proof that these products are used by real customers in real environments.
```

## Implementation plan

### 1. Update review reply prompt

Files:

```text
prompts/review_reply.tmpl
reply/review.go
```

Prompt changes:

- instruct the model to choose only 2-3 strongest fixes for `best`
- bias toward visual/product-proof feedback when `VisualNotes` exist
- add Kova lens guidance when contexts include Kova-like product-proof material
- remove generic audit tone
- discourage pricing/policy as default top fixes for B2B/quote-driven sites
- require grounding in `visual_notes` and `review_notes`
- prohibit mobile/layout claims not supported by artifacts
- rename variants:
  - `short` -> `shorter`
  - `question-first` -> `question`
  - keep `structured-review`
  - optionally add `visual-organic`
- continue returning strict JSON
- keep `reasoning` in JSON if useful, but make clear it is not rendered

### 2. Update renderer

File:

```text
reply/render.go
```

Changes:

- render `Alternative Picks`, not `Alternatives`
- render `Shorter`, not `Short`
- render `Structured Review`, not `Structured-Review`
- render `Visual / Organic Angle` for `visual-organic`
- render `Question` for `question`
- do not print `Reasoning`
- preserve review header metadata:
  - `Inspection`
  - `Screenshot`
  - `Images analyzed`
  - `Shop type`
  - `Limitations`

### 3. Update tests

Files:

```text
reply/review_test.go
reply/render_test.go
```

Add/update tests for:

- review prompt includes 2-3 strongest fixes guidance
- review prompt includes Kova/product-proof lens when contexts are supplied
- review prompt discourages pricing/policy as default B2B top fixes
- review prompt warns against unsupported mobile/header/image overclaims
- generated review variants use `shorter`, `structured-review`, optional `visual-organic`, optional `question`
- renderer prints `Alternative Picks`
- renderer does not print `Reasoning`
- renderer maps labels correctly

### 4. Manual validation

Re-run:

```text
./tider reply --url=https://www.reddit.com/r/ecommerce/comments/1t06474/looking_for_website_reviewcriticism/ --context=kova --context=personal
```

Compare against the fixture session:

```text
/Users/viggy28/.tider/sessions/replies/2026-05-01-ecommerce-1t06474-2
```

Validation checks:

- `Best Pick` is digestible and under ~260 words.
- `Best Pick` has 2-3 fixes, not 5+.
- no reasoning lines are rendered.
- no pricing/policy tangent unless tightly framed as quote-path clarity.
- visual/product-proof angle appears when Kova context is present.
- no unsupported "header crowded" claim.
- no mobile recommendation as a top fix unless mobile screenshot was analyzed.
- `Alternative Picks` uses postable drafts, not audit commentary.

## Acceptance criteria

1. Review-mode markdown output no longer renders reasoning lines.
2. Review-mode markdown uses `Alternative Picks`.
3. Review-mode `Best Pick` defaults to 2-3 high-leverage fixes.
4. When visual notes exist and Kova context is present, review drafts prioritize image/product-proof/organic-content feedback when supported.
5. B2B/quote-driven sites do not get pricing/policy as default top advice.
6. Visual claims are tightly grounded in `visual-notes.json`, `inspection.json`, and the screenshot.
7. The PND session produces a digestible Reddit comment, not an agency-style audit.
8. `drafts.json` can retain `reasoning` for audit/debug, but `output.md` and terminal output do not show it.

## Future work

- Add mobile screenshot analysis.
- Add product/category page follow-up inspection.
- Add session regeneration for review angles:

  ```text
  tider reply regen --session=<path> --angle=visual-organic
  ```

- Add a human-selectable review focus only if real usage shows the default focus is too narrow.
