package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/draft"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/lastdraft"
	"github.com/viggy28/tider/regen"
)

var (
	regenSub       string
	regenAngle     int
	regenVariant   string
	regenNote      string
	regenLength    int
	regenRender    string
	regenProviders string
	regenAnthropic string
	regenOpenAI    string
)

var regenCmd = &cobra.Command{
	Use:   "regen",
	Short: "Re-roll one piece of an existing draft (titles or body)",
	Long: `regen iterates on a saved draft without regenerating the whole bundle.

Run 'tider post --sub=<sub>' first — it persists a snapshot at
~/.tider/last/<sub>.json that regen reads from. Each successful regen
overwrites the snapshot so subsequent regens iterate on the latest state.

  tider regen titles --sub=databases --angle=2 --note="punchier, lead with the wedge"
  tider regen body --sub=databases --variant=2.1 --note="lighter on tradeoffs" --length=200`,
}

var regenTitlesCmd = &cobra.Command{
	Use:   "titles",
	Short: "Re-roll the titles for a specific angle",
	RunE: func(cmd *cobra.Command, args []string) error {
		if regenSub == "" {
			return errors.New("--sub is required")
		}
		if regenAngle <= 0 {
			return errors.New("--angle must be > 0")
		}
		snap, refs, err := setupRegen()
		if err != nil {
			return err
		}
		bundle, err := regen.Titles(context.Background(), refs, snap, regenAngle, regenNote)
		if err != nil {
			return err
		}
		return finishRegen(snap, bundle)
	},
}

var regenBodyCmd = &cobra.Command{
	Use:   "body",
	Short: "Re-roll a specific body (variant id, e.g. 2.1)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if regenSub == "" {
			return errors.New("--sub is required")
		}
		if regenVariant == "" {
			return errors.New("--variant is required (e.g. --variant=2.1)")
		}
		snap, refs, err := setupRegen()
		if err != nil {
			return err
		}
		bundle, err := regen.Body(context.Background(), refs, snap, regenVariant, regenNote, regenLength)
		if err != nil {
			return err
		}
		return finishRegen(snap, bundle)
	},
}

// setupRegen loads the snapshot for the requested sub and constructs the
// llm provider refs from --providers / --*-model flags, falling back to
// config when flags are unset.
func setupRegen() (*types.Snapshot, []llm.ProviderRef, error) {
	root, err := lastdraft.Default()
	if err != nil {
		return nil, nil, err
	}
	snap, err := lastdraft.Load(root, regenSub)
	if err != nil {
		return nil, nil, err
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	providers := regenProviders
	if providers == "" {
		providers = cfg.Defaults.Providers
	}
	anthropicModel := regenAnthropic
	openaiModel := regenOpenAI
	if anthropicModel == "" {
		anthropicModel = cfg.LLM.AnthropicModel
	}
	if openaiModel == "" {
		openaiModel = cfg.LLM.OpenAIModel
	}
	// Use a real reporter so provider-skip warnings (e.g. one of
	// --providers=anthropic,openai with a missing key) still reach the
	// user. regen has no stage output of its own, so this reporter is
	// only used for Warn.
	refs, err := buildProviderRefs(providers, anthropicModel, openaiModel, newReporter())
	if err != nil {
		return nil, nil, err
	}
	if len(refs) == 0 {
		return nil, nil, errors.New("no usable providers — set ANTHROPIC_API_KEY and/or OPENAI_API_KEY")
	}
	return snap, refs, nil
}

// finishRegen overwrites the snapshot with the new bundle and prints the
// result in the requested format. Saving even on partial failure (some
// drafts have errors) is intentional — the user may want the partial
// progress preserved for the next regen.
func finishRegen(snap *types.Snapshot, bundle *types.DraftBundle) error {
	root, err := lastdraft.Default()
	if err != nil {
		return err
	}
	snap.Bundle = *bundle
	if err := lastdraft.Save(root, regenSub, snap); err != nil {
		return err
	}
	switch resolveRender(regenRender) {
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
		return fmt.Errorf("unknown --render value: %s (use json or markdown)", regenRender)
	}
	return nil
}

func init() {
	for _, c := range []*cobra.Command{regenTitlesCmd, regenBodyCmd} {
		c.Flags().StringVar(&regenSub, "sub", "", "subreddit (the same one you ran `tider post --sub` with)")
		c.Flags().StringVar(&regenNote, "note", "", "guidance for the regeneration (e.g. \"more provocative\")")
		c.Flags().StringVar(&regenRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
		c.Flags().StringVar(&regenProviders, "providers", "", "comma-separated providers (default from config)")
		c.Flags().StringVar(&regenAnthropic, "anthropic-model", "", "Anthropic model to use (default from config)")
		c.Flags().StringVar(&regenOpenAI, "openai-model", "", "OpenAI model to use (default from config)")
	}
	regenTitlesCmd.Flags().IntVar(&regenAngle, "angle", 0, "angle id (e.g. --angle=2)")
	regenBodyCmd.Flags().StringVar(&regenVariant, "variant", "", "body variant id (e.g. --variant=2.1)")
	regenBodyCmd.Flags().IntVar(&regenLength, "length", 0, "target body length in words (0 = no hint)")

	regenCmd.AddCommand(regenTitlesCmd)
	regenCmd.AddCommand(regenBodyCmd)
}
