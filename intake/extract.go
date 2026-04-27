package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var intakeTmpl = template.Must(template.ParseFS(prompts.FS, "intake.tmpl"))

type briefDraft struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Highlights []string `json:"highlights"`
	Audience   string   `json:"audience"`
	Links      []string `json:"links"`
}

func renderIntakePrompt(src types.BriefSource, raw string) (string, error) {
	var buf bytes.Buffer
	err := intakeTmpl.Execute(&buf, struct {
		SourceMode  string
		SourceValue string
		RawContent  string
	}{src.Mode, src.Value, raw})
	if err != nil {
		return "", fmt.Errorf("render intake prompt: %w", err)
	}
	return buf.String(), nil
}

func (i *Intake) extract(ctx context.Context, src types.BriefSource, raw string) (*types.Brief, error) {
	prompt, err := renderIntakePrompt(src, raw)
	if err != nil {
		return nil, err
	}
	resp, err := i.Provider.Complete(ctx, llm.Request{
		MaxTokens: i.MaxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("llm extract: %w", err)
	}
	var d briefDraft
	if err := json.Unmarshal([]byte(resp.Content), &d); err != nil {
		return nil, fmt.Errorf("parse brief json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	return &types.Brief{
		Source:     src,
		Title:      d.Title,
		Summary:    d.Summary,
		Highlights: d.Highlights,
		Audience:   d.Audience,
		Links:      d.Links,
		RawContent: raw,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
