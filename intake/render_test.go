package intake

import (
	"strings"
	"testing"

	"github.com/viggy28/tider/internal/types"
)

func TestRenderMarkdownFullBrief(t *testing.T) {
	b := &types.Brief{
		Source:  types.BriefSource{Mode: "url", Value: "https://github.com/viggy28/kova"},
		Title:   "Kova — Build Plan",
		Summary: "Etsy listing video tool for handmade sellers.",
		Highlights: []string{
			"Two-stage MVP",
			"1080x1080 MP4 under 100MB",
		},
		Audience:   "Solo Etsy sellers who need listing videos.",
		Links:      []string{"https://github.com/viggy28/kova"},
		RawContent: "# Kova\n\nthousands of bytes here that we should NOT render in markdown",
	}

	md := RenderMarkdown(b)

	wantSubstrings := []string{
		"# Kova — Build Plan",                               // h1
		"Etsy listing video tool for handmade sellers.",     // summary
		"**Audience:** Solo Etsy sellers",                   // audience bold
		"## Highlights",                                     // section
		"- Two-stage MVP",                                   // list item
		"- 1080x1080 MP4 under 100MB",                       // list item
		"## Links",                                          // section
		"- https://github.com/viggy28/kova",                 // link item
		"---",                                               // separator before footer
		"*Source: url — https://github.com/viggy28/kova*",   // source footer
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(md, s) {
			t.Errorf("rendered markdown missing %q\n--- output ---\n%s", s, md)
		}
	}

	// raw_content must NOT leak into markdown; it's too long to be useful here.
	if strings.Contains(md, "thousands of bytes here") {
		t.Error("raw_content leaked into markdown render")
	}
}

func TestRenderMarkdownOmitsEmptySections(t *testing.T) {
	b := &types.Brief{
		Title: "Bare brief",
	}
	md := RenderMarkdown(b)

	if !strings.Contains(md, "# Bare brief") {
		t.Error("title missing")
	}
	for _, s := range []string{"## Highlights", "## Links", "**Audience:**", "---"} {
		if strings.Contains(md, s) {
			t.Errorf("empty-field section should be omitted but found %q", s)
		}
	}
}

func TestRenderMarkdownHandlesMissingTitle(t *testing.T) {
	b := &types.Brief{
		Summary: "Has a summary but no title",
	}
	md := RenderMarkdown(b)
	if !strings.Contains(md, "# (untitled brief)") {
		t.Errorf("expected fallback title, got:\n%s", md)
	}
	if !strings.Contains(md, "Has a summary but no title") {
		t.Error("summary lost")
	}
}

func TestRenderMarkdownNilSafe(t *testing.T) {
	if got := RenderMarkdown(nil); got != "" {
		t.Errorf("nil brief should render empty, got %q", got)
	}
}

func TestRenderMarkdownFileSourceFooter(t *testing.T) {
	b := &types.Brief{
		Title:  "From a local file",
		Source: types.BriefSource{Mode: "file", Value: "/tmp/notes.md"},
	}
	md := RenderMarkdown(b)
	if !strings.Contains(md, "*Source: file — /tmp/notes.md*") {
		t.Errorf("file source footer missing or wrong:\n%s", md)
	}
}
