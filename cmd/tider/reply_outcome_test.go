package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
)

func TestPromptIntDefaultsToZeroOnBlank(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("\n"))
	got, err := promptInt(in, io.Discard, "upvotes: ")
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Errorf("blank should default to 0, got %d", got)
	}
}

func TestPromptIntRetriesOnBadInput(t *testing.T) {
	// First line is junk → should reprompt and accept the second.
	in := bufio.NewReader(strings.NewReader("not-a-number\n42\n"))
	got, err := promptInt(in, io.Discard, "upvotes: ")
	if err != nil {
		t.Fatal(err)
	}
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestPromptYesNoAcceptsBothForms(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"no\n", false},
	}
	for _, c := range cases {
		in := bufio.NewReader(strings.NewReader(c.input))
		got, err := promptYesNo(in, io.Discard, "?")
		if err != nil {
			t.Fatalf("input=%q: %v", c.input, err)
		}
		if got != c.want {
			t.Errorf("input=%q got %v, want %v", c.input, got, c.want)
		}
	}
}

func TestPromptYesNoLoopsOnInvalid(t *testing.T) {
	// "maybe" should reprompt; "y" closes the loop.
	in := bufio.NewReader(strings.NewReader("maybe\ny\n"))
	got, err := promptYesNo(in, io.Discard, "?")
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Error("expected true after retry")
	}
}

func TestPromptChoiceByNumber(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("3\n"))
	got, err := promptChoice(in, io.Discard, "landed", landedStates)
	if err != nil {
		t.Fatal(err)
	}
	if got != landedStates[2] {
		t.Errorf("got %q, want %q", got, landedStates[2])
	}
}

func TestPromptChoiceByName(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("HELPED\n"))
	got, err := promptChoice(in, io.Discard, "kova", kovaSignals)
	if err != nil {
		t.Fatal(err)
	}
	if got != "helped" {
		t.Errorf("got %q, want helped (case-insensitive name)", got)
	}
}

func TestPromptChoiceLoopsOnInvalid(t *testing.T) {
	// 99 → out of range, "garbage" → unknown, "1" → first option.
	in := bufio.NewReader(strings.NewReader("99\ngarbage\n1\n"))
	got, err := promptChoice(in, io.Discard, "landed", landedStates)
	if err != nil {
		t.Fatal(err)
	}
	if got != landedStates[0] {
		t.Errorf("got %q, want %q", got, landedStates[0])
	}
}

func TestPromptYesNoAbortsOnEOF(t *testing.T) {
	// Empty stdin (closed pipe) → ReadString returns "" + io.EOF
	// repeatedly. Without an explicit EOF check, the retry loop would
	// spin forever. Verify we abort with a clear error instead.
	in := bufio.NewReader(strings.NewReader(""))
	_, err := promptYesNo(in, io.Discard, "?")
	if err == nil {
		t.Fatal("expected error on EOF, got nil")
	}
	if !strings.Contains(err.Error(), "stdin closed") {
		t.Errorf("expected stdin-closed error, got %v", err)
	}
}

func TestPromptYesNoAbortsAfterInvalidThenEOF(t *testing.T) {
	// First read returns garbage (re-prompt), second read is EOF — must
	// abort, not spin.
	in := bufio.NewReader(strings.NewReader("maybe\n"))
	_, err := promptYesNo(in, io.Discard, "?")
	if err == nil {
		t.Fatal("expected error on EOF after invalid input, got nil")
	}
	if !strings.Contains(err.Error(), "stdin closed") {
		t.Errorf("expected stdin-closed error, got %v", err)
	}
}

func TestPromptChoiceAbortsOnEOF(t *testing.T) {
	in := bufio.NewReader(strings.NewReader(""))
	_, err := promptChoice(in, io.Discard, "landed", landedStates)
	if err == nil {
		t.Fatal("expected error on EOF, got nil")
	}
	if !strings.Contains(err.Error(), "stdin closed") {
		t.Errorf("expected stdin-closed error, got %v", err)
	}
}

func TestPromptChoiceAbortsAfterInvalidThenEOF(t *testing.T) {
	in := bufio.NewReader(strings.NewReader("99\n"))
	_, err := promptChoice(in, io.Discard, "landed", landedStates)
	if err == nil {
		t.Fatal("expected error on EOF after invalid input, got nil")
	}
}

// TestPromptsShareSingleReader exercises the full outcome questionnaire
// sequence (overwrite confirm + upvotes + two yes/no + two choice +
// note) against a single bufio.Reader, simulating piped answers. This
// is the regression test for the bug where runReplyOutcome instantiated
// a fresh bufio.Reader for the overwrite-confirm step and another for
// the questionnaire — the first reader would buffer chunks past its
// own newline and the second reader would lose those bytes, causing
// later prompts to spuriously hit EOF.
func TestPromptsShareSingleReader(t *testing.T) {
	// Simulating: overwrite=y, upvotes=5, op_replied=y, other=n,
	//             landed=1 ("landed"), kova=helped, note="quick note".
	// All in one piped stream. If the bug returned, the second prompt
	// onward would all see EOF.
	input := "y\n5\ny\nn\n1\nhelped\nquick note\n"
	in := bufio.NewReader(strings.NewReader(input))

	overwrite, err := confirmYesNo(in, io.Discard, "overwrite?")
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if !overwrite {
		t.Error("overwrite should be true")
	}

	upvotes, err := promptInt(in, io.Discard, "upvotes: ")
	if err != nil {
		t.Fatalf("upvotes: %v", err)
	}
	if upvotes != 5 {
		t.Errorf("upvotes = %d, want 5", upvotes)
	}

	opReplied, err := promptYesNo(in, io.Discard, "op replied?")
	if err != nil {
		t.Fatalf("op_replied: %v", err)
	}
	if !opReplied {
		t.Error("op_replied should be true")
	}

	other, err := promptYesNo(in, io.Discard, "other engagement?")
	if err != nil {
		t.Fatalf("other: %v", err)
	}
	if other {
		t.Error("other should be false")
	}

	landed, err := promptChoice(in, io.Discard, "landed", landedStates)
	if err != nil {
		t.Fatalf("landed: %v", err)
	}
	if landed != landedStates[0] {
		t.Errorf("landed = %q, want %q", landed, landedStates[0])
	}

	kova, err := promptChoice(in, io.Discard, "kova", kovaSignals)
	if err != nil {
		t.Fatalf("kova: %v", err)
	}
	if kova != "helped" {
		t.Errorf("kova = %q, want helped", kova)
	}

	note := promptLine(in, io.Discard, "note: ")
	if note != "quick note" {
		t.Errorf("note = %q, want %q", note, "quick note")
	}
}
