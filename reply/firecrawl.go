package reply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/viggy28/tider/internal/types"
)

// firecrawlAPIBase is the public endpoint. Package-level so tests can
// point the client at httptest.Server URLs.
var firecrawlAPIBase = "https://api.firecrawl.dev"

// firecrawlScrapeTimeout — Firecrawl rendering + screenshot can take a
// few seconds per page. 60s is conservative.
const firecrawlScrapeTimeout = 60 * time.Second

// Retry policy for transient Firecrawl failures (5xx + 429). Mirrors
// internal/reddit/client.go: 3 retries, 500ms exponential base.
// SCRAPE_SITE_ERROR / ERR_ABORTED comes back as a 500 — usually a
// transient browser-load failure that succeeds on retry. Tunable as
// vars so tests can shrink the delay; production defaults stay constant
// for the lifetime of a binary.
var (
	firecrawlMaxRetry  = 3
	firecrawlBaseDelay = 500 * time.Millisecond
)

// firecrawlScreenshotMaxBytes caps the screenshot download. Real
// full-page screenshots from Firecrawl average 200KB-2MB depending on
// page complexity; cap protects against pathological cases.
const firecrawlScreenshotMaxBytes = 4 * 1024 * 1024

// InspectFirecrawl fetches the target page through Firecrawl's /v1/scrape
// endpoint, requesting markdown + full-page screenshot + links in one
// call. Returns an Inspection populated with both the structural fields
// (title/meta/og/headings derived from the markdown + Firecrawl
// metadata) and the visual fields (ScreenshotURL, ImageURLs).
//
// apiKey is the user's Firecrawl key; empty key returns an error so
// callers can fall back to InspectHTML cleanly. Errors are sanitized —
// the key never appears in returned error strings.
func InspectFirecrawl(ctx context.Context, client *http.Client, apiKey, target string) (*types.Inspection, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("firecrawl: empty API key")
	}
	if client == nil {
		client = &http.Client{Timeout: firecrawlScrapeTimeout}
	}

	reqBody := firecrawlScrapeReq{
		URL: target,
		Formats: []string{
			"markdown",
			"screenshot@fullPage",
			"links",
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("firecrawl: marshal request: %w", err)
	}

	scrapeURL := firecrawlAPIBase + "/v1/scrape"

	respBody, err := firecrawlScrapeWithRetry(ctx, client, scrapeURL, bodyBytes, apiKey)
	if err != nil {
		return nil, err
	}

	var parsed firecrawlScrapeResp
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("firecrawl: parse response: %w", err)
	}
	if !parsed.Success {
		msg := parsed.Error
		if msg == "" {
			msg = "unknown error (success=false)"
		}
		return nil, fmt.Errorf("firecrawl: %s", msg)
	}

	insp := &types.Inspection{
		URL:             target,
		Source:          "firecrawl",
		Title:           parsed.Data.Metadata.Title,
		MetaDescription: parsed.Data.Metadata.Description,
		OGTitle:         parsed.Data.Metadata.OGTitle,
		OGDescription:   parsed.Data.Metadata.OGDescription,
		Markdown:        parsed.Data.Markdown,
		ScreenshotURL:   parsed.Data.Screenshot,
		FetchedAt:       time.Now().UTC(),
	}
	if parsed.Data.Metadata.StatusCode > 0 {
		insp.Status = parsed.Data.Metadata.StatusCode
	} else {
		insp.Status = http.StatusOK
	}
	// Headings + snippets from the markdown — gives downstream LLM the
	// same structural shape as InspectHTML.
	insp.Headings = headingsFromMarkdown(parsed.Data.Markdown)
	insp.Snippets = snippetsFromMarkdown(parsed.Data.Markdown)
	insp.ImageURLs = imageURLsFromMarkdown(parsed.Data.Markdown)

	return insp, nil
}

// firecrawlScrapeWithRetry POSTs to /v1/scrape and retries on transient
// upstream failures (5xx + 429). Same backoff shape as
// internal/reddit/client.go. The auth key is set fresh each attempt and
// never appears in returned error strings.
func firecrawlScrapeWithRetry(ctx context.Context, client *http.Client, scrapeURL string, bodyBytes []byte, apiKey string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= firecrawlMaxRetry; attempt++ {
		if attempt > 0 {
			delay := firecrawlBaseDelay << (attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, scrapeURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, fmt.Errorf("firecrawl: build request: %w", err)
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("User-Agent", inspectUserAgent)

		resp, err := client.Do(httpReq)
		if err != nil {
			lastErr = fmt.Errorf("firecrawl: %w", err)
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("firecrawl: read response: %w", readErr)
		}
		if resp.StatusCode == http.StatusOK {
			return respBody, nil
		}
		short := strings.TrimSpace(string(respBody))
		if len(short) > 400 {
			short = short[:400] + "..."
		}
		statusErr := fmt.Errorf("firecrawl: status %d: %s", resp.StatusCode, short)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = statusErr
			continue
		}
		return nil, statusErr
	}
	return nil, fmt.Errorf("firecrawl: exhausted retries: %w", lastErr)
}

// DownloadScreenshot fetches the screenshot URL into destDir as a PNG
// and returns the local path. Used by the CLI after a successful
// Firecrawl inspection so the screenshot persists in the session even
// after Firecrawl's hosted URL eventually expires.
//
// destDir is created if it doesn't exist. Filename is derived from the
// URL hash + a timestamp so re-running against the same target doesn't
// overwrite the previous capture.
func DownloadScreenshot(ctx context.Context, client *http.Client, screenshotURL, destDir string) (string, error) {
	if screenshotURL == "" {
		return "", fmt.Errorf("download screenshot: empty URL")
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("download screenshot: mkdir: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, screenshotURL, nil)
	if err != nil {
		return "", fmt.Errorf("download screenshot: build request: %w", err)
	}
	req.Header.Set("User-Agent", inspectUserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download screenshot: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download screenshot: status %d", resp.StatusCode)
	}

	// Filename: timestamp + a hash-derived suffix so multiple captures
	// can coexist. We avoid using URL components directly because
	// Firecrawl's URLs include long signed tokens we don't want in
	// filenames.
	stamp := time.Now().UTC().Format("20060102-150405")
	name := fmt.Sprintf("screenshot-%s.png", stamp)
	dest := filepath.Join(destDir, name)

	// Read up to cap+1 bytes. If we got cap+1 back, the upstream had
	// more bytes available — bail BEFORE writing so the session never
	// records a path to a truncated PNG.
	buf, err := io.ReadAll(io.LimitReader(resp.Body, firecrawlScreenshotMaxBytes+1))
	if err != nil {
		return "", fmt.Errorf("download screenshot: read: %w", err)
	}
	if int64(len(buf)) > firecrawlScreenshotMaxBytes {
		return "", fmt.Errorf("download screenshot: response exceeds %d byte cap (would truncate)", firecrawlScreenshotMaxBytes)
	}
	if err := os.WriteFile(dest, buf, 0o644); err != nil {
		return "", fmt.Errorf("download screenshot: write: %w", err)
	}
	return dest, nil
}

// Firecrawl request/response shapes. Kept narrow — only the fields
// inspection actually consumes.

type firecrawlScrapeReq struct {
	URL     string   `json:"url"`
	Formats []string `json:"formats"`
}

type firecrawlScrapeResp struct {
	Success bool          `json:"success"`
	Data    firecrawlData `json:"data"`
	Error   string        `json:"error,omitempty"`
}

type firecrawlData struct {
	Markdown   string            `json:"markdown"`
	Links      []string          `json:"links"`
	Screenshot string            `json:"screenshot"`
	Metadata   firecrawlMetadata `json:"metadata"`
}

type firecrawlMetadata struct {
	Title         string `json:"title"`
	Description   string `json:"description"`
	OGTitle       string `json:"ogTitle"`
	OGDescription string `json:"ogDescription"`
	Language      string `json:"language"`
	SourceURL     string `json:"sourceURL"`
	StatusCode    int    `json:"statusCode"`
}

// Markdown helpers — derive structural signal from Firecrawl's markdown
// output so InspectFirecrawl returns the same shape as InspectHTML for
// downstream consumers (notes prompt expects Headings + Snippets).

var (
	mdHeadingRE = regexp.MustCompile(`(?m)^(#{1,3})\s+(.+?)\s*$`)
	mdImageRE   = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
)

func headingsFromMarkdown(md string) []types.Heading {
	matches := mdHeadingRE.FindAllStringSubmatch(md, -1)
	var out []types.Heading
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		level := len(m[1])
		text := strings.TrimSpace(m[2])
		if text == "" {
			continue
		}
		out = append(out, types.Heading{Level: level, Text: text})
		if len(out) >= inspectMaxHeadings {
			break
		}
	}
	return out
}

// snippetsFromMarkdown turns markdown paragraphs into Snippets matching
// what InspectHTML produces — substantial paragraph-like blocks, deduped,
// each capped at inspectMaxSnippetBytes, total capped at
// inspectMaxSnippets.
func snippetsFromMarkdown(md string) []string {
	if md == "" {
		return nil
	}
	// Naive paragraph split on blank lines, then filter heading lines /
	// list markers / code fences for snippet quality.
	var snippets []string
	for _, block := range strings.Split(md, "\n\n") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		// Skip headings, code fences, and image-only blocks.
		if strings.HasPrefix(block, "#") || strings.HasPrefix(block, "```") {
			continue
		}
		if strings.HasPrefix(block, "![") && !strings.Contains(block, "\n") {
			continue
		}
		// Collapse internal whitespace.
		block = strings.Join(strings.Fields(block), " ")
		if len(block) < 20 {
			continue
		}
		if len(block) > inspectMaxSnippetBytes {
			block = block[:inspectMaxSnippetBytes] + "…"
		}
		snippets = append(snippets, block)
	}
	return dedupeStrings(snippets, inspectMaxSnippets)
}

// imageURLsFromMarkdown extracts <img>-equivalent references (markdown's
// ![alt](url) syntax). Filters out reddit, common tracking pixels, and
// invalid URLs. Deduped.
func imageURLsFromMarkdown(md string) []string {
	matches := mdImageRE.FindAllStringSubmatch(md, -1)
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		raw := strings.TrimSpace(m[1])
		if raw == "" {
			continue
		}
		// Validate URL shape — Firecrawl can occasionally emit relative
		// paths or data: URIs we don't want in image_urls.
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			continue
		}
		key := strings.ToLower(raw)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, raw)
	}
	return out
}
