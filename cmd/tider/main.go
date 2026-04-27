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
	rootCmd.AddCommand(researchCmd)
	rootCmd.AddCommand(intakeCmd)
	rootCmd.AddCommand(draftCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
