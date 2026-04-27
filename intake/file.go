package intake

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/viggy28/tider/internal/types"
)

// FromFile reads path (capped at i.MaxBytes) and runs LLM extraction.
func (i *Intake) FromFile(ctx context.Context, path string) (*types.Brief, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, i.MaxBytes))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return i.extract(ctx, types.BriefSource{Mode: "file", Value: path}, string(raw))
}
