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
