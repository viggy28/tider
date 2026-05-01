package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/contextbank"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/reddit"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/reply"
)

var (
	replyURL       string
	replyContexts  []string
	replyRender    string
	replyProvider  string
	replyModel     string
	replyMaxTokens int
)

var replyCmd = &cobra.Command{
	Use:   "reply",
	Short: "Draft a reply to an existing Reddit thread",
	Long: `reply fetches a Reddit thread (read-only), classifies it as a normal
discussion or a review request based on the original post only, and
drafts 3-4 reply variants you can review and post manually.

  tider reply --url=https://www.reddit.com/r/shopify/comments/abc/...
  tider reply --url=... --context=kova
  tider reply --url=... --context=kova --context=./profile.md

Each run creates a session at ~/.tider/sessions/replies/<date>-<sub>-<id>/
with the fetched thread, loaded contexts, mode-detection result, and
drafts. The session path prints to stderr at the start.

API key for the chosen provider must be set in the environment
(ANTHROPIC_API_KEY or OPENAI_API_KEY).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if replyURL == "" {
			return errors.New("--url is required")
		}

		// 1. Parse URL.
		sub, postID, err := reddit.ParseThreadURL(replyURL)
		if err != nil {
			return fmt.Errorf("parse url: %w", err)
		}

		// 2. Fetch thread (no caching — comments are state we want fresh).
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		cacheRoot := filepath.Join(home, ".tider", "cache")
		client := reddit.NewClient(reddit.NewCache(cacheRoot))
		ctx := context.Background()
		thread, err := client.FetchThread(ctx, sub, postID)
		if err != nil {
			return err
		}

		// 3. Create session — uses subreddit from the response (so even
		//    a redd.it short-link parse with empty sub gets the right slug).
		sessionsRoot, err := reply.SessionsRoot()
		if err != nil {
			return err
		}
		sess, err := reply.NewSession(sessionsRoot, thread.Subreddit, thread.PostID, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "session: %s\n", sess.Path())

		// 4. Save thread immediately. Subsequent steps may fail; this
		//    artifact is preserved regardless.
		if err := sess.SaveThread(thread); err != nil {
			return err
		}

		// 5. Load contexts (optional). Snapshot bodies into the session
		//    so the run is reproducible against the saved JSON later.
		bankDir, err := contextbank.DefaultDir()
		if err != nil {
			return err
		}
		contexts, err := reply.LoadContexts(bankDir, replyContexts)
		if err != nil {
			return err
		}
		if err := sess.SaveContexts(contexts); err != nil {
			return err
		}

		// 6. Load config. Mode classifier + drafter pull defaults from here.
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		// 7. Mode classifier — uses the cheaper model from tasks.reply_mode.
		modeProvider, modeModel, modeMaxTokens := cfg.ForTask("reply_mode")
		modeP, err := llm.New(llm.Config{Provider: modeProvider, Model: modeModel})
		if err != nil {
			return fmt.Errorf("mode classifier provider: %w", err)
		}
		modeResult, err := reply.DetectMode(ctx, modeP, modeModel, thread, modeMaxTokens)
		if err != nil {
			return err
		}
		if err := sess.SaveMode(modeResult); err != nil {
			return err
		}

		// 8. Branch on mode.
		switch modeResult.Mode {
		case types.ReplyModeReview:
			if len(modeResult.TargetURLs) == 0 {
				return errors.New("review request detected, but no shop/site URL was found in the original post — pass --context with notes you want to base the reply on, or wait for a thread that includes a target link")
			}
			// Persist target.json so a future commit can pick this up
			// without re-classifying.
			if err := sess.WriteJSON("target.json", map[string]any{
				"url":          modeResult.TargetURLs[0],
				"alternatives": modeResult.TargetURLs[1:],
				"reason":       modeResult.Reason,
			}); err != nil {
				return err
			}
			return errors.New("review mode detected and target saved, but site inspection is not implemented yet — review-mode drafting will land in the next commit on this branch")
		case types.ReplyModeReply:
			// Continue to drafter below.
		default:
			return fmt.Errorf("unexpected mode: %s", modeResult.Mode)
		}

		// 9. Drafter — uses tasks.reply (typically the strong model).
		draftProvider, draftModel, draftMaxTokens := cfg.ForTask("reply")
		if replyProvider != "" {
			draftProvider = replyProvider
		}
		if replyModel != "" {
			draftModel = replyModel
		}
		if replyMaxTokens > 0 {
			draftMaxTokens = replyMaxTokens
		}
		draftP, err := llm.New(llm.Config{Provider: draftProvider, Model: draftModel})
		if err != nil {
			return fmt.Errorf("drafter provider: %w", err)
		}
		bundle, err := reply.GenerateReply(ctx, draftP, draftModel, &reply.DraftInput{
			Thread:        thread,
			Mode:          modeResult,
			Contexts:      contexts,
			AuthorContext: cfg.AuthorContext,
		}, draftMaxTokens)
		if err != nil {
			return err
		}
		if err := sess.SaveDrafts(bundle); err != nil {
			return err
		}

		// 10. Render + persist + print.
		md := reply.RenderMarkdown(bundle, thread.Title, sess.Path())
		if err := sess.SaveOutput(md); err != nil {
			return err
		}

		switch resolveRender(replyRender) {
		case "markdown":
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
			return fmt.Errorf("unknown --render value: %s (use json or markdown)", replyRender)
		}
		return nil
	},
}

func init() {
	replyCmd.Flags().StringVar(&replyURL, "url", "", "URL of the Reddit thread to draft a reply for")
	replyCmd.Flags().StringSliceVar(&replyContexts, "context", nil, "context-bank id or file path (repeatable)")
	replyCmd.Flags().StringVar(&replyRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
	replyCmd.Flags().StringVar(&replyProvider, "provider", "", "LLM provider for the drafter call (default from config tasks.reply)")
	replyCmd.Flags().StringVar(&replyModel, "model", "", "LLM model for the drafter call (default from config tasks.reply)")
	replyCmd.Flags().IntVar(&replyMaxTokens, "max-tokens", 0, "drafter completion budget (default from config tasks.reply)")
}
