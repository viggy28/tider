package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/reddit"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/research"
)

var (
	researchRefresh   bool
	researchNotesPath string
	researchCacheRoot string
	researchRender    string
	researchRaw       bool
	researchProvider  string
	researchModel     string
	researchMaxTokens int
)

var researchCmd = &cobra.Command{
	Use:   "research <sub>",
	Short: "Research pain points and repeated asks in a subreddit",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub, err := research.NormalizeSub(args[0])
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		cacheRoot := researchCacheRoot
		if cacheRoot == "" {
			cacheRoot = filepath.Join(home, ".tider", "cache")
		}
		notesPath := researchNotesPath
		if notesPath == "" {
			notesPath = filepath.Join(home, ".tider", "subreddits.yaml")
		}

		ctx := context.Background()
		bundle, err := loadOrFetchResearch(ctx, cacheRoot, notesPath, sub, researchRefresh)
		if err != nil {
			return err
		}

		if researchRaw {
			return printJSON(bundle)
		}

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		provider, model, maxTokens := cfg.ForTask("research")
		if researchProvider != "" {
			provider = researchProvider
		}
		if researchModel != "" {
			model = researchModel
		}
		if researchMaxTokens > 0 {
			maxTokens = researchMaxTokens
		}
		if !cmd.Flags().Changed("max-tokens") && maxTokens < research.DefaultInsightsMaxTokens {
			maxTokens = research.DefaultInsightsMaxTokens
		}
		p, err := llm.New(llm.Config{Provider: provider, Model: model})
		if err != nil {
			return fmt.Errorf("%w (use --raw to print cached/fetched Reddit data without LLM insights)", err)
		}
		fmt.Fprintf(os.Stderr, "generating pain-point insights with %s/%s...\n", provider, model)
		insights, err := research.GenerateInsights(ctx, p, model, *bundle, maxTokens)
		if err != nil {
			return err
		}
		report := &types.ResearchReport{
			Raw:       *bundle,
			Insights:  *insights,
			Generated: insights.Generated,
		}

		switch researchRender {
		case "", "markdown":
			md := research.RenderMarkdown(insights)
			if isTerminal(os.Stdout) {
				md = renderTerminal(md)
			}
			fmt.Print(md)
		case "json":
			return printJSON(report)
		case "insights-json":
			return printJSON(insights)
		default:
			return fmt.Errorf("unknown --render value: %s (use markdown, json, or insights-json)", researchRender)
		}
		return nil
	},
}

func loadOrFetchResearch(ctx context.Context, cacheRoot, notesPath, sub string, refresh bool) (*types.Research, error) {
	if !refresh {
		if cached, err := research.LoadRaw(cacheRoot, sub, research.RawBundleTTL); err != nil {
			return nil, err
		} else if cached != nil {
			return cached, nil
		}
	}
	notes, err := research.LoadNotes(notesPath)
	if err != nil {
		return nil, err
	}
	client := reddit.NewClient(reddit.NewCache(cacheRoot))
	bundle, err := research.For(ctx, client, notes, sub, refresh)
	if err != nil {
		return nil, err
	}
	if err := research.SaveRaw(cacheRoot, sub, bundle); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save raw research cache: %v\n", err)
	}
	return bundle, nil
}

func printJSON(v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}

func init() {
	researchCmd.Flags().BoolVar(&researchRefresh, "refresh", false, "force fresh fetch, bypass cache")
	researchCmd.Flags().StringVar(&researchNotesPath, "notes", "", "path to subreddits.yaml (default ~/.tider/subreddits.yaml)")
	researchCmd.Flags().StringVar(&researchCacheRoot, "cache", "", "cache root dir (default ~/.tider/cache)")
	researchCmd.Flags().StringVar(&researchRender, "render", "markdown", "output format: markdown | json | insights-json")
	researchCmd.Flags().BoolVar(&researchRaw, "raw", false, "print the raw Reddit research bundle without LLM insights")
	researchCmd.Flags().StringVar(&researchProvider, "provider", "", "LLM provider: anthropic | openai (default from config)")
	researchCmd.Flags().StringVar(&researchModel, "model", "", "LLM model name (default from config)")
	researchCmd.Flags().IntVar(&researchMaxTokens, "max-tokens", 0, "LLM completion budget (default from config)")
}
