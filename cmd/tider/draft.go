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
	"github.com/viggy28/tider/draft"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/reddit"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/lastdraft"
	"github.com/viggy28/tider/research"
)

var (
	draftBriefPath  string
	draftSub        string
	draftProviders  string
	draftAnthropic  string
	draftOpenAI     string
	draftRender     string
	draftDryRun     bool
	draftRefresh    bool
	draftNotesPath  string
	draftCacheRoot  string
	draftMaxTokens  int
	draftVariantSet string
)

var draftCmd = &cobra.Command{
	Use:   "draft",
	Short: "Generate drafts (fan-out across providers) for a Brief on one subreddit",
	Long: `draft turns a Brief + per-sub research into structured Drafts. By default
it fans out across both Anthropic and OpenAI concurrently so you can
compare framings side-by-side.

  tider draft --brief=brief.json --sub=golang
  tider draft --brief=brief.json --sub=PostgreSQL --providers=anthropic
  tider draft --brief=brief.json --sub=golang --render=markdown
  tider draft --brief=brief.json --sub=golang --dry-run    # show prompt only

API keys come from env vars: ANTHROPIC_API_KEY, OPENAI_API_KEY. Providers
without a key are skipped with a warning rather than failing the run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if draftBriefPath == "" || draftSub == "" {
			return errors.New("--brief and --sub are required")
		}
		brief, err := loadBrief(draftBriefPath)
		if err != nil {
			return err
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		cacheRoot := draftCacheRoot
		if cacheRoot == "" {
			cacheRoot = filepath.Join(home, ".tider", "cache")
		}
		notesPath := draftNotesPath
		if notesPath == "" {
			notesPath = filepath.Join(home, ".tider", "subreddits.yaml")
		}

		notes, err := research.LoadNotes(notesPath)
		if err != nil {
			return err
		}
		client := reddit.NewClient(reddit.NewCache(cacheRoot))
		ctx := context.Background()
		researchBundle, err := research.For(ctx, client, notes, draftSub, draftRefresh)
		if err != nil {
			return fmt.Errorf("research %s: %w", draftSub, err)
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_, _, cfgMaxTokens := cfg.FanOutModels("draft")

		opts := draft.Default()
		if draftVariantSet == "full" {
			opts = draft.Full()
		}
		// Resolve max_tokens: explicit flag wins, then config, then opts default.
		if draftMaxTokens > 0 {
			opts.MaxTokens = draftMaxTokens
		} else if cfgMaxTokens > 0 {
			opts.MaxTokens = cfgMaxTokens
		}
		opts.AuthorContext = cfg.AuthorContext

		if draftDryRun {
			prompt, err := draft.RenderPrompt(*brief, *researchBundle, opts)
			if err != nil {
				return err
			}
			fmt.Println(prompt)
			return nil
		}

		// Resolve fan-out provider list and per-provider models with config fallback.
		providers := draftProviders
		if providers == "" {
			providers = cfg.Defaults.Providers
		}
		anthropicModel := draftAnthropic
		openaiModel := draftOpenAI
		if anthropicModel == "" {
			anthropicModel = cfg.LLM.AnthropicModel
		}
		if openaiModel == "" {
			openaiModel = cfg.LLM.OpenAIModel
		}
		refs, err := buildProviderRefs(providers, anthropicModel, openaiModel)
		if err != nil {
			return err
		}
		if len(refs) == 0 {
			return errors.New("no usable providers — set ANTHROPIC_API_KEY and/or OPENAI_API_KEY")
		}

		bundle, err := draft.Generate(ctx, refs, *brief, *researchBundle, opts)
		if err != nil {
			return err
		}

		// Persist snapshot so `tider regen` can pick up where we left off.
		// A failure here shouldn't block the user from seeing the bundle —
		// log to stderr and proceed.
		if root, derr := lastdraft.Default(); derr == nil {
			snap := &types.Snapshot{Brief: *brief, Research: *researchBundle, Bundle: *bundle}
			if serr := lastdraft.Save(root, draftSub, snap); serr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save last-draft snapshot: %v\n", serr)
			}
		}

		switch resolveRender(draftRender) {
		case "markdown":
			md := draft.RenderMarkdown(bundle)
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
			return fmt.Errorf("unknown --render value: %s (use json or markdown)", draftRender)
		}
		return nil
	},
}

func loadBrief(path string) (*types.Brief, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read brief: %w", err)
	}
	var b types.Brief
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse brief: %w", err)
	}
	if b.Title == "" {
		return nil, errors.New("brief has no title — is this a real Brief JSON?")
	}
	return &b, nil
}

// buildProviderRefs constructs the list of providers to fan out across.
// providersFlag is comma-separated ("anthropic,openai"). Each requested
// provider needs its API key in the environment; missing keys cause that
// provider to be silently skipped (with a stderr warning) rather than
// failing the whole run, so a working provider isn't blocked by a missing
// key for the other.
func buildProviderRefs(providersFlag, anthropicModel, openaiModel string) ([]draft.ProviderRef, error) {
	want := strings.Split(providersFlag, ",")
	var refs []draft.ProviderRef
	for _, name := range want {
		name = strings.TrimSpace(name)
		switch name {
		case "anthropic":
			p, err := llm.NewAnthropic(anthropicModel)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skipping anthropic: %v\n", err)
				continue
			}
			refs = append(refs, draft.ProviderRef{Provider: p, Model: anthropicModel})
		case "openai":
			p, err := llm.NewOpenAI(openaiModel)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skipping openai: %v\n", err)
				continue
			}
			refs = append(refs, draft.ProviderRef{Provider: p, Model: openaiModel})
		case "":
			// trailing comma; ignore
		default:
			return nil, fmt.Errorf("unknown provider %q (supported: anthropic, openai)", name)
		}
	}
	return refs, nil
}

func init() {
	draftCmd.Flags().StringVar(&draftBriefPath, "brief", "", "path to a brief.json (output of `tider intake`)")
	draftCmd.Flags().StringVar(&draftSub, "sub", "", "subreddit name to draft for (e.g., golang)")
	draftCmd.Flags().StringVar(&draftProviders, "providers", "", "comma-separated providers to fan out across (default from config)")
	draftCmd.Flags().StringVar(&draftAnthropic, "anthropic-model", "", "Anthropic model to use (default from config)")
	draftCmd.Flags().StringVar(&draftOpenAI, "openai-model", "", "OpenAI model to use (default from config)")
	draftCmd.Flags().StringVar(&draftRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
	draftCmd.Flags().BoolVar(&draftDryRun, "dry-run", false, "render the prompt only, do not call the LLM")
	draftCmd.Flags().BoolVar(&draftRefresh, "refresh", false, "force fresh Reddit fetch, bypass cache")
	draftCmd.Flags().StringVar(&draftNotesPath, "notes", "", "path to subreddits.yaml (default ~/.tider/subreddits.yaml)")
	draftCmd.Flags().StringVar(&draftCacheRoot, "cache", "", "Reddit cache root dir (default ~/.tider/cache)")
	draftCmd.Flags().IntVar(&draftMaxTokens, "max-tokens", 0, "LLM completion budget; 0 uses variant default")
	draftCmd.Flags().StringVar(&draftVariantSet, "variants", "default", "variant set: default (2×3×2) | full (3×5×3)")
}
