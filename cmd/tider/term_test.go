package main

import (
	"strings"
	"testing"
)

func TestRenderLineHeadings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"# Heading 1", ansiBold + ansiMagenta + "Heading 1" + ansiReset},
		{"## Heading 2", ansiBold + ansiGreen + "Heading 2" + ansiReset},
		{"### Heading 3", ansiBold + ansiCyan + "Heading 3" + ansiReset},
	}
	for _, c := range cases {
		if got := renderLine(c.in); got != c.want {
			t.Errorf("renderLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRenderLineHorizontalRule(t *testing.T) {
	got := renderLine("---")
	if !strings.Contains(got, "─") {
		t.Errorf("hr should render as box-drawing chars, got %q", got)
	}
	if !strings.Contains(got, ansiDim) {
		t.Errorf("hr should be dim, got %q", got)
	}
}

func TestRenderLineBlockquote(t *testing.T) {
	got := renderLine("> reasoning here")
	if !strings.Contains(got, "│") {
		t.Errorf("blockquote should have left bar, got %q", got)
	}
	if !strings.Contains(got, "reasoning here") {
		t.Errorf("blockquote text missing: %q", got)
	}
	if !strings.Contains(got, ansiDim) {
		t.Errorf("blockquote should be dim, got %q", got)
	}
}

func TestRenderLineListItem(t *testing.T) {
	got := renderLine("- some item")
	if !strings.Contains(got, "•") {
		t.Errorf("list bullet should be •, got %q", got)
	}
	if !strings.Contains(got, "some item") {
		t.Errorf("list text missing: %q", got)
	}
}

func TestApplyInlineBold(t *testing.T) {
	got := applyInline("normal **bold text** normal")
	if !strings.Contains(got, ansiBold+"bold text"+ansiReset) {
		t.Errorf("bold not wrapped, got %q", got)
	}
	if !strings.Contains(got, "normal") {
		t.Errorf("surrounding text dropped: %q", got)
	}
}

func TestApplyInlineCode(t *testing.T) {
	got := applyInline("see `1.1` for details")
	if !strings.Contains(got, ansiCyan+"1.1"+ansiReset) {
		t.Errorf("inline code not styled, got %q", got)
	}
}

func TestApplyInlineItalicUnderscore(t *testing.T) {
	got := applyInline("a _word_ here")
	if !strings.Contains(got, ansiItalic+"word"+ansiReset) {
		t.Errorf("underscore italic missed, got %q", got)
	}
}

func TestApplyInlineItalicStar(t *testing.T) {
	got := applyInline("a *phrase* here")
	if !strings.Contains(got, ansiItalic+"phrase"+ansiReset) {
		t.Errorf("star italic missed, got %q", got)
	}
}

func TestApplyInlineDoesNotEatBoldAsItalic(t *testing.T) {
	// **bold** should not be partially matched as italic.
	got := applyInline("**Pick:** something")
	if strings.Contains(got, ansiItalic) {
		t.Errorf("bold leaked into italic match: %q", got)
	}
	if !strings.Contains(got, ansiBold+"Pick:"+ansiReset) {
		t.Errorf("bold not applied: %q", got)
	}
}

func TestApplyInlineUnderscoreDoesNotMatchIdentifiers(t *testing.T) {
	// snake_case identifiers should not be italicized by the underscore rule.
	got := applyInline("the my_var_name field")
	if strings.Contains(got, ansiItalic) {
		t.Errorf("snake_case wrongly italicized: %q", got)
	}
}

func TestResolveRenderHonorsExplicitFlag(t *testing.T) {
	// Explicit values pass through regardless of TTY state.
	for _, v := range []string{"markdown", "json"} {
		if got := resolveRender(v); got != v {
			t.Errorf("resolveRender(%q) = %q, want %q", v, got, v)
		}
	}
}

func TestResolveRenderEmptyFallsBackToTTYDetection(t *testing.T) {
	// Tests can't easily simulate a real TTY, but we can verify the empty-string
	// path delegates to isTerminal() rather than returning the literal flag.
	got := resolveRender("")
	if got != "json" && got != "markdown" {
		t.Errorf("resolveRender(\"\") = %q, want json or markdown", got)
	}
	// In a `go test` run stdout is a pipe (not a TTY), so empty should
	// resolve to json.
	if got != "json" {
		t.Logf("test stdout is unexpectedly a TTY; got %q", got)
	}
}

func TestRenderTerminalRoundTripFromDraftMarkdown(t *testing.T) {
	// Real-shape input: the kind of lines draft.RenderMarkdown produces.
	in := strings.Join([]string{
		"# Drafts for r/golang",
		"",
		"**Brief:** Streambed",
		"",
		"---",
		"",
		"## openai · gpt-5 · risk: low",
		"",
		"### Pick: angle 2 / title 2.1 / body 2.1",
		"",
		"> reasoning here",
		"",
		"**Title:** Some title",
		"",
		"### Alternatives",
		"",
		"**Angle 1** — premise *(recommended angle)*",
		"*Hook:* what surprised me",
		"",
		"Titles:",
		"- `1.1` First title ← picked",
		"- `1.2` Second title",
		"",
		"_tokens: in=1500 out=800_",
	}, "\n")
	out := renderTerminal(in)

	mustContain := []string{
		"Drafts for r/golang",
		"openai · gpt-5",
		"Pick:",
		"reasoning here",
		"Streambed",
		"recommended angle",
		"what surprised me",
		"First title",
		"in=1500 out=800",
		ansiBold,
		ansiCyan,
		ansiGreen,
		ansiMagenta,
		ansiDim,
		ansiItalic,
		"•",
		"│",
		"─",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("rendered output missing %q\n--- output ---\n%s", s, out)
		}
	}
	mustNotContain := []string{"**", "###", "## ", "# ", "> ", "- `"}
	for _, s := range mustNotContain {
		if strings.Contains(out, s) {
			t.Errorf("raw markdown leftover %q in output\n--- output ---\n%s", s, out)
		}
	}
}
