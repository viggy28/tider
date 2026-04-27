package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/internal/reddit"
	"github.com/viggy28/tider/research"
)

var (
	researchRefresh   bool
	researchNotesPath string
	researchCacheRoot string
)

var researchCmd = &cobra.Command{
	Use:   "research <sub>",
	Short: "Fetch and assemble a research bundle for a subreddit",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sub := args[0]
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

		notes, err := research.LoadNotes(notesPath)
		if err != nil {
			return err
		}

		client := reddit.NewClient(reddit.NewCache(cacheRoot))
		bundle, err := research.For(context.Background(), client, notes, sub, researchRefresh)
		if err != nil {
			return err
		}
		out, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	},
}

func init() {
	researchCmd.Flags().BoolVar(&researchRefresh, "refresh", false, "force fresh fetch, bypass cache")
	researchCmd.Flags().StringVar(&researchNotesPath, "notes", "", "path to subreddits.yaml (default ~/.tider/subreddits.yaml)")
	researchCmd.Flags().StringVar(&researchCacheRoot, "cache", "", "cache root dir (default ~/.tider/cache)")
}
