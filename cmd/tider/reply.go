package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	replyURL          string
	replyContexts     []string
	replyRender       string
	replyProvider     string
	replyModel        string
	replyMaxTokens    int
	replyModeOverride string // --mode=reply forces reply mode (skips OP-only classifier)
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

		// 1. Parse URL. Canonicalize first so Reddit mobile share links
		//    (https://www.reddit.com/s/<token>, common when pasting from
		//    the Reddit app's Share menu) get resolved to a canonical
		//    /r/<sub>/comments/<id>/... URL before parsing.
		ctx := context.Background()
		canonURL, err := reddit.Canonicalize(ctx, &http.Client{Timeout: 15 * time.Second}, replyURL)
		if err != nil {
			return fmt.Errorf("resolve url: %w", err)
		}
		sub, postID, err := reddit.ParseThreadURL(canonURL)
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

		// 7. Mode resolution. Either:
		//    a) honor --mode=reply (recoverable escape hatch — see
		//       SPEC_REVIEW_VISUAL_FIRECRAWL.md "CLI behavior") and skip
		//       the classifier entirely; or
		//    b) run the cheaper classifier from tasks.reply_mode.
		//    --provider propagates so `--provider=anthropic` doesn't
		//    silently still require OPENAI_API_KEY for the classifier.
		var modeResult *types.ReplyModeResult
		if replyModeOverride != "" {
			switch replyModeOverride {
			case "reply":
				modeResult = &types.ReplyModeResult{
					Mode:   types.ReplyModeReply,
					Reason: "forced by --mode=reply (classifier skipped)",
				}
			default:
				return fmt.Errorf("unsupported --mode=%q (only 'reply' is supported in v1)", replyModeOverride)
			}
		} else {
			modeProvider, modeModel, modeMaxTokens := resolveProviderModel(cfg, "reply_mode", replyProvider, "", 0)
			modeP, err := llm.New(llm.Config{Provider: modeProvider, Model: modeModel})
			if err != nil {
				return fmt.Errorf("mode classifier provider: %w", err)
			}
			modeResult, err = reply.DetectMode(ctx, modeP, modeModel, thread, modeMaxTokens)
			if err != nil {
				return err
			}
		}
		if err := sess.SaveMode(modeResult); err != nil {
			return err
		}

		// 8. Drafter provider — needed for both reply and review pipelines.
		draftProvider, draftModel, draftMaxTokens := resolveProviderModel(cfg, "reply", replyProvider, replyModel, replyMaxTokens)
		draftP, err := llm.New(llm.Config{Provider: draftProvider, Model: draftModel})
		if err != nil {
			return fmt.Errorf("drafter provider: %w", err)
		}

		// 9. Branch on mode.
		var bundle *types.ReplyBundle
		switch modeResult.Mode {
		case types.ReplyModeReply:
			input := &reply.DraftInput{
				Thread:        thread,
				Mode:          modeResult,
				Contexts:      contexts,
				AuthorContext: cfg.AuthorContext,
			}
			// Snapshot the assembled input so the user can debug "why did
			// the drafts come out this way?" without re-running.
			if err := sess.WriteJSON("draft-input.json", input); err != nil {
				return err
			}
			bundle, err = reply.GenerateReply(ctx, draftP, draftModel, input, draftMaxTokens)
			if err != nil {
				return err
			}

		case types.ReplyModeReview:
			if len(modeResult.TargetURLs) == 0 {
				return errors.New("review request detected, but no shop/site URL was found in the original post — pass --context with notes you want to base the reply on, rerun with --mode=reply for an ordinary reply, or wait for a thread that includes a target link")
			}
			// Save target.json before any potentially-failing step so the
			// session preserves what we knew even if inspection fails.
			if err := sess.WriteJSON("target.json", map[string]any{
				"url":          modeResult.TargetURLs[0],
				"alternatives": modeResult.TargetURLs[1:],
				"reason":       modeResult.Reason,
			}); err != nil {
				return err
			}

			// Visual review pipeline (SPEC_REVIEW_VISUAL_FIRECRAWL.md):
			//   1. Firecrawl required → InspectReviewTarget. Errors are
			//      explicit and recoverable via --mode=reply.
			//   2. Screenshot is mandatory — fail before drafting if it
			//      can't be persisted (no degraded text-only review).
			//   3. Visual analyzer (vision-capable model) consumes the
			//      screenshot + selected product images.
			//   4. Text-only review notes still run alongside; both flow
			//      into the drafter.
			httpClient := &http.Client{Timeout: 60 * time.Second}
			inspection, err := reply.InspectReviewTarget(ctx, httpClient, modeResult.TargetURLs[0])
			if err != nil {
				return fmt.Errorf("inspection: %w (session preserved at %s; rerun with --mode=reply to draft an ordinary reply)", err, sess.Path())
			}

			screenshotDir := filepath.Join(sess.Path(), "screenshots")
			localPath, err := reply.DownloadScreenshot(ctx, httpClient, inspection.ScreenshotURL, screenshotDir)
			if err != nil {
				return fmt.Errorf("review mode could not persist Firecrawl screenshot: %w (session preserved at %s)", err, sess.Path())
			}
			inspection.ScreenshotPath = localPath

			if err := sess.WriteJSON("inspection.json", inspection); err != nil {
				return err
			}

			// Visual analyzer. Resolve review_visual via config; OpenAI-
			// only in v1 per spec. Gate with llm.SupportsVision so the
			// failure mode is "your configured model is not vision-
			// capable" rather than a provider-level cryptic error.
			visualProvider, visualModel, visualMaxTokens := resolveProviderModel(cfg, "review_visual", replyProvider, "", 0)
			if !llm.SupportsVision(visualProvider, visualModel) {
				return fmt.Errorf("review mode requires a vision-capable model for tasks.review_visual; %s/%s is not on the supported list (try gpt-4o); session preserved at %s", visualProvider, visualModel, sess.Path())
			}
			visualP, err := llm.New(llm.Config{Provider: visualProvider, Model: visualModel})
			if err != nil {
				return fmt.Errorf("visual analyzer provider: %w", err)
			}
			selectedImages := reply.SelectImagesForAnalysis(inspection.ImageURLs, inspection.ScreenshotURL)
			visualInput := &reply.VisualInput{
				Inspection: inspection,
				Contexts:   contexts,
				ImageURLs:  selectedImages,
			}
			// Persist visual-input.json BEFORE the LLM call so a failed
			// analysis still leaves the input footprint for debugging.
			contextIDs := make([]string, 0, len(contexts))
			for _, c := range contexts {
				if c.ID != "" {
					contextIDs = append(contextIDs, c.ID)
				}
			}
			imageRefs := make([]types.VisualImageRef, 0, len(selectedImages))
			for _, u := range selectedImages {
				imageRefs = append(imageRefs, types.VisualImageRef{URL: u, Reason: "product image candidate"})
			}
			if err := sess.WriteJSON("visual-input.json", types.VisualInputRecord{
				TargetURL:           inspection.URL,
				ScreenshotPath:      inspection.ScreenshotPath,
				ScreenshotSourceURL: inspection.ScreenshotURL,
				ImageRefs:           imageRefs,
				PageTitle:           inspection.Title,
				ContextIDs:          contextIDs,
				Generated:           time.Now().UTC(),
			}); err != nil {
				return err
			}

			visualNotes, err := reply.AnalyzeVisual(ctx, visualP, visualModel, visualInput, visualMaxTokens)
			if err != nil {
				return fmt.Errorf("visual analysis: %w (session preserved at %s)", err, sess.Path())
			}
			if err := sess.WriteJSON("visual-notes.json", visualNotes); err != nil {
				return err
			}

			// Text-only review notes — still useful alongside visual
			// because they cover headings/copy/meta which aren't really
			// "visual" anyway. Uses the cheap classifier model.
			notesProvider, notesModel, notesMaxTokens := resolveProviderModel(cfg, "reply_mode", replyProvider, "", 0)
			notesP, err := llm.New(llm.Config{Provider: notesProvider, Model: notesModel})
			if err != nil {
				return fmt.Errorf("review-notes provider: %w", err)
			}
			notes, err := reply.BuildReviewNotes(ctx, notesP, notesModel, inspection, notesMaxTokens)
			if err != nil {
				return err
			}
			if err := sess.WriteJSON("review-notes.json", notes); err != nil {
				return err
			}

			// Snapshot review-mode drafter input — both text and visual.
			input := &reply.ReviewDraftInput{
				Thread:        thread,
				Mode:          modeResult,
				Notes:         notes,
				VisualNotes:   visualNotes,
				Contexts:      contexts,
				AuthorContext: cfg.AuthorContext,
			}
			if err := sess.WriteJSON("draft-input.json", input); err != nil {
				return err
			}
			bundle, err = reply.GenerateReviewReply(ctx, draftP, draftModel, input, draftMaxTokens)
			if err != nil {
				return err
			}
			// Populate the InspectionSummary so the renderer shows what
			// was inspected.
			bundle.Inspection = &types.InspectionSummary{
				Source:         inspection.Source,
				ScreenshotPath: inspection.ScreenshotPath,
				ImagesAnalyzed: len(selectedImages),
				ShopType:       visualNotes.ShopType,
				Limitations:    visualNotes.Limitations,
			}

		default:
			return fmt.Errorf("unexpected mode: %s", modeResult.Mode)
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

// resolveProviderModel produces the (provider, model, maxTokens) triple
// for a given task, honoring CLI overrides.
//
// Plain ForTask handles per-task config defaults but doesn't know about
// the CLI's --provider flag — so a task whose default model belongs to
// the other provider (e.g. tasks.reply_mode defaults to gpt-4o-mini)
// would otherwise get sent to the wrong provider when the user passes
// --provider=anthropic. This helper swaps in the per-provider default
// model whenever --provider changes the provider, then layers --model
// and --max-tokens on top.
//
// modelOverride / maxTokensOverride may be empty/zero — only --provider
// is honored across all three reply pipeline calls (mode, drafter,
// review-notes); the drafter call is the only one that exposes --model
// and --max-tokens to the user.
func resolveProviderModel(cfg *config.Config, task, providerOverride, modelOverride string, maxTokensOverride int) (provider, model string, maxTokens int) {
	provider, model, maxTokens = cfg.ForTask(task)
	if providerOverride != "" && providerOverride != provider {
		provider = providerOverride
		// Provider changed — task's default model is for the wrong
		// provider. Fall back to the per-provider default in config.
		switch providerOverride {
		case "anthropic":
			if cfg.LLM.AnthropicModel != "" {
				model = cfg.LLM.AnthropicModel
			}
		case "openai":
			if cfg.LLM.OpenAIModel != "" {
				model = cfg.LLM.OpenAIModel
			}
		}
	}
	if modelOverride != "" {
		model = modelOverride
	}
	if maxTokensOverride > 0 {
		maxTokens = maxTokensOverride
	}
	return
}

func init() {
	replyCmd.Flags().StringVar(&replyURL, "url", "", "URL of the Reddit thread to draft a reply for")
	replyCmd.Flags().StringSliceVar(&replyContexts, "context", nil, "context-bank id or file path (repeatable)")
	replyCmd.Flags().StringVar(&replyRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
	replyCmd.Flags().StringVar(&replyProvider, "provider", "", "LLM provider for every call in this command (default from config tasks.<task>)")
	replyCmd.Flags().StringVar(&replyModel, "model", "", "LLM model for the drafter call (default from config tasks.reply)")
	replyCmd.Flags().IntVar(&replyMaxTokens, "max-tokens", 0, "drafter completion budget (default from config tasks.reply)")
	replyCmd.Flags().StringVar(&replyModeOverride, "mode", "", "force a mode, skipping the OP-only classifier. Currently supports 'reply' (use when the classifier mistakes a discussion for a review request, or when FIRECRAWL_API_KEY isn't set and you want to draft a normal reply anyway)")
}
