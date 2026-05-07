package progress

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "1s"}, // rounds to 1s
		{499 * time.Millisecond, "0s"}, // rounds to 0s
		{5 * time.Second, "5s"},
		{38 * time.Second, "38s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m00s"},
		{63 * time.Second, "1m03s"},
		{72 * time.Second, "1m12s"},
		{102 * time.Second, "1m42s"},
		{3601 * time.Second, "60m01s"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStepFormat(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{})
	r.Start(3)
	r.Step("fetching Reddit thread...")
	r.Step("loading contexts: kova")
	r.Step("classifying thread...")

	got := buf.String()
	want := "[1/3] fetching Reddit thread...\n[2/3] loading contexts: kova\n[3/3] classifying thread...\n"
	if got != want {
		t.Errorf("step output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestUpdateFormat(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{})
	r.Start(1)
	r.Step("classifying thread...")
	r.Update("reply")

	got := buf.String()
	want := "[1/1] classifying thread...\n  → reply\n"
	if got != want {
		t.Errorf("update output mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestSessionPath(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{})
	r.SessionPath("/tmp/sessions/abc")

	if got := buf.String(); got != "session: /tmp/sessions/abc\n" {
		t.Errorf("session path output: %q", got)
	}
}

func TestDonePrintsElapsed(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{})
	r.Start(1)
	r.Step("only step")
	// Backdate the start so Done sees a non-zero elapsed.
	r.cmdStart = time.Now().Add(-65 * time.Second)
	r.Done()

	out := buf.String()
	if !strings.Contains(out, "done in 1m05s\n") {
		t.Errorf("expected 'done in 1m05s' in output, got: %q", out)
	}
}

func TestDoneWithoutStartIsNoop(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{})
	r.Done()
	if got := buf.String(); got != "" {
		t.Errorf("expected no output, got %q", got)
	}
}

func TestWarnFormat(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{})
	r.Warn("could not save %s: %v", "foo.json", "disk full")

	if got := buf.String(); got != "warning: could not save foo.json: disk full\n" {
		t.Errorf("warn output: %q", got)
	}
}

func TestQuietSuppressesEverything(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{Quiet: true})
	r.Start(2)
	r.SessionPath("/tmp/x")
	r.Step("one")
	r.Update("annotation")
	r.Step("two")
	r.Warn("warn me")
	r.Done()

	if got := buf.String(); got != "" {
		t.Errorf("quiet mode should produce no output, got: %q", got)
	}
}

func TestNilReporterIsSafe(t *testing.T) {
	// Tests and helpers commonly pass nil when they don't need progress.
	// Every method must short-circuit instead of panicking.
	var r *Reporter
	r.Start(3)
	r.SetTotal(5)
	r.Step("one")
	r.Update("annotated")
	r.SessionPath("/tmp/x")
	r.Warn("careful")
	r.Done()
}

func TestNoProgressKeepsWarnings(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, Options{NoProgress: true})
	r.Start(2)
	r.SessionPath("/tmp/x")
	r.Step("one")
	r.Update("annotation")
	r.Warn("still here")
	r.Done()

	got := buf.String()
	if got != "warning: still here\n" {
		t.Errorf("no-progress should print only warnings, got: %q", got)
	}
}
