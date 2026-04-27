package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
)

const seedAuthorContext = `Software engineering leader and serial founder with deep roots in Postgres
and distributed systems. Five years at Cloudflare leading the Postgres /
storage platform team (scaling to 1M+ QPS). Co-founder of Omnigres
(Postgres-as-a-runtime). Currently building Streambed — a WAL-native CDC
tool written in Go that pipes Postgres data into Iceberg/Parquet on S3
and queries it via DuckDB over the Postgres wire protocol, with no
external catalog dependencies and a single-binary architecture. Go-first
builder; speaks at QConSF and other technical conferences.
`

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Inspect or scaffold ~/.tider/config.yaml",
	Long: `tider's config lives at ~/.tider/config.yaml. It holds provider/model
defaults (per-task overrides supported), default render mode, and your
author_context — the multi-line voice description that grounds drafts in
your real lived experience instead of generic developer-tone prose.

Subcommands:
  tider config show   # print the effective config (defaults + overrides)
  tider config init   # scaffold ~/.tider/config.yaml with sane defaults`,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the effective config",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Load()
		if err != nil {
			return err
		}
		data, err := c.Marshal()
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	},
}

var configInitForce bool

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold ~/.tider/config.yaml with defaults",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := config.Path()
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err == nil && !configInitForce {
			return errors.New(path + " already exists; use --force to overwrite")
		}
		c := config.Default()
		c.AuthorContext = seedAuthorContext
		if err := c.Save(); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "wrote %s — edit author_context to match your voice.\n", path)
		return nil
	},
}

func init() {
	configInitCmd.Flags().BoolVar(&configInitForce, "force", false, "overwrite an existing config file")
	configCmd.AddCommand(configShowCmd)
	configCmd.AddCommand(configInitCmd)
}
