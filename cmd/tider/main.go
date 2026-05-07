package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/internal/progress"
)

var (
	flagQuiet      bool
	flagNoProgress bool
)

var rootCmd = &cobra.Command{
	Use:   "tider",
	Short: "Reddit drafting co-pilot (read-only)",
	Long: `tider drafts Reddit posts and reply suggestions, grounded in each
subreddit's rules and what's worked there. The bot reads Reddit; you
post manually. No auth, no posting, no commenting — by design.`,
}

// newReporter constructs a stage-progress reporter for the current
// command, honoring the persistent --quiet / --no-progress flags.
// Always writes to stderr so stdout stays clean for piping.
func newReporter() *progress.Reporter {
	return progress.New(os.Stderr, progress.Options{
		Quiet:      flagQuiet,
		NoProgress: flagNoProgress,
	})
}

func main() {
	// Auto-load .env from cwd and ~/.tider/.env so users don't have to
	// `source .env` (and don't have to remember `export` or `set -a`).
	// Real env always wins.
	autoloadEnv()

	rootCmd.PersistentFlags().BoolVar(&flagQuiet, "quiet", false, "suppress progress and warnings; only final output and errors")
	rootCmd.PersistentFlags().BoolVar(&flagNoProgress, "no-progress", false, "suppress stage progress lines; warnings and errors still print")

	rootCmd.AddCommand(researchCmd)
	rootCmd.AddCommand(intakeCmd)
	rootCmd.AddCommand(postCmd)
	rootCmd.AddCommand(replyCmd)
	rootCmd.AddCommand(regenCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(contextCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
