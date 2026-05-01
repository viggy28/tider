package reply

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var reviewVisualTmpl = template.Must(template.ParseFS(prompts.FS, "review_visual.tmpl"))

// DefaultVisualMaxTokens — the visual analyzer returns structured JSON
// with bounded fields; 4096 is generous and accommodates reasoning
// models that consume internal tokens.
const DefaultVisualMaxTokens = 4096

// VisualInput is what AnalyzeVisual needs. Inspection provides the
// captured artifacts (screenshot path, image URLs, title, headings) and
// Contexts seed the Kova lens. ScreenshotPath MUST point at a local
// file — the analyzer reads + base64-encodes it so a re-run after the
// signed Firecrawl URL expires still works.
type VisualInput struct {
	Inspection *types.Inspection
	Contexts   []types.LoadedReplyContext
	// ImageURLs are the specific product/page image references to attach
	// (already filtered to exclude logos/icons/payment badges by
	// SelectImagesForAnalysis). Pass nil for screenshot-only analysis.
	ImageURLs []string
}

// AnalyzeVisual runs the visual review prompt against the screenshot and
// optional product images. The provider must support image inputs —
// callers should gate with llm.SupportsVision(provider, model) and fail
// fast at the CLI layer rather than trip a provider-level error here.
func AnalyzeVisual(ctx context.Context, p llm.Provider, model string, input *VisualInput, maxTokens int) (*types.VisualReviewNotes, error) {
	if input == nil || input.Inspection == nil {
		return nil, fmt.Errorf("visual analyzer: nil input or inspection")
	}
	insp := input.Inspection
	if insp.ScreenshotPath == "" {
		return nil, fmt.Errorf("visual analyzer: ScreenshotPath required (review-mode invariant)")
	}
	if maxTokens <= 0 {
		maxTokens = DefaultVisualMaxTokens
	}

	prompt, err := renderVisualPrompt(input)
	if err != nil {
		return nil, err
	}

	// Build the image inputs: screenshot first (always), then any
	// selected product images. Screenshot uses local Path so the request
	// is self-contained; product images use remote URLs (v1 doesn't
	// download them per spec).
	images := []llm.ImageInput{{Path: insp.ScreenshotPath}}
	for _, u := range input.ImageURLs {
		images = append(images, llm.ImageInput{URL: u})
	}

	resp, err := p.Complete(ctx, llm.Request{
		Model:     model,
		MaxTokens: maxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
		Images:    images,
	})
	if err != nil {
		return nil, fmt.Errorf("visual analyzer: %w", err)
	}

	var raw struct {
		ShopType     string                     `json:"shop_type"`
		Summary      string                     `json:"summary"`
		Observations []types.VisualObservation  `json:"observations"`
		KovaSignals  []string                   `json:"kova_signals"`
		Questions    []string                   `json:"questions"`
		Limitations  []string                   `json:"limitations"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("visual analyzer: parse json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	if raw.ShopType == "" {
		// The classification gate matters for the KovaSignals filter —
		// missing shop_type means we can't trust the kova_signals
		// payload. Fail loud.
		return nil, fmt.Errorf("visual analyzer: response missing shop_type (required for KovaSignals gating)")
	}
	// Belt-and-suspenders: enforce the KovaSignals gate on this side too,
	// in case the model violates the prompt rule. handmade and boutique
	// are the only ShopTypes where kova_signals are valid.
	if raw.ShopType != "handmade" && raw.ShopType != "boutique" {
		raw.KovaSignals = nil
	}

	return &types.VisualReviewNotes{
		TargetURL:    insp.URL,
		ShopType:     raw.ShopType,
		Summary:      raw.Summary,
		Observations: raw.Observations,
		KovaSignals:  raw.KovaSignals,
		Questions:    raw.Questions,
		Limitations:  raw.Limitations,
		Generated:    time.Now().UTC(),
	}, nil
}

// RenderVisualPrompt is exported for inspection / dry-run use.
func RenderVisualPrompt(input *VisualInput) (string, error) {
	return renderVisualPrompt(input)
}

func renderVisualPrompt(input *VisualInput) (string, error) {
	var buf bytes.Buffer
	insp := input.Inspection
	err := reviewVisualTmpl.Execute(&buf, struct {
		TargetURL       string
		Title           string
		MetaDescription string
		Headings        []types.Heading
		ImageURLs       []string
		Contexts        []types.LoadedReplyContext
	}{
		TargetURL:       insp.URL,
		Title:           insp.Title,
		MetaDescription: insp.MetaDescription,
		Headings:        insp.Headings,
		ImageURLs:       input.ImageURLs,
		Contexts:        input.Contexts,
	})
	if err != nil {
		return "", fmt.Errorf("render visual prompt: %w", err)
	}
	return buf.String(), nil
}
