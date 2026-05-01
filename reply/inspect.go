package reply

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/viggy28/tider/internal/types"
)

// Inspection budgets — bounded so the downstream review-notes prompt
// stays under the LLM's context window even for bloated marketing sites.
const (
	inspectMaxBytes        = 256 * 1024
	inspectMaxSnippets     = 30
	inspectMaxSnippetBytes = 400
	inspectMaxHeadings     = 30
)

// inspectUserAgent identifies tider when fetching review targets. Some
// sites gate scraping by UA — we use a polite, identifiable string with
// a contact path.
const inspectUserAgent = "tider/0.1 (review-mode inspection; contact /u/tider28)"

// Inspect dispatches to a backend based on environment:
//
//   - If FIRECRAWL_API_KEY is set → InspectFirecrawl. Returns markdown,
//     full-page screenshot URL, and embedded image URLs alongside the
//     usual structural fields. Inspection.Source = "firecrawl".
//   - Otherwise → InspectHTML. stdlib + x/net/html, text-only signal.
//     Inspection.Source = "html".
//
// If Firecrawl is configured but its call fails (transient outage,
// invalid key, quota/rate-limit error), Inspect emits a warning and
// falls back to InspectHTML so review mode still produces usable notes
// instead of hard-failing the whole reply pipeline. The fallback is
// surfaced on stderr so users can tell their key isn't being honored.
//
// Both backends return *types.Inspection with the same shape;
// downstream steps (review notes, drafter) read whatever's present.
func Inspect(ctx context.Context, client *http.Client, target string) (*types.Inspection, error) {
	if key := strings.TrimSpace(os.Getenv("FIRECRAWL_API_KEY")); key != "" {
		insp, err := InspectFirecrawl(ctx, client, key, target)
		if err == nil {
			return insp, nil
		}
		// Don't fall back if the user canceled — a context error means
		// the operator wants out, not a degraded retry.
		if ctx.Err() != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "warning: Firecrawl inspection failed (%v); falling back to HTML inspection (no screenshot/images)\n", err)
	}
	return InspectHTML(ctx, client, target)
}

// InspectHTML is the stdlib-only backend: fetch HTML, parse via
// golang.org/x/net/html, extract title/meta/og/headings/snippets. Always
// available — no API key required, but sees only text content.
//
// Errors:
//   - non-2xx → fail with status code preserved on the returned error
//   - HTML parse errors on real HTML are rare (x/net/html is forgiving);
//     malformed input still produces a partial Inspection where possible
//   - network errors are propagated
func InspectHTML(ctx context.Context, client *http.Client, target string) (*types.Inspection, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", inspectUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", target, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("inspect %s: status %d", target, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, inspectMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("inspect %s: read body: %w", target, err)
	}

	insp, err := parseHTMLInspection(body)
	if err != nil {
		return nil, fmt.Errorf("inspect %s: parse: %w", target, err)
	}
	insp.URL = target
	insp.Status = resp.StatusCode
	insp.Source = "html"
	insp.FetchedAt = time.Now().UTC()
	return insp, nil
}

func parseHTMLInspection(data []byte) (*types.Inspection, error) {
	root, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}

	insp := &types.Inspection{}
	walk(root, insp, walkState{})
	// Trim & dedupe snippets/headings after collection.
	insp.Snippets = dedupeStrings(insp.Snippets, inspectMaxSnippets)
	if len(insp.Headings) > inspectMaxHeadings {
		insp.Headings = insp.Headings[:inspectMaxHeadings]
	}
	return insp, nil
}

// walkState tracks whether we're inside elements whose text content we
// should ignore (script/style/nav/footer/aside/svg). Headings and
// paragraph snippets remain meaningful elsewhere.
type walkState struct {
	skipText bool
}

func walk(n *html.Node, out *types.Inspection, state walkState) {
	if n == nil {
		return
	}
	if n.Type == html.ElementNode {
		switch strings.ToLower(n.Data) {
		case "script", "style", "noscript", "svg", "iframe":
			return // skip subtree entirely — never useful for review
		case "nav", "footer", "aside", "form":
			state.skipText = true
		case "title":
			if t := textContent(n); t != "" && out.Title == "" {
				out.Title = collapseWhitespace(t)
			}
		case "meta":
			handleMeta(n, out)
		case "h1", "h2", "h3":
			level := int(n.Data[1] - '0')
			if t := textContent(n); t != "" {
				out.Headings = append(out.Headings, types.Heading{
					Level: level,
					Text:  collapseWhitespace(t),
				})
			}
			// Don't descend further — h tags' text is captured.
			return
		case "p", "li", "blockquote", "dd":
			if !state.skipText {
				if t := textContent(n); t != "" {
					t = collapseWhitespace(t)
					if len(t) > inspectMaxSnippetBytes {
						t = t[:inspectMaxSnippetBytes] + "…"
					}
					if len(t) >= 20 { // skip stub-y "Read more" type links
						out.Snippets = append(out.Snippets, t)
					}
				}
			}
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, out, state)
	}
}

func handleMeta(n *html.Node, out *types.Inspection) {
	var name, property, content string
	for _, a := range n.Attr {
		switch strings.ToLower(a.Key) {
		case "name":
			name = strings.ToLower(a.Val)
		case "property":
			property = strings.ToLower(a.Val)
		case "content":
			content = a.Val
		}
	}
	content = collapseWhitespace(content)
	if content == "" {
		return
	}
	switch {
	case name == "description" && out.MetaDescription == "":
		out.MetaDescription = content
	case property == "og:title" && out.OGTitle == "":
		out.OGTitle = content
	case property == "og:description" && out.OGDescription == "":
		out.OGDescription = content
	}
}

// textContent recursively concatenates text-node descendants of n,
// stopping at script/style/noscript subtrees.
func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode {
		switch strings.ToLower(n.Data) {
		case "script", "style", "noscript":
			return ""
		}
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(textContent(c))
	}
	return sb.String()
}

// collapseWhitespace turns runs of whitespace (incl. newlines) into a
// single space and trims the result. HTML whitespace rules say multiple
// spaces collapse anyway; we mimic that for clean snippets.
func collapseWhitespace(s string) string {
	if s == "" {
		return ""
	}
	var sb strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				sb.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		sb.WriteRune(r)
		prevSpace = false
	}
	return strings.TrimSpace(sb.String())
}

func dedupeStrings(in []string, cap int) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
		if len(out) >= cap {
			break
		}
	}
	return out
}

// ErrInspectionFailed wraps inspection errors so callers can preserve
// session state when inspection is the failing step (vs. a downstream
// drafter error).
var ErrInspectionFailed = errors.New("review inspection failed")
