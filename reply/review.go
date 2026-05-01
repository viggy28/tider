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

var (
	reviewNotesTmpl = template.Must(template.ParseFS(prompts.FS, "review_notes.tmpl"))
	reviewTmpl      = template.Must(template.ParseFS(prompts.FS, "review.tmpl"))
)

// DefaultReviewNotesMaxTokens — the notes step is bounded JSON output;
// reasoning models still fit within this.
const DefaultReviewNotesMaxTokens = 4096

// BuildReviewNotes asks the LLM to turn an Inspection into structured
// review notes (strengths/weaknesses/suggestions/open-questions). The
// notes are the evidence base the review drafter draws from — keeping
// the drafting prompt focused on writing rather than judgment.
func BuildReviewNotes(ctx context.Context, p llm.Provider, model string, inspection *types.Inspection, maxTokens int) (*types.ReviewNotes, error) {
	if inspection == nil {
		return nil, fmt.Errorf("review notes: nil inspection")
	}
	if maxTokens <= 0 {
		maxTokens = DefaultReviewNotesMaxTokens
	}
	prompt, err := renderReviewNotesPrompt(inspection)
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
		return nil, fmt.Errorf("review notes: %w", err)
	}
	var raw struct {
		Strengths     []string `json:"strengths"`
		Weaknesses    []string `json:"weaknesses"`
		Suggestions   []string `json:"suggestions"`
		OpenQuestions []string `json:"open_questions"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("review notes: parse json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	return &types.ReviewNotes{
		TargetURL:     inspection.URL,
		Strengths:     raw.Strengths,
		Weaknesses:    raw.Weaknesses,
		Suggestions:   raw.Suggestions,
		OpenQuestions: raw.OpenQuestions,
		Generated:     time.Now().UTC(),
	}, nil
}

// ReviewDraftInput collects everything the review drafter needs. The
// review prompt is tightly scoped to the notes (the LLM's evidence base)
// — comments are NOT included in review-mode prompts because the user
// isn't replying to the conversation, they're reviewing the OP's
// resource.
//
// VisualNotes is populated when review mode ran the visual analyzer
// (FIRECRAWL_API_KEY + vision-capable model). It carries screenshot/
// product-image observations that the drafter prompt cites alongside
// the text-derived Notes. May be nil for threads where visual analysis
// genuinely produced nothing (rare; review mode requires a screenshot
// per SPEC_REVIEW_VISUAL_FIRECRAWL.md).
type ReviewDraftInput struct {
	Thread        *types.Thread
	Mode          *types.ReplyModeResult
	Notes         *types.ReviewNotes
	VisualNotes   *types.VisualReviewNotes
	Contexts      []types.LoadedReplyContext
	AuthorContext string
}

// GenerateReviewReply produces a ReplyBundle of 3-4 review-style variants
// grounded in the inspection-derived notes. Single LLM call, single
// provider — same shape as GenerateReply for reply mode.
func GenerateReviewReply(ctx context.Context, p llm.Provider, model string, input *ReviewDraftInput, maxTokens int) (*types.ReplyBundle, error) {
	if input == nil || input.Thread == nil || input.Notes == nil {
		return nil, fmt.Errorf("review drafter: nil input/thread/notes")
	}
	if maxTokens <= 0 {
		maxTokens = DefaultReplyMaxTokens
	}
	prompt, err := renderReviewPrompt(input)
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
		return nil, fmt.Errorf("review drafter: %w", err)
	}
	var raw struct {
		Drafts []types.ReplyDraft `json:"drafts"`
		PickID string             `json:"pick_id"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("review drafter: parse json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	if len(raw.Drafts) == 0 {
		return nil, fmt.Errorf("review drafter: no drafts returned (model output: %q)", truncate(resp.Content, 200))
	}
	pickID := raw.PickID
	if pickID == "" {
		pickID = raw.Drafts[0].ID
	}
	return &types.ReplyBundle{
		ThreadURL: input.Thread.URL,
		Subreddit: input.Thread.Subreddit,
		Mode:      types.ReplyModeReview,
		Drafts:    raw.Drafts,
		PickID:    pickID,
		Generated: time.Now().UTC(),
	}, nil
}

// RenderReviewNotesPrompt and RenderReviewPrompt are exported so callers
// can inspect prompts without burning the LLM call (useful for future
// --dry-run support).

func RenderReviewNotesPrompt(inspection *types.Inspection) (string, error) {
	return renderReviewNotesPrompt(inspection)
}

func RenderReviewPrompt(input *ReviewDraftInput) (string, error) {
	return renderReviewPrompt(input)
}

func renderReviewNotesPrompt(inspection *types.Inspection) (string, error) {
	var buf bytes.Buffer
	err := reviewNotesTmpl.Execute(&buf, inspection)
	if err != nil {
		return "", fmt.Errorf("render review-notes prompt: %w", err)
	}
	return buf.String(), nil
}

func renderReviewPrompt(input *ReviewDraftInput) (string, error) {
	var buf bytes.Buffer
	err := reviewTmpl.Execute(&buf, struct {
		Subreddit     string
		Title         string
		Body          string
		TargetURL     string
		Strengths     []string
		Weaknesses    []string
		Suggestions   []string
		OpenQuestions []string
		VisualNotes   *types.VisualReviewNotes
		AuthorContext string
		Contexts      []types.LoadedReplyContext
	}{
		Subreddit:     input.Thread.Subreddit,
		Title:         input.Thread.Title,
		Body:          input.Thread.Body,
		TargetURL:     input.Notes.TargetURL,
		Strengths:     input.Notes.Strengths,
		Weaknesses:    input.Notes.Weaknesses,
		Suggestions:   input.Notes.Suggestions,
		OpenQuestions: input.Notes.OpenQuestions,
		VisualNotes:   input.VisualNotes,
		AuthorContext: input.AuthorContext,
		Contexts:      input.Contexts,
	})
	if err != nil {
		return "", fmt.Errorf("render review prompt: %w", err)
	}
	return buf.String(), nil
}
