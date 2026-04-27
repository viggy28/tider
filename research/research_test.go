package research

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/viggy28/tider/internal/types"
)

type fakeFetcher struct {
	about  types.Subreddit
	rules  []types.Rule
	wiki   string
	topW   []types.Post
	topM   []types.Post
	hot    []types.Post
	flairs []types.Flair
}

func (f *fakeFetcher) About(_ context.Context, _ string, _ bool) (types.Subreddit, error) {
	return f.about, nil
}
func (f *fakeFetcher) Rules(_ context.Context, _ string, _ bool) ([]types.Rule, error) {
	return f.rules, nil
}
func (f *fakeFetcher) WikiRules(_ context.Context, _ string, _ bool) (string, error) {
	return f.wiki, nil
}
func (f *fakeFetcher) Top(_ context.Context, _, period string, _ bool) ([]types.Post, error) {
	if period == "week" {
		return f.topW, nil
	}
	return f.topM, nil
}
func (f *fakeFetcher) Hot(_ context.Context, _ string, _ bool) ([]types.Post, error) {
	return f.hot, nil
}
func (f *fakeFetcher) Flairs(_ context.Context, _ string, _ bool) ([]types.Flair, error) {
	return f.flairs, nil
}

func TestForAssemblesBundle(t *testing.T) {
	f := &fakeFetcher{
		about:  types.Subreddit{Name: "golang", Subscribers: 250000},
		rules:  []types.Rule{{ShortName: "No spam"}},
		wiki:   "Detailed rules",
		topW:   []types.Post{{ID: "a"}},
		topM:   []types.Post{{ID: "b"}},
		hot:    []types.Post{{ID: "c", Stickied: true}, {ID: "d", Stickied: false}},
		flairs: []types.Flair{{ID: "f1", Text: "discussion"}},
	}
	notes := &types.SubsConfig{Subs: []types.SubNotes{{Name: "golang", Tone: "terse"}}}

	r, err := For(context.Background(), f, notes, "golang", false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Sub.Name != "golang" {
		t.Fatalf("sub: %+v", r.Sub)
	}
	if r.Notes == nil || r.Notes.Tone != "terse" {
		t.Fatalf("notes: %+v", r.Notes)
	}
	if len(r.Stickies) != 1 || r.Stickies[0].ID != "c" {
		t.Fatalf("stickies: %+v", r.Stickies)
	}
	if r.WikiRules != "Detailed rules" {
		t.Fatalf("wiki: %q", r.WikiRules)
	}
	if r.Generated.IsZero() {
		t.Fatal("generated not set")
	}
}

func TestForNotesAbsent(t *testing.T) {
	f := &fakeFetcher{about: types.Subreddit{Name: "obscuresub"}}
	r, err := For(context.Background(), f, nil, "obscuresub", false)
	if err != nil {
		t.Fatal(err)
	}
	if r.Notes != nil {
		t.Fatalf("expected nil notes, got %+v", r.Notes)
	}
}

func TestLoadNotesMissing(t *testing.T) {
	cfg, err := LoadNotes(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatalf("expected nil cfg, got %+v", cfg)
	}
}

func TestLoadNotesParse(t *testing.T) {
	cfg, err := LoadNotes("testdata/subreddits.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg == nil || len(cfg.Subs) != 2 {
		t.Fatalf("cfg: %+v", cfg)
	}
	g := FindSub(cfg, "golang")
	if g == nil || g.Tone == "" || !g.Flair.Required {
		t.Fatalf("golang notes: %+v", g)
	}
	// case-insensitive match
	if FindSub(cfg, "POSTGRESQL") == nil {
		t.Fatal("case-insensitive lookup failed")
	}
	if FindSub(cfg, "doesnotexist") != nil {
		t.Fatal("expected nil for unknown sub")
	}
}
