package intake

import (
	"context"

	"github.com/viggy28/tider/internal/llm"
)

// fakeProvider returns a canned LLM response, capturing the request for
// assertions. Every test that needs a Provider uses this.
type fakeProvider struct {
	name        string
	response    string
	err         error
	gotRequests []llm.Request
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Complete(_ context.Context, req llm.Request) (*llm.Response, error) {
	f.gotRequests = append(f.gotRequests, req)
	if f.err != nil {
		return nil, f.err
	}
	return &llm.Response{Content: f.response, InputTokens: 10, OutputTokens: 20}, nil
}

const cannedBriefJSON = `{
  "title": "Streambed",
  "summary": "WAL-native CDC tool for Postgres that pipes data into Iceberg/Parquet on S3.",
  "highlights": [
    "Single Go binary",
    "Reads directly from the Postgres WAL",
    "Stores in Iceberg/Parquet on S3",
    "Queries via DuckDB over the Postgres wire protocol",
    "No external catalog required"
  ],
  "audience": "Postgres practitioners and data engineers",
  "links": ["https://github.com/example/streambed"]
}`
