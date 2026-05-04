package llm

import "fmt"

// Config is the minimum llm-related configuration the factory needs.
// Full config loading (viper, ~/.tider/config.yaml) lives in a future step;
// this struct is what that loader will populate.
type Config struct {
	Provider string
	Model    string
	Tasks    map[string]TaskConfig
}

type TaskConfig struct {
	Provider string
	Model    string
}

// New constructs the provider implied by cfg, wrapped with retry on
// transient 5xx + 429 responses so every call site (draft, regen,
// classifier, intake, research) is resilient to upstream provider
// hiccups without needing per-caller retry plumbing.
func New(cfg Config) (Provider, error) {
	var (
		p   Provider
		err error
	)
	switch cfg.Provider {
	case "anthropic":
		p, err = NewAnthropic(cfg.Model)
	case "openai":
		p, err = NewOpenAI(cfg.Model)
	case "":
		return nil, fmt.Errorf("llm: provider not configured")
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", cfg.Provider)
	}
	if err != nil {
		return nil, err
	}
	return WithRetry(p), nil
}

// ForTask returns the provider for a named task, falling back to the base
// config when no task-specific override exists.
func ForTask(cfg Config, task string) (Provider, error) {
	if t, ok := cfg.Tasks[task]; ok {
		return New(Config{Provider: t.Provider, Model: t.Model})
	}
	return New(cfg)
}
