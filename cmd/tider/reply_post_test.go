package main

import (
	"bufio"
	"bytes"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestPromptTagsParsesNumbersAndNames(t *testing.T) {
	tags := []string{"keep-mostly-same", "shorter", "rewrote-thesis", "more-direct"}

	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"single number", "2\n", []string{"shorter"}},
		{"multiple numbers", "1,3\n", []string{"keep-mostly-same", "rewrote-thesis"}},
		{"name", "more-direct\n", []string{"more-direct"}},
		{"mixed name and number", "shorter, 3\n", []string{"shorter", "rewrote-thesis"}},
		{"dedupe", "2,2,shorter\n", []string{"shorter"}},
		{"out of range silently dropped", "99,1\n", []string{"keep-mostly-same"}},
		{"unknown name silently dropped", "garbage,shorter\n", []string{"shorter"}},
		{"empty returns nil", "\n", nil},
		{"whitespace returns nil", "  \n", nil},
		{"case-insensitive name", "SHORTER\n", []string{"shorter"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := bufio.NewReader(strings.NewReader(c.input))
			got := promptTags(in, io.Discard, tags)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("input=%q got %v, want %v", c.input, got, c.want)
			}
		})
	}
}

func TestConfirmYesNoDefaultsToNo(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"YES\n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},      // blank → no (safer default for overwrite)
		{"garbage\n", false}, // anything else → no
	}
	for _, c := range cases {
		in := bufio.NewReader(strings.NewReader(c.input))
		got, err := confirmYesNo(in, io.Discard, "overwrite?")
		if err != nil {
			t.Fatalf("input=%q: %v", c.input, err)
		}
		if got != c.want {
			t.Errorf("input=%q got %v, want %v", c.input, got, c.want)
		}
	}
}

func TestReadMultilinePreservesBlankLines(t *testing.T) {
	// Reddit replies routinely contain paragraph breaks; the spec calls
	// out that blank-line termination is wrong because of this. Verify
	// blank lines round-trip.
	input := "first paragraph\n\nsecond paragraph\n\n  indented line\n"
	got, err := readMultiline(bytes.NewReader([]byte(input)))
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Errorf("body changed: %q vs %q", got, input)
	}
}
