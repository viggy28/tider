package contextbank

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateID(t *testing.T) {
	for _, id := range []string{"kova", "streambed", "my-project_1"} {
		if err := ValidateID(id); err != nil {
			t.Errorf("ValidateID(%q) unexpected error: %v", id, err)
		}
	}
	for _, id := range []string{"", "../secret", "bad/name", "-starts-dash", strings.Repeat("a", 65)} {
		if err := ValidateID(id); err == nil {
			t.Errorf("ValidateID(%q) expected error", id)
		}
	}
}

func TestListSortedMarkdownOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "streambed.md"), "streambed")
	writeFile(t, filepath.Join(dir, "kova.md"), "kova")
	writeFile(t, filepath.Join(dir, "notes.txt"), "ignore")
	if err := os.Mkdir(filepath.Join(dir, "folder.md"), 0o755); err != nil {
		t.Fatal(err)
	}

	entries, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries len = %d: %+v", len(entries), entries)
	}
	if entries[0].ID != "kova" || entries[1].ID != "streambed" {
		t.Fatalf("entries not sorted/filtered: %+v", entries)
	}
}

func TestListMissingDirIsEmpty(t *testing.T) {
	entries, err := List(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty list, got %+v", entries)
	}
}

func TestImportAndLoadByID(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "kova-source.md")
	writeFile(t, source, "# Kova\n\nContext body.")

	entry, err := Import(dir, "kova", source, false)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "kova" {
		t.Errorf("id = %q", entry.ID)
	}
	if !strings.Contains(entry.Body, "Context body") {
		t.Errorf("body = %q", entry.Body)
	}

	loaded, err := Load(dir, "kova")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Path != filepath.Join(dir, "kova.md") {
		t.Errorf("path = %q", loaded.Path)
	}
}

func TestImportRefusesOverwriteUnlessForced(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(t.TempDir(), "first.md")
	second := filepath.Join(t.TempDir(), "second.md")
	writeFile(t, first, "first")
	writeFile(t, second, "second")

	if _, err := Import(dir, "kova", first, false); err != nil {
		t.Fatal(err)
	}
	if _, err := Import(dir, "kova", second, false); err == nil {
		t.Fatal("expected overwrite error")
	}
	entry, err := Import(dir, "kova", second, true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(entry.Body) != "second" {
		t.Errorf("body = %q", entry.Body)
	}
}

func TestLoadPathRef(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(t.TempDir(), "adhoc.md")
	writeFile(t, source, "ad hoc")

	entry, err := Load(dir, source)
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "adhoc" {
		t.Errorf("id = %q", entry.ID)
	}
	if strings.TrimSpace(entry.Body) != "ad hoc" {
		t.Errorf("body = %q", entry.Body)
	}
}

func TestEnsureCreatesEmptyContext(t *testing.T) {
	dir := t.TempDir()
	path, err := Ensure(dir, "kova")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "kova.md") {
		t.Errorf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("expected empty file, got %q", data)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
