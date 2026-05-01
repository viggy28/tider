package reply

import (
	"fmt"
	"strings"

	"github.com/viggy28/tider/contextbank"
	"github.com/viggy28/tider/internal/types"
)

// LoadContexts resolves --context flag values into snapshots. Each ref
// goes through contextbank.Load which dispatches to either bank lookup
// (looking under dir for <ref>.md) or direct file path, based on the
// shape of the ref.
//
// dir is the bank directory (typically contextbank.DefaultDir(); tests
// pass a temp dir). An empty refs slice returns nil without error —
// replies don't require contexts.
//
// Each returned LoadedReplyContext is labeled with Source ("bank" or
// "path") so the session artifact records how it was resolved.
func LoadContexts(dir string, refs []string) ([]types.LoadedReplyContext, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	out := make([]types.LoadedReplyContext, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		entry, err := contextbank.Load(dir, ref)
		if err != nil {
			return nil, fmt.Errorf("context %q: %w", ref, err)
		}
		out = append(out, types.LoadedReplyContext{
			ID:     entry.ID,
			Source: classifyContextSource(ref),
			Path:   entry.Path,
			Body:   entry.Body,
		})
	}
	return out, nil
}

// classifyContextSource mirrors contextbank.looksLikePath (which is
// unexported) so we can label sources without leaking the internal
// predicate. Bank IDs are validated to alphanumeric + dash + underscore
// (no dots, slashes, or leading dots), so:
//   - "kova"           → bank
//   - "kova-spec"      → bank
//   - "./kova.md"      → path (leading dot)
//   - "/abs/notes.md"  → path (slash)
//   - "kova.md"        → path (extension)
func classifyContextSource(ref string) string {
	if strings.ContainsRune(ref, '/') || strings.HasPrefix(ref, ".") || strings.Contains(ref, ".") {
		return "path"
	}
	return "bank"
}
