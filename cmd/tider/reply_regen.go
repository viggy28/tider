package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/reply"
)

var (
	replyRegenNote      string
	replyRegenRender    string
	replyRegenProvider  string
	replyRegenModel     string
	replyRegenMaxTokens int
)

var replyRegenCmd = &cobra.Command{
	Use:   "regen <session-id>",
	Short: "Regenerate reply drafts from a saved reply session",
	Long: `Regenerate reply drafts from a saved ` + "`tider reply`" + ` session,
guided by --note. Reuses the saved thread, mode, contexts, and original
drafts as the basis — no Reddit re-fetch, no context re-load.

  tider reply regen <session-id> --note="shorter, no Kova mention"
  tider reply regen 1abc23 --note="reply to Buffyferry's last comment"

The operator note has the highest product-level priority short of
truth/safety constraints. It overrides previous drafts, default style,
and default no-pitch behavior — if the note asks to mention a project
(e.g. Kova), the regen does so transparently.

Each run writes a fresh artifact under regens/<timestamp>.json and
appends a regen event to history.jsonl. The original drafts.json is
never overwritten.

Review-mode sessions are not yet supported.`,
	Args: cobra.ExactArgs(1),
	RunE: runReplyRegen,
}

func init() {
	replyRegenCmd.Flags().StringVar(&replyRegenNote, "note", "", "iteration guidance for the regenerated reply drafts (required)")
	replyRegenCmd.Flags().StringVar(&replyRegenRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
	replyRegenCmd.Flags().StringVar(&replyRegenProvider, "provider", "", "LLM provider for regen (default from config tasks.reply)")
	replyRegenCmd.Flags().StringVar(&replyRegenModel, "model", "", "LLM model for regen (default from config tasks.reply)")
	replyRegenCmd.Flags().IntVar(&replyRegenMaxTokens, "max-tokens", 0, "completion budget (default from config tasks.reply)")
	replyCmd.AddCommand(replyRegenCmd)
}

func runReplyRegen(cmd *cobra.Command, args []string) error {
	if strings.TrimSpace(replyRegenNote) == "" {
		return errors.New("--note is required")
	}
	sid := args[0]

	root, err := reply.SessionsRoot()
	if err != nil {
		return err
	}
	sess, err := reply.ResolveSession(root, sid)
	if err != nil {
		return err
	}

	// Required-files check. Failed sessions (no drafts.json) and
	// partially-fetched sessions (no thread.json) can't be regenerated
	// — the prompt has no basis. Fail with a pointer to which file is
	// missing so the user knows whether to re-run `tider reply` or
	// pick a different session.
	for _, name := range []string{"thread.json", "mode.json", "draft-input.json", "drafts.json"} {
		if !sess.HasFile(name) {
			return fmt.Errorf("session %s is missing %s — required for regen (run `tider reply` first to produce a complete session)", filepath.Base(sess.Path()), name)
		}
	}

	var thread types.Thread
	if err := sess.LoadJSON("thread.json", &thread); err != nil {
		return err
	}
	var mode types.ReplyModeResult
	if err := sess.LoadJSON("mode.json", &mode); err != nil {
		return err
	}
	if mode.Mode == types.ReplyModeReview {
		return errors.New("reply regen for review-mode sessions is not implemented yet")
	}

	// draft-input.json is the source of truth for what the original
	// drafter was given. Prefer it over re-loading contexts.json so
	// regen sees exactly the same context bodies the original run did,
	// even if the bank file has since been edited on disk.
	var origInput reply.DraftInput
	if err := sess.LoadJSON("draft-input.json", &origInput); err != nil {
		return fmt.Errorf("read draft-input.json: %w", err)
	}

	var origBundle types.ReplyBundle
	if err := sess.LoadJSON("drafts.json", &origBundle); err != nil {
		return fmt.Errorf("read drafts.json: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	provider, model, maxTokens := resolveProviderModel(cfg, "reply", replyRegenProvider, replyRegenModel, replyRegenMaxTokens)
	p, err := llm.New(llm.Config{Provider: provider, Model: model})
	if err != nil {
		return fmt.Errorf("regen provider: %w", err)
	}

	regenInput := &reply.RegenInput{
		Thread:         &thread,
		Mode:           &mode,
		Contexts:       origInput.Contexts,
		AuthorContext:  origInput.AuthorContext,
		PreviousDrafts: origBundle.Drafts,
		Note:           replyRegenNote,
	}

	bundle, err := reply.GenerateReplyRegen(context.Background(), p, model, regenInput, maxTokens)
	if err != nil {
		return err
	}

	regen := &types.ReplyRegen{
		SessionID:        filepath.Base(sess.Path()),
		Generated:        bundle.Generated,
		Note:             strings.TrimSpace(replyRegenNote),
		SourceDraftsPath: "drafts.json",
		Bundle:           bundle,
	}
	regenPath, err := sess.SaveRegen(regen)
	if err != nil {
		return err
	}
	if err := sess.AppendHistoryEvent(types.HistoryEvent{
		Type:      "regen",
		Generated: bundle.Generated,
		Note:      regen.Note,
		Path:      regenPath,
	}); err != nil {
		return err
	}

	// Render. Surface the regen artifact path on stderr so the user can
	// find the iteration without grepping the directory.
	fmt.Fprintf(os.Stderr, "regen: %s\n", filepath.Join(sess.Path(), regenPath))

	switch resolveRender(replyRegenRender) {
	case "markdown":
		md := reply.RenderMarkdown(bundle, thread.Title, sess.Path())
		if isTerminal(os.Stdout) {
			md = renderTerminal(md)
		}
		fmt.Print(md)
	case "json":
		out, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	default:
		return fmt.Errorf("unknown --render value: %s (use json or markdown)", replyRegenRender)
	}
	return nil
}
