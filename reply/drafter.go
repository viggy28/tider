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

var replyTmpl = template.Must(template.ParseFS(prompts.FS, "reply.tmpl"))

// DefaultReplyMaxTokens is a reasonable budget for the drafter call.
// Reply variants are typically 50-500 words each, 3-4 of them, plus
// reasoning — fits well under 8k. Reasoning models that consume internal
// tokens have headroom too.
const DefaultReplyMaxTokens = 8192

// DraftInput collects everything the reply prompt needs:
//
//   - Thread: fetched OP + selected comments. Comments are passed as
//     context only — the user is replying to the OP, not the commenters.
//   - Mode: classifier output (reply or review). The drafter only handles
//     reply mode; review mode has its own pipeline (commit 8 in the
//     reply branch).
//   - Contexts: project material loaded from the bank or path refs.
//     Background only — naming/pitching the project requires explicit
//     permission in the context body.
//   - AuthorContext: the user's voice/background, from config. Voice
//     grounding only — must not introduce facts not in Thread or Contexts.
type DraftInput struct {
	Thread        *types.Thread
	Mode          *types.ReplyModeResult
	Contexts      []types.LoadedReplyContext
	AuthorContext string
}

// GenerateReply produces a ReplyBundle of 3-4 labeled variants from the
// LLM. Single LLM call, single provider — fan-out doesn't add value for
// reply drafting (focused thread + focused context = focused output).
func GenerateReply(ctx context.Context, p llm.Provider, model string, input *DraftInput, maxTokens int) (*types.ReplyBundle, error) {
	if input == nil || input.Thread == nil {
		return nil, fmt.Errorf("reply drafter: nil input or thread")
	}
	if maxTokens <= 0 {
		maxTokens = DefaultReplyMaxTokens
	}
	prompt, err := renderReplyPrompt(input)
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
		return nil, fmt.Errorf("reply drafter: %w", err)
	}

	var raw struct {
		Drafts []types.ReplyDraft `json:"drafts"`
		PickID string             `json:"pick_id"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &raw); err != nil {
		return nil, fmt.Errorf("reply drafter: parse json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	if len(raw.Drafts) == 0 {
		return nil, fmt.Errorf("reply drafter: no drafts returned (model output: %q)", truncate(resp.Content, 200))
	}

	// Default pick to first draft if model didn't supply one — better
	// than rendering "Best Pick" as empty.
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

// RenderReplyPrompt renders the prompt the LLM will see. Exposed so
// callers (or future --dry-run flag) can inspect it without making the
// completion call.
func RenderReplyPrompt(input *DraftInput) (string, error) {
	return renderReplyPrompt(input)
}

func renderReplyPrompt(input *DraftInput) (string, error) {
	var buf bytes.Buffer
	err := replyTmpl.Execute(&buf, struct {
		Subreddit     string
		Flair         string
		Title         string
		Body          string
		Comments      []types.Comment
		AuthorContext string
		Contexts      []types.LoadedReplyContext
	}{
		Subreddit:     input.Thread.Subreddit,
		Flair:         input.Thread.Flair,
		Title:         input.Thread.Title,
		Body:          input.Thread.Body,
		Comments:      input.Thread.Comments,
		AuthorContext: input.AuthorContext,
		Contexts:      input.Contexts,
	})
	if err != nil {
		return "", fmt.Errorf("render reply prompt: %w", err)
	}
	return buf.String(), nil
}
