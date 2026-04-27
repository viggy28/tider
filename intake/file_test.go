package intake

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/llm"
)

func TestFromFileExtractsBrief(t *testing.T) {
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}

	brief, err := i.FromFile(context.Background(), "testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}

	if brief.Source.Mode != "file" {
		t.Errorf("source mode = %q", brief.Source.Mode)
	}
	if !strings.HasSuffix(brief.Source.Value, "testdata/sample.md") {
		t.Errorf("source value = %q", brief.Source.Value)
	}
	if brief.Title != "Streambed" {
		t.Errorf("title = %q", brief.Title)
	}
	if len(brief.Highlights) != 5 {
		t.Errorf("highlights len = %d", len(brief.Highlights))
	}
	if !strings.Contains(brief.RawContent, "WAL-native CDC tool") {
		t.Errorf("raw content not preserved")
	}
	if brief.CreatedAt.IsZero() {
		t.Error("CreatedAt unset")
	}
}

func TestFromFileSendsRawContentToProvider(t *testing.T) {
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}

	_, err := i.FromFile(context.Background(), "testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.gotRequests) != 1 {
		t.Fatalf("expected 1 LLM call, got %d", len(fake.gotRequests))
	}
	req := fake.gotRequests[0]
	if !req.JSONMode {
		t.Error("JSONMode should be true for extraction")
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != llm.RoleUser {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	prompt := req.Messages[0].Content
	if !strings.Contains(prompt, "Streambed") {
		t.Error("prompt did not include source content")
	}
	if !strings.Contains(prompt, "Source mode: file") {
		t.Error("prompt did not include source mode")
	}
}

func TestFromFileMissing(t *testing.T) {
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{Provider: fake, MaxBytes: 1 << 20, MaxTokens: 1024}
	_, err := i.FromFile(context.Background(), filepath.Join(t.TempDir(), "missing.md"))
	if err == nil {
		t.Fatal("expected error opening missing file")
	}
	if !strings.Contains(err.Error(), "open") {
		t.Errorf("error not from open path: %v", err)
	}
}

func TestFromFileRespectsMaxBytes(t *testing.T) {
	// 10KB file, MaxBytes=1KB → only first 1KB read
	dir := t.TempDir()
	path := filepath.Join(dir, "big.md")
	big := strings.Repeat("abcdefghij", 1024) // 10KB
	if err := writeFile(path, big); err != nil {
		t.Fatal(err)
	}
	fake := &fakeProvider{name: "fake", response: cannedBriefJSON}
	i := &Intake{Provider: fake, MaxBytes: 1024, MaxTokens: 1024}

	brief, err := i.FromFile(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(brief.RawContent); got != 1024 {
		t.Errorf("raw content len = %d, want 1024 (truncated)", got)
	}
}
