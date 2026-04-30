package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeEnv is a small helper that writes content to <dir>/.env and
// returns the path.
func writeEnv(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDotenvBasic(t *testing.T) {
	t.Setenv("TIDER_TEST_BASIC", "")
	os.Unsetenv("TIDER_TEST_BASIC")
	path := writeEnv(t, t.TempDir(), "TIDER_TEST_BASIC=hello\n")
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TIDER_TEST_BASIC"); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestLoadDotenvWithExport(t *testing.T) {
	os.Unsetenv("TIDER_TEST_EXPORT")
	path := writeEnv(t, t.TempDir(), "export TIDER_TEST_EXPORT=world\n")
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TIDER_TEST_EXPORT"); got != "world" {
		t.Errorf("got %q, want world", got)
	}
}

func TestLoadDotenvStripsQuotes(t *testing.T) {
	os.Unsetenv("TIDER_TEST_DQ")
	os.Unsetenv("TIDER_TEST_SQ")
	os.Unsetenv("TIDER_TEST_NOQ")
	body := `TIDER_TEST_DQ="double quoted"
TIDER_TEST_SQ='single quoted'
TIDER_TEST_NOQ=plain
`
	path := writeEnv(t, t.TempDir(), body)
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"TIDER_TEST_DQ":  "double quoted",
		"TIDER_TEST_SQ":  "single quoted",
		"TIDER_TEST_NOQ": "plain",
	}
	for k, want := range cases {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestLoadDotenvIgnoresCommentsAndBlanks(t *testing.T) {
	os.Unsetenv("TIDER_TEST_AFTER_COMMENT")
	body := `
# This is a comment
   # Indented comment

TIDER_TEST_AFTER_COMMENT=ok

`
	path := writeEnv(t, t.TempDir(), body)
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TIDER_TEST_AFTER_COMMENT"); got != "ok" {
		t.Errorf("got %q, want ok", got)
	}
}

func TestLoadDotenvDoesNotOverrideExistingEnv(t *testing.T) {
	t.Setenv("TIDER_TEST_EXISTING", "from-shell")
	path := writeEnv(t, t.TempDir(), "TIDER_TEST_EXISTING=from-dotenv\n")
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TIDER_TEST_EXISTING"); got != "from-shell" {
		t.Errorf("real env should win: got %q, want from-shell", got)
	}
}

func TestLoadDotenvMissingFileIsOK(t *testing.T) {
	if err := loadDotenv(filepath.Join(t.TempDir(), "does-not-exist.env")); err != nil {
		t.Errorf("missing file should not error, got %v", err)
	}
}

func TestLoadDotenvMalformedLinesSkipped(t *testing.T) {
	os.Unsetenv("TIDER_TEST_GOOD")
	body := `
no_equals_sign_here
=no_key_either
TIDER_TEST_GOOD=yes
`
	path := writeEnv(t, t.TempDir(), body)
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TIDER_TEST_GOOD"); got != "yes" {
		t.Errorf("good line after malformed lines lost: got %q", got)
	}
}

func TestLoadDotenvTrimsKeyAndValueWhitespace(t *testing.T) {
	os.Unsetenv("TIDER_TEST_SPACES")
	path := writeEnv(t, t.TempDir(), "  TIDER_TEST_SPACES   =   trimmed   \n")
	if err := loadDotenv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("TIDER_TEST_SPACES"); got != "trimmed" {
		t.Errorf("got %q, want trimmed", got)
	}
}

func TestStripQuotes(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`"hello"`, "hello"},
		{`'hello'`, "hello"},
		{`hello`, "hello"},
		{`"unclosed`, `"unclosed`},
		{`unclosed"`, `unclosed"`},
		{``, ``},
		{`"`, `"`},
		{`""`, ``},
	}
	for _, c := range cases {
		if got := stripQuotes(c.in); got != c.want {
			t.Errorf("stripQuotes(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
