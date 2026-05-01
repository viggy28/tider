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
	postBriefPath  string
	postSub        string
	postProviders  string
	postAnthropic  string
	postOpenAI     string
	postRender     string
	postDryRun     bool
	postRefresh    bool
	postNotesPath  string
	postCacheRoot  string
	postMaxTokens  int
	postVariantSet string
)

var postCmd = &cobra.Command{
	Use:   "post",
	Short: "Draft a Reddit submission (fan-out across providers) for a Brief on one subreddit",
	Long: `post turns a Brief + per-sub research into structured post drafts. By
default it fans out across both Anthropic and OpenAI concurrently so you
can compare framings side-by-side.

  tider post --brief=brief.json --sub=golang
  tider post --brief=brief.json --sub=PostgreSQL --providers=anthropic
  tider post --brief=brief.json --sub=golang --render=markdown
  tider post --brief=brief.json --sub=golang --dry-run    # show prompt only

API keys come from env vars: ANTHROPIC_API_KEY, OPENAI_API_KEY. Providers
without a key are skipped with a warning rather than failing the run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if postBriefPath == "" || postSub == "" {
			return errors.New("--brief and --sub are required")
		}
		brief, err := loadBrief(postBriefPath)
		if err != nil {
			return err
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		cacheRoot := postCacheRoot
		if cacheRoot == "" {
			cacheRoot = filepath.Join(home, ".tider", "cache")
		}
		notesPath := postNotesPath
		if notesPath == "" {
			notesPath = filepath.Join(home, ".tider", "subreddits.yaml")
		}

		notes, err := research.LoadNotes(notesPath)
		if err != nil {
			return err
		}
		client := reddit.NewClient(reddit.NewCache(cacheRoot))
		ctx := context.Background()
		researchBundle, err := research.For(ctx, client, notes, postSub, postRefresh)
		if err != nil {
			return fmt.Errorf("research %s: %w", postSub, err)
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		_, _, cfgMaxTokens := cfg.FanOutModels("post")

		opts := draft.Default()
		if postVariantSet == "full" {
			opts = draft.Full()
		}
		// Resolve max_tokens: explicit flag wins, then config, then opts default.
		if postMaxTokens > 0 {
			opts.MaxTokens = postMaxTokens
		} else if cfgMaxTokens > 0 {
			opts.MaxTokens = cfgMaxTokens
		}
		opts.AuthorContext = cfg.AuthorContext

		if postDryRun {
			prompt, err := draft.RenderPrompt(*brief, *researchBundle, opts)
			if err != nil {
				return err
			}
			fmt.Println(prompt)
			return nil
		}

		// Resolve fan-out provider list and per-provider models with config fallback.
		providers := postProviders
		if providers == "" {
			providers = cfg.Defaults.Providers
		}
		anthropicModel := postAnthropic
		openaiModel := postOpenAI
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
			if serr := lastdraft.Save(root, postSub, snap); serr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save last-draft snapshot: %v\n", serr)
			}
		}

		switch resolveRender(postRender) {
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
			return fmt.Errorf("unknown --render value: %s (use json or markdown)", postRender)
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
	postCmd.Flags().StringVar(&postBriefPath, "brief", "", "path to a brief.json (output of `tider intake`)")
	postCmd.Flags().StringVar(&postSub, "sub", "", "subreddit name to draft for (e.g., golang)")
	postCmd.Flags().StringVar(&postProviders, "providers", "", "comma-separated providers to fan out across (default from config)")
	postCmd.Flags().StringVar(&postAnthropic, "anthropic-model", "", "Anthropic model to use (default from config)")
	postCmd.Flags().StringVar(&postOpenAI, "openai-model", "", "OpenAI model to use (default from config)")
	postCmd.Flags().StringVar(&postRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
	postCmd.Flags().BoolVar(&postDryRun, "dry-run", false, "render the prompt only, do not call the LLM")
	postCmd.Flags().BoolVar(&postRefresh, "refresh", false, "force fresh Reddit fetch, bypass cache")
	postCmd.Flags().StringVar(&postNotesPath, "notes", "", "path to subreddits.yaml (default ~/.tider/subreddits.yaml)")
	postCmd.Flags().StringVar(&postCacheRoot, "cache", "", "Reddit cache root dir (default ~/.tider/cache)")
	postCmd.Flags().IntVar(&postMaxTokens, "max-tokens", 0, "LLM completion budget; 0 uses variant default")
	postCmd.Flags().StringVar(&postVariantSet, "variants", "default", "variant set: default (2×3×2) | full (3×5×3)")
}
