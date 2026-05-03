package reply

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var replyRegenTmpl = template.Must(template.ParseFS(prompts.FS, "reply_regen.tmpl"))

// ErrRegenReviewModeUnsupported is returned by GenerateReplyRegen when
// the saved session was a review-mode run. Review mode has a different
// input shape (visual notes, inspection, review notes) that the regen
// pipeline doesn't speak yet — we fail fast with a clear message
// instead of silently producing a degraded draft.
//
// Sentinel rather than an inline error so the CLI layer can format the
// message ("reply regen for review-mode sessions is not implemented
// yet") consistently with the issue spec.
var ErrRegenReviewModeUnsupported = errors.New("reply regen for review-mode sessions is not implemented yet")

// RegenInput is the assembled regeneration request for the saved
// session. The session basis (Thread, Mode, Contexts, AuthorContext,
// PreviousDrafts) is loaded from the session directory; Note is the
// operator's --note from this run.
//
// The regen runs against the *original* drafts.json — not against any
// prior regens — so iterations stay anchored to the original basis
// and don't accumulate into a chain of "yes-and" conversations.
type RegenInput struct {
	Thread         *types.Thread
	Mode           *types.ReplyModeResult
	Contexts       []types.LoadedReplyContext
	AuthorContext  string
	PreviousDrafts []types.ReplyDraft
	Note           string
}

// GenerateReplyRegen produces a fresh ReplyBundle of three variants
// (best / shorter / question) anchored on the saved session plus the
// operator note. Single LLM call, single provider — same shape as
// GenerateReply, kept separate to keep the regen prompt dedicated and
// avoid overloading the initial reply prompt.
//
// Review-mode sessions are rejected with ErrRegenReviewModeUnsupported.
func GenerateReplyRegen(ctx context.Context, p llm.Provider, model string, input *RegenInput, maxTokens int) (*types.ReplyBundle, error) {
	if input == nil || input.Thread == nil {
		return nil, fmt.Errorf("reply regen: nil input or thread")
	}
	if strings.TrimSpace(input.Note) == "" {
		return nil, fmt.Errorf("reply regen: --note is required")
	}
	if input.Mode != nil && input.Mode.Mode == types.ReplyModeReview {
		return nil, ErrRegenReviewModeUnsupported
	}
	if maxTokens <= 0 {
		maxTokens = DefaultReplyMaxTokens
	}

	prompt, err := renderReplyRegenPrompt(input)
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
		return nil, fmt.Errorf("reply regen: %w", err)
	}

	var raw struct {
		Drafts []types.ReplyDraft `json:"drafts"`
		PickID string             `json:"pick_id"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("reply regen: parse json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	if len(raw.Drafts) == 0 {
		return nil, fmt.Errorf("reply regen: no drafts returned (model output: %q)", truncate(resp.Content, 200))
	}

	pickID := raw.PickID
	if pickID == "" {
		pickID = raw.Drafts[0].ID
	}

	return &types.ReplyBundle{
		ThreadURL: input.Thread.URL,
		Subreddit: input.Thread.Subreddit,
		Mode:      types.ReplyModeReply,
		Drafts:    raw.Drafts,
		PickID:    pickID,
		Generated: time.Now().UTC(),
	}, nil
}

// RenderRegenPrompt exposes the rendered prompt for inspection/tests
// without making the completion call. Mirrors RenderReplyPrompt for the
// initial drafter.
func RenderRegenPrompt(input *RegenInput) (string, error) {
	return renderReplyRegenPrompt(input)
}

func renderReplyRegenPrompt(input *RegenInput) (string, error) {
	var buf bytes.Buffer
	err := replyRegenTmpl.Execute(&buf, struct {
		Thread         *types.Thread
		AuthorContext  string
		Contexts       []types.LoadedReplyContext
		PreviousDrafts []types.ReplyDraft
		Note           string
	}{
		Thread:         input.Thread,
		AuthorContext:  input.AuthorContext,
		Contexts:       input.Contexts,
		PreviousDrafts: input.PreviousDrafts,
		Note:           strings.TrimSpace(input.Note),
	})
	if err != nil {
		return "", fmt.Errorf("render regen prompt: %w", err)
	}
	return buf.String(), nil
}
