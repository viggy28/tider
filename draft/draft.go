// Package draft turns a Brief + Research into per-provider Drafts so the
// user can compare framings side-by-side before posting.
package draft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"text/template"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var draftTmpl = template.Must(template.ParseFS(prompts.FS, "draft.tmpl"))

// Options control variant counts and the LLM token budget.
type Options struct {
	AngleCount     int
	TitlesPerAngle int
	BodiesPerAngle int
	MaxTokens      int
	// AuthorContext is the user's voice/background — what they've built,
	// what experiences inform their take, what tone they want drafts in.
	// Empty means generic developer-tone; loaded from ~/.tider/config.yaml's
	// author_context field. Flows into the draft prompt so generated copy
	// references real lived experience.
	AuthorContext string
}

// Input bundles everything one Generate call needs. Contexts and
// OperatorNote are new in the post one-step UX: contexts carry
// background project material loaded via --context, OperatorNote carries
// the operator's intent passed via --note or stdin (treated as
// instruction, not source material). Both are optional — a brief
// extracted from --file or --url with no contexts and no note still
// renders cleanly.
type Input struct {
	Brief        types.Brief
	Research     types.Research
	Contexts     []types.LoadedReplyContext
	OperatorNote string
	Opts         Options
}

// Default is the spec's "2 angles × 3 titles × 2 bodies" — ~12 artifacts.
// MaxTokens sized for reasoning models (gpt-5, o-series): real-run usage
// has been ~5K output tokens, so 10K leaves headroom for reasoning.
func Default() Options {
	return Options{
		AngleCount:     2,
		TitlesPerAngle: 3,
		BodiesPerAngle: 2,
		MaxTokens:      10000,
	}
}

// Full is the wider "3 × 5 × 3" spread — ~33 artifacts. Bump max tokens
// proportionally.
func Full() Options {
	return Options{
		AngleCount:     3,
		TitlesPerAngle: 5,
		BodiesPerAngle: 3,
		MaxTokens:      16384,
	}
}

// ProviderRef is an alias of llm.ProviderRef kept for back-compat with
// callers that already import draft.ProviderRef. New code should use
// llm.ProviderRef directly so draft and regen share a type.
type ProviderRef = llm.ProviderRef

// RenderPrompt produces the user-facing prompt the LLM will see. Exposed
// so `--dry-run` can print it without burning a real API call.
func RenderPrompt(in Input) (string, error) {
	if in.Opts.AngleCount <= 0 || in.Opts.TitlesPerAngle <= 0 || in.Opts.BodiesPerAngle <= 0 {
		return "", fmt.Errorf("draft: variant counts must be > 0")
	}
	var buf bytes.Buffer
	err := draftTmpl.Execute(&buf, struct {
		SubName        string
		Subscribers    int
		CuratedNotes   *types.SubNotes
		Rules          []types.Rule
		TopWeek        []types.Post
		TopMonth       []types.Post
		Hot            []types.Post
		Stickies       []types.Post
		Flairs         []types.Flair
		Brief          types.Brief
		Contexts       []types.LoadedReplyContext
		OperatorNote   string
		AngleCount     int
		TitlesPerAngle int
		BodiesPerAngle int
		AuthorContext  string
	}{
		SubName:        in.Research.Sub.Name,
		Subscribers:    in.Research.Sub.Subscribers,
		CuratedNotes:   in.Research.Notes,
		Rules:          in.Research.Rules,
		TopWeek:        in.Research.TopWeek,
		TopMonth:       in.Research.TopMonth,
		Hot:            in.Research.Hot,
		Stickies:       in.Research.Stickies,
		Flairs:         in.Research.Flairs,
		Brief:          in.Brief,
		Contexts:       in.Contexts,
		OperatorNote:   in.OperatorNote,
		AngleCount:     in.Opts.AngleCount,
		TitlesPerAngle: in.Opts.TitlesPerAngle,
		BodiesPerAngle: in.Opts.BodiesPerAngle,
		AuthorContext:  in.Opts.AuthorContext,
	})
	if err != nil {
		return "", fmt.Errorf("render draft prompt: %w", err)
	}
	return buf.String(), nil
}

// Generate fans out across providers concurrently and returns the bundle.
// Per-provider errors are recorded on the Draft (not propagated) so a
// transient failure in one provider doesn't kill the whole comparison.
func Generate(ctx context.Context, refs []ProviderRef, in Input) (*types.DraftBundle, error) {
	if len(refs) == 0 {
		return nil, errors.New("draft: at least one provider required")
	}
	prompt, err := RenderPrompt(in)
	if err != nil {
		return nil, err
	}

	drafts := make([]types.Draft, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func(i int, ref ProviderRef) {
			defer wg.Done()
			drafts[i] = generateOne(ctx, ref, in.Research.Sub.Name, prompt, in.Opts.MaxTokens)
		}(i, ref)
	}
	wg.Wait()

	return &types.DraftBundle{
		Sub:       in.Research.Sub.Name,
		Brief:     in.Brief,
		Drafts:    drafts,
		Generated: time.Now().UTC(),
	}, nil
}

func generateOne(ctx context.Context, ref ProviderRef, sub, prompt string, maxTokens int) types.Draft {
	d := types.Draft{
		Sub:       sub,
		Provider:  ref.Provider.Name(),
		Model:     ref.Model,
		Generated: time.Now().UTC(),
	}
	resp, err := ref.Provider.Complete(ctx, llm.Request{
		Model:     ref.Model,
		MaxTokens: maxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.InputTokens = resp.InputTokens
	d.OutputTokens = resp.OutputTokens
	if err := parseInto(&d, resp.Content); err != nil {
		d.Error = err.Error()
	}
	return d
}

// parseInto unmarshals the LLM JSON into d, preserving Provider/Model/etc.
// fields already set on the struct. Strict on shape but tolerant on extras.
func parseInto(d *types.Draft, raw string) error {
	if raw == "" {
		return errors.New("empty response")
	}
	var payload struct {
		Risk                string               `json:"risk"`
		RiskReason          string               `json:"risk_reason"`
		Angles              []types.Angle        `json:"angles"`
		Recommendation      types.Recommendation `json:"recommendation"`
		Flair               types.FlairRec       `json:"flair"`
		SuggestedWindow     string               `json:"suggested_window"`
		MediaRecommendation string               `json:"media_recommendation"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return fmt.Errorf("parse draft json: %w (model returned: %q)", err, truncate(raw, 200))
	}
	if payload.Risk == "" {
		return errors.New("draft missing required field: risk")
	}
	d.Risk = payload.Risk
	d.RiskReason = payload.RiskReason
	d.Angles = payload.Angles
	d.Recommendation = payload.Recommendation
	d.Flair = payload.Flair
	d.SuggestedWindow = payload.SuggestedWindow
	d.MediaRecommendation = payload.MediaRecommendation
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
