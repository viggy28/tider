package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/intake"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
)

var (
	intakeURL       string
	intakeFile      string
	intakeProvider  string
	intakeModel     string
	intakeMaxTokens int
	intakeRender    string
)

var intakeCmd = &cobra.Command{
	Use:   "intake",
	Short: "Turn a URL or file into a structured Brief (read-only, LLM-extracted)",
	Long: `intake reads source material and emits a structured Brief in JSON.

Exactly one of --url or --file must be set. The interactive --topic mode
lands in a follow-up.

API key for the chosen provider must be set in the environment
(ANTHROPIC_API_KEY or OPENAI_API_KEY).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if (intakeURL == "") == (intakeFile == "") {
			return fmt.Errorf("exactly one of --url or --file is required")
		}

		// 2 stages: cheap setup (config + provider) then the LLM
		// extraction call, which is the only step worth waiting on.
		rep := newReporter()
		rep.Start(2)
		rep.Step("validating source...")

		cfg, err := config.Load()
		if err != nil {
			return err
		}
		provider, model, maxTokens := cfg.ForTask("intake")
		// Flags override config when set explicitly.
		if intakeProvider != "" {
			provider = intakeProvider
		}
		if intakeModel != "" {
			model = intakeModel
		}
		if intakeMaxTokens > 0 {
			maxTokens = intakeMaxTokens
		}

		p, err := llm.New(llm.Config{Provider: provider, Model: model})
		if err != nil {
			return err
		}
		i := intake.New(p)
		i.MaxTokens = maxTokens

		ctx := context.Background()
		var brief *types.Brief
		var sourceLabel string
		switch {
		case intakeURL != "":
			sourceLabel = intakeURL
			rep.Step(fmt.Sprintf("extracting brief from %s with %s/%s...", sourceLabel, provider, model))
			brief, err = i.FromURL(ctx, intakeURL)
		case intakeFile != "":
			sourceLabel = intakeFile
			rep.Step(fmt.Sprintf("extracting brief from %s with %s/%s...", sourceLabel, provider, model))
			brief, err = i.FromFile(ctx, intakeFile)
		}
		if err != nil {
			return err
		}
		rep.Done()

		switch resolveRender(intakeRender) {
		case "markdown":
			md := intake.RenderMarkdown(brief)
			if isTerminal(os.Stdout) {
				md = renderTerminal(md)
			}
			fmt.Print(md)
		case "json":
			out, err := json.MarshalIndent(brief, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
		default:
			return fmt.Errorf("unknown --render value: %s (use json or markdown)", intakeRender)
		}
		return nil
	},
}

func init() {
	intakeCmd.Flags().StringVar(&intakeURL, "url", "", "URL to a blog post or GitHub repo")
	intakeCmd.Flags().StringVar(&intakeFile, "file", "", "path to a markdown brief")
	intakeCmd.Flags().StringVar(&intakeProvider, "provider", "", "LLM provider: anthropic | openai (default from config)")
	intakeCmd.Flags().StringVar(&intakeModel, "model", "", "LLM model name (default from config)")
	intakeCmd.Flags().IntVar(&intakeMaxTokens, "max-tokens", 0, "LLM completion budget (default from config)")
	intakeCmd.Flags().StringVar(&intakeRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
}
