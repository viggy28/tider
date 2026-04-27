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

// New constructs the provider implied by cfg.
func New(cfg Config) (Provider, error) {
	switch cfg.Provider {
	case "anthropic":
		return NewAnthropic(cfg.Model)
	case "openai":
		return NewOpenAI(cfg.Model)
	case "":
		return nil, fmt.Errorf("llm: provider not configured")
	default:
		return nil, fmt.Errorf("llm: unknown provider %q", cfg.Provider)
	}
}

// ForTask returns the provider for a named task, falling back to the base
// config when no task-specific override exists.
func ForTask(cfg Config, task string) (Provider, error) {
	if t, ok := cfg.Tasks[task]; ok {
		return New(Config{Provider: t.Provider, Model: t.Model})
	}
	return New(cfg)
}
