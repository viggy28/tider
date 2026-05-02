package main

import (
	"strings"
	"testing"
)

func TestReplyRegenRejectsEmptyNote(t *testing.T) {
	// The CLI must reject empty/whitespace --note before any session
	// resolution or LLM call. Set the package-level flag directly to
	// simulate the cobra flag binding without spinning up the full
	// command lifecycle.
	cases := []string{"", "   ", "\t\n  "}
	for _, n := range cases {
		t.Run("note="+n, func(t *testing.T) {
			oldNote := replyRegenNote
			replyRegenNote = n
			defer func() { replyRegenNote = oldNote }()

			err := runReplyRegen(replyRegenCmd, []string{"any-id"})
			if err == nil {
				t.Fatalf("expected error for note %q", n)
			}
			if !strings.Contains(err.Error(), "--note is required") {
				t.Errorf("expected --note required error, got %v", err)
			}
		})
	}
}
