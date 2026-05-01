// Package reply implements `tider reply` — drafting Reddit comment-style
// replies in either of two internal modes:
//
//   - reply: normal discussion thread; draft from OP + selected comments
//     + context.
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
// are merged with URLs extracted from the body (markdown links + raw
// URLs) plus the outbound URL, deduplicated, with reddit/image links
// filtered out.
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
	mdLinkRE = regexp.MustCompile(`\[[^\]]*\]\((https?://[^\s)]+)\)`)
	rawURLRE = regexp.MustCompile(`https?://[^\s)\]>]+`)
)

func extractURLs(t *types.Thread) []string {
	var raw []string
	if t.OutboundURL != "" {
		raw = append(raw, t.OutboundURL)
	}
	for _, m := range mdLinkRE.FindAllStringSubmatch(t.Body, -1) {
		if len(m) > 1 {
			raw = append(raw, m[1])
		}
	}
	for _, u := range rawURLRE.FindAllString(t.Body, -1) {
		raw = append(raw, u)
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

func hasImageExt(u string) bool {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg"} {
		if strings.HasSuffix(u, ext) {
			return true
		}
	}
	return false
}

// mergeTargetURLs combines two URL lists, dedupes (case-insensitive
// host, exact path), and preserves the order of the LLM's picks first
// (the model's judgment about "the target" wins for ranking).
func mergeTargetURLs(primary, fallback []string) []string {
	if len(primary) == 0 && len(fallback) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		key := strings.ToLower(u)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, u)
	}
	for _, u := range primary {
		add(u)
	}
	for _, u := range fallback {
		add(u)
	}
	// Stable order preserves LLM ranking; sort only when LLM gave nothing.
	if len(primary) == 0 {
		sort.Strings(out)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
