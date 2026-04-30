package research

import (
	"os"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func TestNormalizeSub(t *testing.T) {
	for _, in := range []string{"woocommerce", "r/woocommerce", "R/PostgreSQL", "go_lang"} {
		if _, err := NormalizeSub(in); err != nil {
			t.Errorf("NormalizeSub(%q) unexpected error: %v", in, err)
		}
	}
	for _, in := range []string{"../x", "bad/name", "", "a"} {
		if _, err := NormalizeSub(in); err == nil {
			t.Errorf("NormalizeSub(%q) expected error", in)
		}
	}
}

func TestRawCacheRoundTrip(t *testing.T) {
	root := t.TempDir()
	raw := &types.Research{
		Sub:       types.Subreddit{Name: "woocommerce"},
		TopWeek:   []types.Post{{Title: "A"}},
		Generated: time.Now().UTC(),
	}
	if err := SaveRaw(root, "woocommerce", raw); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRaw(root, "woocommerce", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Sub.Name != "woocommerce" || len(got.TopWeek) != 1 {
		t.Fatalf("cached raw = %+v", got)
	}
}

func TestLoadRawMissingOrStale(t *testing.T) {
	root := t.TempDir()
	got, err := LoadRaw(root, "missing", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing cache, got %+v", got)
	}

	raw := &types.Research{Sub: types.Subreddit{Name: "woocommerce"}}
	if err := SaveRaw(root, "woocommerce", raw); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(rawPath(root, "woocommerce"), old, old); err != nil {
		t.Fatal(err)
	}
	got, err = LoadRaw(root, "woocommerce", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected stale cache miss, got %+v", got)
	}
}
