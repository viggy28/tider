package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadDotenv reads a .env file at path (if it exists) and sets each
// KEY=value pair as an environment variable, but only if the variable
// isn't already set. Explicit `export KEY=value` lines are supported but
// the `export` keyword is optional; surrounding double or single quotes
// on values are stripped; lines starting with # and blank lines are
// ignored.
//
// Real env (passed by the parent shell) always wins over .env. A missing
// file is not an error — returns nil so call sites can opportunistically
// load without checking.
//
// Why we ship this: `source .env` in shells doesn't export plain
// `KEY=value` lines to subprocesses, which silently breaks the binary.
// Auto-loading at startup matches the convention used by most CLI tools
// that read API keys (rails, node apps, etc.).
func loadDotenv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue // malformed (no key, or no =) — skip silently
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		val = stripQuotes(val)

		// Don't override real env that the user already exported. Their
		// explicit choice wins over a stale .env value.
		if _, present := os.LookupEnv(key); present {
			continue
		}
		if err := os.Setenv(key, val); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	return scanner.Err()
}

func stripQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	first, last := s[0], s[len(s)-1]
	if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// autoloadEnv applies loadDotenv to the conventional locations: the
// current working directory's .env first (highest priority after real
// env), then ~/.tider/.env as a fallback for users who keep keys in a
// per-tool-global file. cwd wins because the skip-if-already-set logic
// in loadDotenv leaves earlier sets in place.
func autoloadEnv() {
	// Errors here are intentionally swallowed — a malformed .env shouldn't
	// block the user from running the tool. They'll see "API_KEY not set"
	// from the provider constructor instead, which is a clearer signal.
	_ = loadDotenv(".env")
	if home, err := os.UserHomeDir(); err == nil {
		_ = loadDotenv(filepath.Join(home, ".tider", ".env"))
	}
}
