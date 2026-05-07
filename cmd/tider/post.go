package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/contextbank"
	"github.com/viggy28/tider/draft"
	"github.com/viggy28/tider/intake"
	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/progress"
	"github.com/viggy28/tider/internal/reddit"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/lastdraft"
	"github.com/viggy28/tider/reply"
	"github.com/viggy28/tider/research"
)

var (
	postBriefPath  string
	postNote       string
	postFile       string
	postURL        string
	postContexts   []string
	postSub        string
	postProviders  string
	postAnthropic  string
	postOpenAI     string
	postRender     string
	postDryRun     bool
	postRefresh    bool
	postNotesPath  string
	postCacheRoot  string
	postMaxTokens  int
	postVariantSet string
)

var postCmd = &cobra.Command{
	Use:   "post",
	Short: "Draft a Reddit submission for a subreddit (one-step from --note, stdin, --file, or --url)",
	Long: `post turns operator intent or source material plus per-sub research into
structured post drafts, fanning out across providers so framings are
side-by-side comparable.

Source input — exactly one is required:

  --note "..."      operator intent (raw, no extraction)
  stdin             piped content (raw, no extraction); e.g. pbpaste | tider post --sub=…
  --file=path       local file (extracted via LLM)
  --url=https://…   remote URL (extracted via LLM)
  --brief=path      pre-built Brief JSON (advanced — output of ` + "`tider intake`" + `)

Reusable background context (repeatable):

  --context=<id>    name in the context bank (~/.tider/contexts/<id>.md)
  --context=path/to/file.md

  tider post --sub=EtsySellers --note="Ask sellers whether AI listing images hurt buyer trust. Don't pitch Kova." --context=kova
  pbpaste | tider post --sub=EtsySellers --context=kova
  tider post --sub=EtsySellers --file=./notes.md
  tider post --sub=EtsySellers --url=https://example.com/source

API keys come from env vars: ANTHROPIC_API_KEY, OPENAI_API_KEY. Providers
without a key are skipped with a warning rather than failing the run.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPost(cmd.Context(), os.Stdin)
	},
}

func runPost(ctx context.Context, stdin *os.File) error {
	if postSub == "" {
		return errors.New("--sub is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// --dry-run skips provider selection, drafting, and snapshot save:
	// 4 stages (resolve / research / contexts / render prompt) versus 6
	// for a normal run.
	rep := newReporter()
	if postDryRun {
		rep.Start(4)
	} else {
		rep.Start(6)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	rep.Step("resolving source...")
	brief, operatorNote, err := resolvePostSource(ctx, cfg, stdin, rep)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("home dir: %w", err)
	}
	cacheRoot := postCacheRoot
	if cacheRoot == "" {
		cacheRoot = filepath.Join(home, ".tider", "cache")
	}
	notesPath := postNotesPath
	if notesPath == "" {
		notesPath = filepath.Join(home, ".tider", "subreddits.yaml")
	}

	rep.Step(fmt.Sprintf("loading Reddit research for r/%s...", postSub))
	notes, err := research.LoadNotes(notesPath)
	if err != nil {
		return err
	}
	client := reddit.NewClient(reddit.NewCache(cacheRoot))
	researchBundle, err := research.For(ctx, client, notes, postSub, postRefresh)
	if err != nil {
		return fmt.Errorf("research %s: %w", postSub, err)
	}

	rep.Step("loading contexts: " + formatContextLabel(postContexts))
	bankDir, err := contextbank.DefaultDir()
	if err != nil {
		return err
	}
	contexts, err := reply.LoadContexts(bankDir, postContexts)
	if err != nil {
		return err
	}

	_, _, cfgMaxTokens := cfg.FanOutModels("post")

	opts := draft.Default()
	if postVariantSet == "full" {
		opts = draft.Full()
	}
	if postMaxTokens > 0 {
		opts.MaxTokens = postMaxTokens
	} else if cfgMaxTokens > 0 {
		opts.MaxTokens = cfgMaxTokens
	}
	opts.AuthorContext = cfg.AuthorContext

	in := draft.Input{
		Brief:        *brief,
		Research:     *researchBundle,
		Contexts:     contexts,
		OperatorNote: operatorNote,
		Opts:         opts,
	}

	if postDryRun {
		rep.Step("rendering prompt (dry-run)...")
		prompt, err := draft.RenderPrompt(in)
		if err != nil {
			return err
		}
		rep.Done()
		fmt.Println(prompt)
		return nil
	}

	rep.Step("selecting providers...")
	providers := postProviders
	if providers == "" {
		providers = cfg.Defaults.Providers
	}
	anthropicModel := postAnthropic
	openaiModel := postOpenAI
	if anthropicModel == "" {
		anthropicModel = cfg.LLM.AnthropicModel
	}
	if openaiModel == "" {
		openaiModel = cfg.LLM.OpenAIModel
	}
	refs, err := buildProviderRefs(providers, anthropicModel, openaiModel, rep)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return errors.New("no usable providers — set ANTHROPIC_API_KEY and/or OPENAI_API_KEY")
	}

	rep.Step(fmt.Sprintf("drafting with %s...", providerSummary(refs)))
	bundle, err := draft.Generate(ctx, refs, in)
	if err != nil {
		return err
	}

	if root, derr := lastdraft.Default(); derr == nil {
		snap := &types.Snapshot{Brief: *brief, Research: *researchBundle, Bundle: *bundle}
		if serr := lastdraft.Save(root, postSub, snap); serr != nil {
			rep.Warn("failed to save last-draft snapshot: %v", serr)
		}
	}
	rep.Step("saved snapshot")
	rep.Done()

	switch resolveRender(postRender) {
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
		return fmt.Errorf("unknown --render value: %s (use json or markdown)", postRender)
	}
	return nil
}

// resolvePostSource picks the single allowed source input and returns
// the resulting Brief plus the operator-note string (empty for
// --file/--url/--brief because those go through extraction). Stdin is
// used only when no source flag is set and stdin is piped — an
// interactive shell is treated as "no source" so we can fail with a
// usage message instead of blocking on a tty.
//
// rep is threaded so the intake LLM-fallback warning honors --quiet /
// --no-progress instead of bypassing them via a raw stderr write.
func resolvePostSource(ctx context.Context, cfg *config.Config, stdin *os.File, rep *progress.Reporter) (*types.Brief, string, error) {
	sources := []string{}
	if postNote != "" {
		sources = append(sources, "--note")
	}
	if postFile != "" {
		sources = append(sources, "--file")
	}
	if postURL != "" {
		sources = append(sources, "--url")
	}
	if postBriefPath != "" {
		sources = append(sources, "--brief")
	}
	stdinPiped := isStdinPiped(stdin)
	if len(sources) == 0 && stdinPiped {
		sources = append(sources, "stdin")
	}

	switch len(sources) {
	case 0:
		return nil, "", errors.New("provide a source: --note, stdin (piped), --file, --url, or --brief")
	case 1:
		// proceed
	default:
		return nil, "", fmt.Errorf("only one source may be set; got %s", strings.Join(sources, ", "))
	}

	switch sources[0] {
	case "--note":
		b, err := intake.FromNote(postNote)
		if err != nil {
			return nil, "", err
		}
		return b, b.Summary, nil
	case "stdin":
		b, err := intake.FromStdin(stdin, 256*1024)
		if err != nil {
			return nil, "", err
		}
		return b, b.Summary, nil
	case "--file":
		ip, err := newIntakeProvider(cfg, rep)
		if err != nil {
			return nil, "", err
		}
		b, err := ip.build().FromFile(ctx, postFile)
		if err != nil {
			return nil, "", err
		}
		return b, "", nil
	case "--url":
		ip, err := newIntakeProvider(cfg, rep)
		if err != nil {
			return nil, "", err
		}
		b, err := ip.build().FromURL(ctx, postURL)
		if err != nil {
			return nil, "", err
		}
		return b, "", nil
	case "--brief":
		b, err := loadBrief(postBriefPath)
		if err != nil {
			return nil, "", err
		}
		return b, "", nil
	}
	return nil, "", fmt.Errorf("internal: unhandled source %s", sources[0])
}

func newIntakeProvider(cfg *config.Config, rep *progress.Reporter) (postIntake, error) {
	provider, model, maxTokens := cfg.ForTask("intake")
	p, err := llm.New(llm.Config{Provider: provider, Model: model})
	if err == nil {
		return postIntake{p: p, maxTokens: maxTokens}, nil
	}
	// Fall back to the other provider when the configured intake
	// provider's key isn't set. Without this a single-provider user
	// (only ANTHROPIC_API_KEY, default intake → openai) hits a hard
	// fail at --file/--url before drafting even runs, which
	// contradicts the rest of post's "missing key → skip with warning"
	// behavior. Codex P1 finding from PR #48 review.
	altProvider, altModel := otherIntakeProvider(provider, cfg)
	if altProvider == "" {
		return postIntake{}, fmt.Errorf("intake: %w", err)
	}
	p, altErr := llm.New(llm.Config{Provider: altProvider, Model: altModel})
	if altErr != nil {
		return postIntake{}, fmt.Errorf("intake: no usable provider — set ANTHROPIC_API_KEY or OPENAI_API_KEY")
	}
	rep.Warn("intake: %s key missing, falling back to %s", provider, altProvider)
	return postIntake{p: p, maxTokens: maxTokens}, nil
}

// otherIntakeProvider returns the cross-provider name + its configured
// model for use when the primary intake provider's key is missing.
// Returns ("", "") for unknown primaries (no fallback attempted).
func otherIntakeProvider(current string, cfg *config.Config) (string, string) {
	switch current {
	case "openai":
		return "anthropic", cfg.LLM.AnthropicModel
	case "anthropic":
		return "openai", cfg.LLM.OpenAIModel
	default:
		return "", ""
	}
}

type postIntake struct {
	p         llm.Provider
	maxTokens int
}

func (ip postIntake) build() *intake.Intake {
	i := intake.New(ip.p)
	if ip.maxTokens > 0 {
		i.MaxTokens = ip.maxTokens
	}
	return i
}

// isStdinPiped reports whether stdin is connected to a pipe or
// redirected file rather than an interactive terminal. Mirrors the
// idiom in term.go's isTerminal but for stdin.
func isStdinPiped(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice == 0
}

func loadBrief(path string) (*types.Brief, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read brief: %w", err)
	}
	var b types.Brief
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse brief: %w", err)
	}
	if b.Title == "" {
		return nil, errors.New("brief has no title — is this a real Brief JSON?")
	}
	return &b, nil
}

// buildProviderRefs constructs the list of providers to fan out across.
// providersFlag is comma-separated ("anthropic,openai"). Each requested
// provider needs its API key in the environment; missing keys cause that
// provider to be silently skipped (with a warning) rather than failing
// the whole run, so a working provider isn't blocked by a missing key
// for the other.
func buildProviderRefs(providersFlag, anthropicModel, openaiModel string, rep *progress.Reporter) ([]draft.ProviderRef, error) {
	want := strings.Split(providersFlag, ",")
	var refs []draft.ProviderRef
	for _, name := range want {
		name = strings.TrimSpace(name)
		switch name {
		case "anthropic":
			p, err := llm.NewAnthropic(anthropicModel)
			if err != nil {
				rep.Warn("skipping anthropic: %v", err)
				continue
			}
			refs = append(refs, draft.ProviderRef{Provider: p, Model: anthropicModel})
		case "openai":
			p, err := llm.NewOpenAI(openaiModel)
			if err != nil {
				rep.Warn("skipping openai: %v", err)
				continue
			}
			refs = append(refs, draft.ProviderRef{Provider: p, Model: openaiModel})
		case "":
			// trailing comma; ignore
		default:
			return nil, fmt.Errorf("unknown provider %q (supported: anthropic, openai)", name)
		}
	}
	return refs, nil
}

// providerSummary renders a fan-out provider list as a stage-progress
// suffix, e.g. "anthropic/claude-3-5-sonnet, openai/gpt-5".
func providerSummary(refs []draft.ProviderRef) string {
	parts := make([]string, 0, len(refs))
	for _, r := range refs {
		parts = append(parts, fmt.Sprintf("%s/%s", r.Provider.Name(), r.Model))
	}
	return strings.Join(parts, ", ")
}

func init() {
	postCmd.Flags().StringVar(&postBriefPath, "brief", "", "path to a brief.json (advanced — output of `tider intake`)")
	postCmd.Flags().StringVar(&postNote, "note", "", "inline operator intent; rendered raw without LLM extraction")
	postCmd.Flags().StringVar(&postFile, "file", "", "local file with source material; LLM-extracted into a Brief")
	postCmd.Flags().StringVar(&postURL, "url", "", "URL with source material; LLM-extracted into a Brief")
	postCmd.Flags().StringSliceVar(&postContexts, "context", nil, "context bank id or path; repeatable")
	postCmd.Flags().StringVar(&postSub, "sub", "", "subreddit name to draft for (e.g., golang)")
	postCmd.Flags().StringVar(&postProviders, "providers", "", "comma-separated providers to fan out across (default from config)")
	postCmd.Flags().StringVar(&postAnthropic, "anthropic-model", "", "Anthropic model to use (default from config)")
	postCmd.Flags().StringVar(&postOpenAI, "openai-model", "", "OpenAI model to use (default from config)")
	postCmd.Flags().StringVar(&postRender, "render", "", "output format: json | markdown (default: markdown in TTY, json when piped)")
	postCmd.Flags().BoolVar(&postDryRun, "dry-run", false, "render the prompt only, do not call the LLM")
	postCmd.Flags().BoolVar(&postRefresh, "refresh", false, "force fresh Reddit fetch, bypass cache")
	postCmd.Flags().StringVar(&postNotesPath, "notes", "", "path to subreddits.yaml (default ~/.tider/subreddits.yaml)")
	postCmd.Flags().StringVar(&postCacheRoot, "cache", "", "Reddit cache root dir (default ~/.tider/cache)")
	postCmd.Flags().IntVar(&postMaxTokens, "max-tokens", 0, "LLM completion budget; 0 uses variant default")
	postCmd.Flags().StringVar(&postVariantSet, "variants", "default", "variant set: default (2×3×2) | full (3×5×3)")
}
