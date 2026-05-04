package intake

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/viggy28/tider/internal/types"
)

// FromNote builds a Brief directly from operator-supplied text without
// running the LLM extractor. A note is the operator's *intent* ("ask
// sellers whether AI listing images hurt buyer trust, don't pitch
// Kova"), not source material to polish — running extraction over it
// would invent structure and bias the drafter.
//
// Title is a deterministic placeholder ("Operator note") so downstream
// code that requires a non-empty title (lastdraft, snapshot validation)
// keeps working.
func FromNote(note string) (*types.Brief, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return nil, fmt.Errorf("note is empty")
	}
	return &types.Brief{
		Source:     types.BriefSource{Mode: "note"},
		Title:      "Operator note",
		Summary:    note,
		RawContent: note,
		CreatedAt:  time.Now().UTC(),
	}, nil
}

// FromStdin reads up to maxBytes from r and builds a Brief without
// extraction — same operator-intent semantics as FromNote. Stdin is
// almost always either a one-liner from `echo … | tider post` or a
// larger blob from `pbpaste | tider post`; in either case the operator
// wrote it, so it should land in the prompt as-is, not be re-summarized.
func FromStdin(r io.Reader, maxBytes int64) (*types.Brief, error) {
	if r == nil {
		return nil, fmt.Errorf("stdin reader is nil")
	}
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	raw, err := io.ReadAll(io.LimitReader(r, maxBytes))
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return nil, fmt.Errorf("stdin is empty")
	}
	return &types.Brief{
		Source:     types.BriefSource{Mode: "stdin", Value: "-"},
		Title:      "Operator note",
		Summary:    body,
		RawContent: body,
		CreatedAt:  time.Now().UTC(),
	}, nil
}
