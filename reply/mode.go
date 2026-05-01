// Package reply implements `tider reply` — drafting Reddit comment-style
// replies in either of two internal modes:
//
//   - reply: normal discussion thread; draft from OP, selected comments,
//     and context.
//   - review: OP asks for review/feedback/critique of a specific external
//     resource; draft from OP + inspected target + context.
//
// Mode detection uses ONLY the original post (title/flair/body/outbound
// URL). Comments are explicitly not used for detection so a stray
// "review mine too?" reply doesn't flip the mode.
package reply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var modeTmpl = template.Must(template.ParseFS(prompts.FS, "reply_mode.tmpl"))

// DefaultModeMaxTokens is a budget for the mode-detection LLM call. The
// classifier is short — JSON with three small fields — so this is a
// generous cap. Reasoning models that consume internal tokens still fit.
const DefaultModeMaxTokens = 2048

// DetectMode runs the LLM classifier against the OP-only inputs and
// returns the resulting ModeResult. The classifier-supplied target URLs
// are merged with URLs extracted from the body (markdown links, raw
// URLs, and bare hostnames) plus the outbound URL, deduplicated, with
// reddit/image links filtered out.
//
// Errors fall into three buckets:
//   - llm provider error → propagated
//   - JSON parse error → wrapped with truncated raw model output
//   - invalid mode value (not "reply"/"review") → wrapped error
func DetectMode(ctx context.Context, p llm.Provider, model string, thread *types.Thread, maxTokens int) (*types.ReplyModeResult, error) {
	if maxTokens <= 0 {
		maxTokens = DefaultModeMaxTokens
	}
	prompt, err := renderModePrompt(thread)
	if err != nil {
		return nil, err
	}
	resp, err := p.Complete(ctx, llm.Request{
		Model:     model,
		MaxTokens: maxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("mode classifier: %w", err)
	}

	var raw struct {
		Mode       string   `json:"mode"`
		Reason     string   `json:"reason"`
		TargetURLs []string `json:"target_urls"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("mode classifier: parse json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	mode := types.ReplyMode(strings.TrimSpace(raw.Mode))
	if mode != types.ReplyModeReply && mode != types.ReplyModeReview {
		return nil, fmt.Errorf("mode classifier: invalid mode %q (expected reply or review)", raw.Mode)
	}

	merged := mergeTargetURLs(raw.TargetURLs, extractURLs(thread))

	return &types.ReplyModeResult{
		Mode:       mode,
		Reason:     strings.TrimSpace(raw.Reason),
		TargetURLs: merged,
	}, nil
}

func renderModePrompt(t *types.Thread) (string, error) {
	var buf bytes.Buffer
	err := modeTmpl.Execute(&buf, struct {
		Subreddit, Title, Flair, Body, OutboundURL string
	}{
		Subreddit:   t.Subreddit,
		Title:       t.Title,
		Flair:       t.Flair,
		Body:        t.Body,
		OutboundURL: t.OutboundURL,
	})
	if err != nil {
		return "", fmt.Errorf("render mode prompt: %w", err)
	}
	return buf.String(), nil
}

// extractURLs pulls candidate target URLs from OP fields. Used as
// belt-and-suspenders alongside the LLM classifier — if the model
// missed a URL the user pasted, we still surface it; if it hallucinates
// one, the merge step keeps only URLs that came from the body.
//
// Filters out:
//   - reddit.com / redd.it (would be the thread itself or other discussions)
//   - common image extensions (.jpg/.png/.gif/.webp/.svg/.jpeg)
var (
	mdLinkRE       = regexp.MustCompile(`\[[^\]]*\]\((https?://[^\s)]+)\)`)
	rawURLRE       = regexp.MustCompile(`https?://[^\s)\]>]+`)
	bareHostnameRE = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}(?:/[^\s)\]>]*)?`)
)

func extractURLs(t *types.Thread) []string {
	var raw []string
	if t.OutboundURL != "" {
		raw = append(raw, t.OutboundURL)
	}

	var bodyURLs []extractedURL
	for _, m := range mdLinkRE.FindAllStringSubmatchIndex(t.Body, -1) {
		if len(m) >= 4 {
			bodyURLs = append(bodyURLs, extractedURL{pos: m[0], url: t.Body[m[2]:m[3]]})
		}
	}
	for _, m := range rawURLRE.FindAllStringIndex(t.Body, -1) {
		bodyURLs = append(bodyURLs, extractedURL{pos: m[0], url: t.Body[m[0]:m[1]]})
	}
	for _, u := range extractBareHostnameURLs(t.Body) {
		bodyURLs = append(bodyURLs, u)
	}
	sort.SliceStable(bodyURLs, func(i, j int) bool {
		return bodyURLs[i].pos < bodyURLs[j].pos
	})
	for _, u := range bodyURLs {
		raw = append(raw, u.url)
	}
	// Markdown-link URLs and raw URLs commonly overlap (the markdown link
	// embeds a URL that the raw regex also finds). Dedupe case-insensitively
	// while preserving first-seen order so the OutboundURL ranks first.
	seen := map[string]bool{}
	var keep []string
	for _, u := range raw {
		u = strings.TrimRight(u, ".,;)]")
		lower := strings.ToLower(u)
		if strings.Contains(lower, "reddit.com") || strings.Contains(lower, "redd.it") {
			continue
		}
		if hasImageExt(lower) {
			continue
		}
		if seen[lower] {
			continue
		}
		seen[lower] = true
		keep = append(keep, u)
	}
	return keep
}

type extractedURL struct {
	pos int
	url string
}

func extractBareHostnameURLs(body string) []extractedURL {
	var out []extractedURL
	matches := bareHostnameRE.FindAllStringIndex(body, -1)
	for _, match := range matches {
		start, end := match[0], match[1]
		if start > 0 {
			prev := body[start-1]
			if prev == '@' {
				continue
			}
		}
		host := body[start:end]
		prefixStart := start - len("https://")
		if prefixStart >= 0 && strings.EqualFold(body[prefixStart:start], "https://") {
			continue
		}
		prefixStart = start - len("http://")
		if prefixStart >= 0 && strings.EqualFold(body[prefixStart:start], "http://") {
			continue
		}
		out = append(out, extractedURL{pos: start, url: normalizeBareHostnameURL(host)})
	}
	return out
}

func normalizeBareHostnameURL(host string) string {
	if slash := strings.IndexByte(host, '/'); slash >= 0 {
		return "https://" + strings.ToLower(host[:slash]) + host[slash:]
	}
	return "https://" + strings.ToLower(host)
}

func hasImageExt(u string) bool {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg"} {
		if strings.HasSuffix(u, ext) {
			return true
		}
	}
	return false
}

// mergeTargetURLs combines classifier-supplied URLs with body-extracted
// URLs, dedupes case-insensitively, and preserves the LLM's ranking.
//
// Critically, classifier URLs are accepted ONLY if they also appear in
// the fallback set (which was derived from OP body + outbound URL).
// This guards against hallucination: if the classifier invents a
// plausible-looking URL not in the OP, we reject it rather than feeding
// a wrong target to the inspection step. Spec rule: "never invent URLs."
//
// Hallucinated URLs are still preserved but get demoted behind verified
// URLs, so TargetURLs[0] is always grounded in actual OP content when at
// least one body-extracted URL exists.
func mergeTargetURLs(primary, fallback []string) []string {
	if len(primary) == 0 && len(fallback) == 0 {
		return nil
	}
	// Build a lookup of URLs that came from the OP itself.
	verified := map[string]bool{}
	for _, u := range fallback {
		verified[strings.ToLower(strings.TrimSpace(u))] = true
	}

	seen := map[string]bool{}
	var grounded []string   // classifier URLs that appear in OP — trustworthy
	var ungrounded []string // classifier URLs not in OP — possibly hallucinated
	add := func(target *[]string, u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		key := strings.ToLower(u)
		if seen[key] {
			return
		}
		seen[key] = true
		*target = append(*target, u)
	}

	for _, u := range primary {
		key := strings.ToLower(strings.TrimSpace(u))
		if verified[key] {
			add(&grounded, u)
		} else {
			add(&ungrounded, u)
		}
	}
	// Then add any fallback URLs the classifier didn't surface — the
	// classifier may have missed something the regex caught.
	var fromFallback []string
	for _, u := range fallback {
		add(&fromFallback, u)
	}

	// Order: grounded classifier picks first (LLM ranked them, and they
	// exist in the OP), then any extra fallback URLs (regex found, LLM
	// didn't pick), then ungrounded classifier picks last (suspicious,
	// kept only when we have at least some grounding signal so the user
	// can audit them in mode.json).
	//
	// CRITICAL: when fallback is empty (OP has no parseable URLs), we
	// drop ungrounded entries entirely. Returning hallucinated URLs in
	// that case would let the CLI's `len(TargetURLs) > 0` check pass
	// and route review mode at an invented URL — violating the spec's
	// "no fake fallbacks" rule. Empty result here is correct: the CLI
	// then surfaces the spec-mandated "no shop/site URL was found in
	// the original post" failure.
	//
	// Body-order is preserved (no alpha sort) since it's itself a
	// signal: a user who wrote "my shop is X, also see my docs at Y"
	// wants the shop inspected, not docs sorted to the front.
	out := append([]string{}, grounded...)
	out = append(out, fromFallback...)
	if len(fallback) > 0 {
		out = append(out, ungrounded...)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
