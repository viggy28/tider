package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/viggy28/tider/config"
	"github.com/viggy28/tider/internal/types"
)

// resetPostFlags zeros every package-level post flag and restores the
// previous values when the test ends. Without this, table tests share
// state and bleed into each other (the cobra flag parser sets these
// once at command-line parse time; tests don't go through cobra).
func resetPostFlags(t *testing.T) {
	t.Helper()
	prevNote, prevFile, prevURL, prevBrief := postNote, postFile, postURL, postBriefPath
	postNote, postFile, postURL, postBriefPath = "", "", "", ""
	t.Cleanup(func() {
		postNote, postFile, postURL, postBriefPath = prevNote, prevFile, prevURL, prevBrief
	})
}

// stdinPipe returns an *os.File that reads from contents and looks
// "piped" to isStdinPiped (i.e. not a tty). Cleanup closes both ends.
func stdinPipe(t *testing.T, contents string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(contents); err != nil {
		t.Fatal(err)
	}
	w.Close()
	t.Cleanup(func() { r.Close() })
	return r
}

// stdinTTY returns an *os.File that looks like an interactive terminal
// to isStdinPiped. We open /dev/tty when available (CI may not have
// one); on systems without a tty we synthesize the same condition by
// returning os.Stdout, which has ModeCharDevice when tests run under a
// terminal. The function skips the test when neither path works so we
// don't get false failures in headless CI.
func stdinTTY(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open("/dev/tty")
	if err == nil {
		t.Cleanup(func() { f.Close() })
		fi, statErr := f.Stat()
		if statErr == nil && fi.Mode()&os.ModeCharDevice != 0 {
			return f
		}
	}
	t.Skip("no tty available for interactive-stdin simulation")
	return nil
}

func TestResolvePostSourceFromNote(t *testing.T) {
	resetPostFlags(t)
	postNote = "Ask sellers about AI listing images. Don't pitch Kova."

	brief, opNote, err := resolvePostSource(context.Background(), &config.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Source.Mode != "note" {
		t.Errorf("Source.Mode = %q", brief.Source.Mode)
	}
	if brief.Title != "Operator note" {
		t.Errorf("Title = %q", brief.Title)
	}
	if opNote != postNote {
		t.Errorf("operator note = %q, want %q", opNote, postNote)
	}
	if !strings.Contains(brief.Summary, "Don't pitch Kova") {
		t.Errorf("Summary = %q", brief.Summary)
	}
}

func TestResolvePostSourceFromStdin(t *testing.T) {
	resetPostFlags(t)
	const body = "Long pasted material with multiple paragraphs.\n\nFrom pbpaste."
	stdin := stdinPipe(t, body)

	brief, opNote, err := resolvePostSource(context.Background(), &config.Config{}, stdin)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Source.Mode != "stdin" {
		t.Errorf("Source.Mode = %q", brief.Source.Mode)
	}
	if brief.Source.Value != "-" {
		t.Errorf("Source.Value = %q", brief.Source.Value)
	}
	if opNote != body {
		t.Errorf("operator note mismatch")
	}
}

func TestResolvePostSourceConflictingFlags(t *testing.T) {
	resetPostFlags(t)
	postNote = "x"
	postFile = "y.md"

	_, _, err := resolvePostSource(context.Background(), &config.Config{}, nil)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if !strings.Contains(err.Error(), "only one source") {
		t.Errorf("error = %v, want 'only one source'", err)
	}
	if !strings.Contains(err.Error(), "--note") || !strings.Contains(err.Error(), "--file") {
		t.Errorf("conflict error should name both flags; got %v", err)
	}
}

func TestResolvePostSourceConflictWithStdin(t *testing.T) {
	resetPostFlags(t)
	postNote = "operator intent"
	stdin := stdinPipe(t, "piped content")

	// --note set AND stdin piped → should pick --note (explicit flag),
	// not error. Stdin only counts as a source when no flag is set.
	brief, _, err := resolvePostSource(context.Background(), &config.Config{}, stdin)
	if err != nil {
		t.Fatalf("explicit flag should win over piped stdin: %v", err)
	}
	if brief.Source.Mode != "note" {
		t.Errorf("Source.Mode = %q, want note (explicit flag should win)", brief.Source.Mode)
	}
}

func TestResolvePostSourceNoSource(t *testing.T) {
	resetPostFlags(t)
	tty := stdinTTY(t)

	_, _, err := resolvePostSource(context.Background(), &config.Config{}, tty)
	if err == nil {
		t.Fatal("expected usage error when no source set and stdin is interactive")
	}
	if !strings.Contains(err.Error(), "provide a source") {
		t.Errorf("error = %v, want 'provide a source'", err)
	}
}

func TestResolvePostSourceFromBriefJSON(t *testing.T) {
	resetPostFlags(t)
	dir := t.TempDir()
	briefPath := filepath.Join(dir, "brief.json")
	b := types.Brief{
		Source: types.BriefSource{Mode: "file", Value: "notes.md"},
		Title:  "Streambed launch",
	}
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(briefPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	postBriefPath = briefPath

	brief, opNote, err := resolvePostSource(context.Background(), &config.Config{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if brief.Title != "Streambed launch" {
		t.Errorf("Title = %q", brief.Title)
	}
	// --brief is the structured-source path, NOT operator-note semantics.
	if opNote != "" {
		t.Errorf("operator note should be empty for --brief; got %q", opNote)
	}
}

func TestNewIntakeProviderFallsBackWhenPrimaryKeyMissing(t *testing.T) {
	// Default intake task points to openai (gpt-4o-mini). With only
	// ANTHROPIC_API_KEY set, the fallback should kick in instead of
	// hard-failing — that's the Codex P1 fix on PR #48.
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic")

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider:       "openai",
			Model:          "gpt-5",
			AnthropicModel: "claude-sonnet-4-7",
			OpenAIModel:    "gpt-5",
			Tasks: map[string]config.TaskConfig{
				"intake": {Model: "gpt-4o-mini", MaxTokens: 8192},
			},
		},
	}
	ip, err := newIntakeProvider(cfg)
	if err != nil {
		t.Fatalf("expected fallback to anthropic, got error: %v", err)
	}
	if ip.p == nil || ip.p.Name() != "anthropic" {
		t.Errorf("expected anthropic provider; got %v", ip.p)
	}
}

func TestNewIntakeProviderErrorsWhenAllKeysMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	cfg := &config.Config{
		LLM: config.LLMConfig{
			Provider:       "openai",
			Model:          "gpt-5",
			AnthropicModel: "claude-sonnet-4-7",
			OpenAIModel:    "gpt-5",
			Tasks: map[string]config.TaskConfig{
				"intake": {Model: "gpt-4o-mini"},
			},
		},
	}
	_, err := newIntakeProvider(cfg)
	if err == nil {
		t.Fatal("expected error when no provider keys are set")
	}
	if !strings.Contains(err.Error(), "no usable provider") {
		t.Errorf("error should explain no usable provider; got %v", err)
	}
}

func TestIsStdinPipedDetectsPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if !isStdinPiped(r) {
		t.Error("pipe should report as piped")
	}
}

func TestIsStdinPipedHandlesNil(t *testing.T) {
	if isStdinPiped(nil) {
		t.Error("nil should not be piped")
	}
}

// TestResolvePostSourceEmptyNote verifies we surface a clean error
// rather than producing an empty Brief when --note is set to an
// all-whitespace string. intake.FromNote enforces this; this test
// covers the integration.
func TestResolvePostSourceEmptyNote(t *testing.T) {
	resetPostFlags(t)
	postNote = "   \n\t  "

	_, _, err := resolvePostSource(context.Background(), &config.Config{}, nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only note")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty; got %v", err)
	}
}
