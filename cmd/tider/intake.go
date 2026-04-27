package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

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

		p, err := llm.New(llm.Config{Provider: intakeProvider, Model: intakeModel})
		if err != nil {
			return err
		}
		i := intake.New(p)
		if intakeMaxTokens > 0 {
			i.MaxTokens = intakeMaxTokens
		}

		ctx := context.Background()
		var brief *types.Brief
		switch {
		case intakeURL != "":
			brief, err = i.FromURL(ctx, intakeURL)
		case intakeFile != "":
			brief, err = i.FromFile(ctx, intakeFile)
		}
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(brief, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	},
}

func init() {
	intakeCmd.Flags().StringVar(&intakeURL, "url", "", "URL to a blog post or GitHub repo")
	intakeCmd.Flags().StringVar(&intakeFile, "file", "", "path to a markdown brief")
	intakeCmd.Flags().StringVar(&intakeProvider, "provider", "anthropic", "LLM provider: anthropic | openai")
	intakeCmd.Flags().StringVar(&intakeModel, "model", "claude-sonnet-4-7", "LLM model name")
	intakeCmd.Flags().IntVar(&intakeMaxTokens, "max-tokens", 0, "LLM completion budget; 0 uses package default (2048). Bump for reasoning models.")
}
