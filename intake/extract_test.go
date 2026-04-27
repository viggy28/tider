package intake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/types"
)

func TestExtractRendersPromptWithSource(t *testing.T) {
	src := types.BriefSource{Mode: "url", Value: "https://example.com"}
	prompt, err := renderIntakePrompt(src, "raw page body")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Source mode: url") {
		t.Error("prompt missing source mode")
	}
	if !strings.Contains(prompt, "https://example.com") {
		t.Error("prompt missing source value")
	}
	if !strings.Contains(prompt, "raw page body") {
		t.Error("prompt missing raw content")
	}
	if !strings.Contains(prompt, "JSON object") {
		t.Error("prompt missing JSON-output instruction")
	}
}

func TestExtractRejectsBadJSON(t *testing.T) {
	fake := &fakeProvider{name: "fake", response: "not json at all"}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.extract(context.Background(), types.BriefSource{Mode: "file", Value: "x"}, "raw")
	if err == nil || !strings.Contains(err.Error(), "parse brief json") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestExtractIncludesModelOutputInError(t *testing.T) {
	fake := &fakeProvider{name: "fake", response: "<<<garbage>>>"}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.extract(context.Background(), types.BriefSource{Mode: "file", Value: "x"}, "raw")
	if err == nil || !strings.Contains(err.Error(), "<<<garbage>>>") {
		t.Errorf("error did not include model output: %v", err)
	}
}

func TestExtractPropagatesProviderError(t *testing.T) {
	fake := &fakeProvider{name: "fake", err: errors.New("boom")}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.extract(context.Background(), types.BriefSource{Mode: "file", Value: "x"}, "raw")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected wrapped provider error, got %v", err)
	}
}

func TestExtractPopulatesAllFields(t *testing.T) {
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	src := types.BriefSource{Mode: "file", Value: "sample.md"}
	brief, err := i.extract(context.Background(), src, "raw content")
	if err != nil {
		t.Fatal(err)
	}
	if brief.Title != "Streambed" {
		t.Errorf("title = %q", brief.Title)
	}
	if brief.Summary == "" {
		t.Error("summary empty")
	}
	if len(brief.Highlights) == 0 {
		t.Error("highlights empty")
	}
	if brief.Audience == "" {
		t.Error("audience empty")
	}
	if len(brief.Links) == 0 {
		t.Error("links empty")
	}
	if brief.RawContent != "raw content" {
		t.Errorf("raw content = %q", brief.RawContent)
	}
	if brief.Source != src {
		t.Errorf("source = %+v", brief.Source)
	}
	if brief.CreatedAt.IsZero() {
		t.Error("CreatedAt unset")
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 100, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"longer than n", 6, "longer..."},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}
