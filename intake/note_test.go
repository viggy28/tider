package intake

import (
	"strings"
	"testing"
)

func TestFromNoteBuildsBriefWithoutExtraction(t *testing.T) {
	const note = "Ask sellers whether AI listing images hurt buyer trust. Don't pitch Kova."
	b, err := FromNote(note)
	if err != nil {
		t.Fatal(err)
	}
	if b.Source.Mode != "note" {
		t.Errorf("Source.Mode = %q, want note", b.Source.Mode)
	}
	if b.Source.Value != "" {
		t.Errorf("Source.Value = %q, want empty for note", b.Source.Value)
	}
	if b.Title != "Operator note" {
		t.Errorf("Title = %q, want 'Operator note'", b.Title)
	}
	if b.Summary != note {
		t.Errorf("Summary = %q, want verbatim note", b.Summary)
	}
	if b.RawContent != note {
		t.Errorf("RawContent = %q, want verbatim note", b.RawContent)
	}
	if len(b.Highlights) != 0 {
		t.Errorf("Highlights = %v, want empty (no extraction)", b.Highlights)
	}
	if b.CreatedAt.IsZero() {
		t.Error("CreatedAt unset")
	}
}

func TestFromNoteTrimsWhitespace(t *testing.T) {
	b, err := FromNote("   hello   \n")
	if err != nil {
		t.Fatal(err)
	}
	if b.Summary != "hello" {
		t.Errorf("Summary = %q, want trimmed", b.Summary)
	}
}

func TestFromNoteRejectsEmpty(t *testing.T) {
	cases := []string{"", "   ", "\n\t"}
	for _, in := range cases {
		if _, err := FromNote(in); err == nil {
			t.Errorf("FromNote(%q) should error", in)
		}
	}
}

func TestFromStdinBuildsBriefWithoutExtraction(t *testing.T) {
	const body = "Long pasted material from pbpaste\n\nWith multiple paragraphs."
	b, err := FromStdin(strings.NewReader(body), 0)
	if err != nil {
		t.Fatal(err)
	}
	if b.Source.Mode != "stdin" {
		t.Errorf("Source.Mode = %q, want stdin", b.Source.Mode)
	}
	if b.Source.Value != "-" {
		t.Errorf("Source.Value = %q, want '-'", b.Source.Value)
	}
	if b.Title != "Operator note" {
		t.Errorf("Title = %q", b.Title)
	}
	if b.Summary != body {
		t.Errorf("Summary mismatch")
	}
	if b.RawContent != body {
		t.Errorf("RawContent mismatch")
	}
}

func TestFromStdinHonorsMaxBytes(t *testing.T) {
	big := strings.Repeat("a", 1000)
	b, err := FromStdin(strings.NewReader(big), 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Summary) != 100 {
		t.Errorf("Summary len = %d, want 100", len(b.Summary))
	}
}

func TestFromStdinRejectsEmpty(t *testing.T) {
	if _, err := FromStdin(strings.NewReader(""), 0); err == nil {
		t.Error("empty stdin should error")
	}
	if _, err := FromStdin(strings.NewReader("   \n"), 0); err == nil {
		t.Error("whitespace-only stdin should error")
	}
}
