// Package config loads ~/.tider/config.yaml and provides typed access to
// the values. Missing config is fine — Default() returns hardcoded sane
// defaults that match what the project's been using as flag defaults.
//
// Note: the original plan listed `viper` as the config library. Going
// with yaml.v3 (already a dep from research/) instead — viper would pull
// ~30 transitive deps for what amounts to "load one YAML file," which
// runs counter to the project's minimal-deps stance (no SDKs, stdlib
// HTTP, etc.). The package boundary here is what callers see, so a swap
// to viper later is mechanical.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the full schema of ~/.tider/config.yaml. Every field is
// optional — missing values fall back to Default().
type Config struct {
	LLM           LLMConfig      `yaml:"llm"`
	Defaults      DefaultsConfig `yaml:"defaults"`
	AuthorContext string         `yaml:"author_context"`
}

type LLMConfig struct {
	// Single-provider defaults — used by commands that don't fan out
	// (intake, eventually suggest/reply). The CLI's --provider / --model
	// flags fall back to these.
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	MaxTokens int    `yaml:"max_tokens"`

	// Per-provider model names — used by fan-out commands (draft, regen)
	// where both providers run in parallel and each needs its own model.
	AnthropicModel string `yaml:"anthropic_model"`
	OpenAIModel    string `yaml:"openai_model"`

	// Per-task overrides. Tasks may set their own provider/model/max_tokens
	// to differ from the LLM-level defaults (e.g. intake uses cheap mini,
	// draft uses reasoning model).
	Tasks map[string]TaskConfig `yaml:"tasks"`
}

// TaskConfig is the per-task override block. Empty fields mean "use the
// LLM-level default" — different tasks have different cost/quality
// profiles (intake → cheap mini, draft → reasoning).
type TaskConfig struct {
	Provider  string `yaml:"provider,omitempty"`
	Model     string `yaml:"model,omitempty"`
	MaxTokens int    `yaml:"max_tokens,omitempty"`
}

type DefaultsConfig struct {
	Render    string `yaml:"render,omitempty"`
	Providers string `yaml:"providers,omitempty"`
}

// Default returns the fallback config: matches the hardcoded flag
// defaults that have been baked into the CLI commands so far.
func Default() *Config {
	return &Config{
		LLM: LLMConfig{
			Provider:       "openai",
			Model:          "gpt-5",
			MaxTokens:      10000,
			AnthropicModel: "claude-sonnet-4-7",
			OpenAIModel:    "gpt-5",
			Tasks: map[string]TaskConfig{
				"intake": {Model: "gpt-4o-mini", MaxTokens: 8192},
				"draft":  {MaxTokens: 10000},
				"regen":  {MaxTokens: 4096},
			},
		},
		Defaults: DefaultsConfig{
			Render:    "",
			Providers: "openai,anthropic",
		},
	}
}

// Path returns the canonical config path: ~/.tider/config.yaml.
func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".tider", "config.yaml"), nil
}

// Load reads ~/.tider/config.yaml and overlays it on Default(). A missing
// file is not an error — defaults flow through.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return LoadFrom(path)
}

// LoadFrom reads config from an explicit path. Useful for tests.
func LoadFrom(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	// Ensure Tasks map is non-nil so callers can range over it safely.
	if cfg.LLM.Tasks == nil {
		cfg.LLM.Tasks = map[string]TaskConfig{}
	}
	return cfg, nil
}

// ForTask resolves provider/model/max_tokens for the named task, falling
// back to LLM-level defaults when a per-task field is empty. Used by
// single-provider commands (intake).
func (c *Config) ForTask(task string) (provider, model string, maxTokens int) {
	provider = c.LLM.Provider
	model = c.LLM.Model
	maxTokens = c.LLM.MaxTokens
	if t, ok := c.LLM.Tasks[task]; ok {
		if t.Provider != "" {
			provider = t.Provider
		}
		if t.Model != "" {
			model = t.Model
		}
		if t.MaxTokens > 0 {
			maxTokens = t.MaxTokens
		}
	}
	return
}

// FanOutModels returns the per-provider models and max-tokens budget for
// commands that fan out across providers (draft, regen). Per-task
// MaxTokens override applies; per-provider model names are LLM-level
// (rare to want different per-provider models per task).
func (c *Config) FanOutModels(task string) (anthropicModel, openaiModel string, maxTokens int) {
	anthropicModel = c.LLM.AnthropicModel
	openaiModel = c.LLM.OpenAIModel
	maxTokens = c.LLM.MaxTokens
	if t, ok := c.LLM.Tasks[task]; ok {
		if t.MaxTokens > 0 {
			maxTokens = t.MaxTokens
		}
	}
	return
}

// Marshal returns the config as YAML bytes — used by `tider config show`.
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}

// Save writes the config to its canonical path, creating the parent
// directory if needed.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir config: %w", err)
	}
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
