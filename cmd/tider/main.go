package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tider",
	Short: "Reddit drafting co-pilot (read-only)",
	Long: `tider drafts Reddit posts and reply suggestions, grounded in each
subreddit's rules and what's worked there. The bot reads Reddit; you
post manually. No auth, no posting, no commenting — by design.`,
}

func main() {
	// Auto-load .env from cwd and ~/.tider/.env so users don't have to
	// `source .env` (and don't have to remember `export` or `set -a`).
	// Real env always wins.
	autoloadEnv()

	rootCmd.AddCommand(researchCmd)
	rootCmd.AddCommand(intakeCmd)
	rootCmd.AddCommand(draftCmd)
	rootCmd.AddCommand(regenCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(contextCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
