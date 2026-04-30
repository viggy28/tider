package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/contextbank"
)

var (
	contextDir         string
	contextImportForce bool
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Manage reusable drafting context files",
	Long: `Manage reusable project/context notes stored under ~/.tider/contexts.

Examples:
  tider context import kova ./kova.md
  tider context list
  tider context show kova
  tider context edit kova`,
}

var contextListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved contexts",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolveContextDir()
		if err != nil {
			return err
		}
		entries, err := contextbank.List(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			fmt.Println(e.ID)
		}
		return nil
	},
}

var contextShowCmd = &cobra.Command{
	Use:   "show <id-or-path>",
	Short: "Print a saved or ad hoc context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolveContextDir()
		if err != nil {
			return err
		}
		entry, err := contextbank.Load(dir, args[0])
		if err != nil {
			return err
		}
		fmt.Print(entry.Body)
		return nil
	},
}

var contextImportCmd = &cobra.Command{
	Use:   "import <id> <path>",
	Short: "Import a markdown file into the context bank",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := resolveContextDir()
		if err != nil {
			return err
		}
		entry, err := contextbank.Import(dir, args[0], args[1], contextImportForce)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "imported %s -> %s\n", entry.ID, entry.Path)
		return nil
	},
}

var contextEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Open a context in $EDITOR",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			return errors.New("EDITOR not set")
		}
		dir, err := resolveContextDir()
		if err != nil {
			return err
		}
		path, err := contextbank.Ensure(dir, args[0])
		if err != nil {
			return err
		}
		c := exec.Command(editor, path)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("run editor: %w", err)
		}
		return nil
	},
}

func resolveContextDir() (string, error) {
	if contextDir != "" {
		return contextDir, nil
	}
	return contextbank.DefaultDir()
}

func init() {
	contextCmd.PersistentFlags().StringVar(&contextDir, "dir", "", "context bank directory (default ~/.tider/contexts)")
	contextImportCmd.Flags().BoolVar(&contextImportForce, "force", false, "overwrite an existing context")
	contextCmd.AddCommand(contextListCmd)
	contextCmd.AddCommand(contextShowCmd)
	contextCmd.AddCommand(contextImportCmd)
	contextCmd.AddCommand(contextEditCmd)
}
