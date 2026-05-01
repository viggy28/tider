package reply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeContext is a small helper that drops a context file at the given path.
func writeContext(t *testing.T, dir, name, body string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadContextsBankAndPathMixed(t *testing.T) {
	bankDir := t.TempDir()
	writeContext(t, bankDir, "kova.md", "# kova\n\nproject context for kova")

	pathDir := t.TempDir()
	pathFile := writeContext(t, pathDir, "notes.md", "# notes\n\nad hoc context")

	got, err := LoadContexts(bankDir, []string{"kova", pathFile})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}

	// First: bank ref
	if got[0].ID != "kova" {
		t.Errorf("first ID = %q, want kova", got[0].ID)
	}
	if got[0].Source != "bank" {
		t.Errorf("first source = %q, want bank", got[0].Source)
	}
	if !strings.Contains(got[0].Body, "project context for kova") {
		t.Errorf("first body lost: %q", got[0].Body)
	}

	// Second: path ref
	if got[1].Source != "path" {
		t.Errorf("second source = %q, want path", got[1].Source)
	}
	if got[1].Path != pathFile {
		t.Errorf("second path = %q, want %q", got[1].Path, pathFile)
	}
	if !strings.Contains(got[1].Body, "ad hoc context") {
		t.Errorf("second body lost: %q", got[1].Body)
	}
}

func TestLoadContextsEmptyRefs(t *testing.T) {
	got, err := LoadContexts(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("empty refs should return nil/empty, got %v", got)
	}
}

func TestLoadContextsEmptyStringsSkipped(t *testing.T) {
	bankDir := t.TempDir()
	writeContext(t, bankDir, "kova.md", "body")

	got, err := LoadContexts(bankDir, []string{"", "kova", "   "})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 entry, got %d", len(got))
	}
}

func TestLoadContextsMissingBankEntryErrors(t *testing.T) {
	bankDir := t.TempDir()
	_, err := LoadContexts(bankDir, []string{"does-not-exist"})
	if err == nil || !strings.Contains(err.Error(), `context "does-not-exist"`) {
		t.Errorf("expected wrapped not-found error, got %v", err)
	}
}

func TestLoadContextsMissingPathErrors(t *testing.T) {
	bankDir := t.TempDir()
	_, err := LoadContexts(bankDir, []string{"./missing-file.md"})
	if err == nil || !strings.Contains(err.Error(), `./missing-file.md`) {
		t.Errorf("expected wrapped path error, got %v", err)
	}
}

func TestClassifyContextSource(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"kova", "bank"},
		{"kova-spec", "bank"},
		{"my_context", "bank"},
		{"./kova.md", "path"},
		{"./kova", "path"},          // dot prefix
		{"/abs/path/notes.md", "path"},
		{"kova.md", "path"},         // has extension
		{"sub/dir/file.md", "path"}, // slash
	}
	for _, c := range cases {
		if got := classifyContextSource(c.in); got != c.want {
			t.Errorf("classifyContextSource(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadContextsPreservesOrder(t *testing.T) {
	bankDir := t.TempDir()
	writeContext(t, bankDir, "first.md", "1")
	writeContext(t, bankDir, "second.md", "2")
	writeContext(t, bankDir, "third.md", "3")

	got, err := LoadContexts(bankDir, []string{"third", "first", "second"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].ID != "third" || got[1].ID != "first" || got[2].ID != "second" {
		t.Errorf("order lost: %s/%s/%s", got[0].ID, got[1].ID, got[2].ID)
	}
}
