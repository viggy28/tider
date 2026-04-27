package reddit

import (
	"encoding/json"
	"fmt"

	"github.com/viggy28/tider/internal/types"
)

type aboutResponse struct {
	Data struct {
		DisplayName       string `json:"display_name"`
		Subscribers       int    `json:"subscribers"`
		Description       string `json:"description"`
		PublicDescription string `json:"public_description"`
		Over18            bool   `json:"over18"`
		URL               string `json:"url"`
	} `json:"data"`
}

func parseAbout(data []byte) (types.Subreddit, error) {
	var r aboutResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return types.Subreddit{}, fmt.Errorf("parse about: %w", err)
	}
	return types.Subreddit{
		Name:              r.Data.DisplayName,
		Subscribers:       r.Data.Subscribers,
		Description:       r.Data.Description,
		PublicDescription: r.Data.PublicDescription,
		Over18:            r.Data.Over18,
		URL:               r.Data.URL,
	}, nil
}

type rulesResponse struct {
	Rules []types.Rule `json:"rules"`
}

func parseRules(data []byte) ([]types.Rule, error) {
	var r rulesResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	if r.Rules == nil {
		return []types.Rule{}, nil
	}
	return r.Rules, nil
}

type wikiResponse struct {
	Data struct {
		ContentMD string `json:"content_md"`
	} `json:"data"`
}

func parseWiki(data []byte) (string, error) {
	var r wikiResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("parse wiki: %w", err)
	}
	return r.Data.ContentMD, nil
}

type listingResponse struct {
	Data struct {
		Children []struct {
			Data types.Post `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

func parseListing(data []byte) ([]types.Post, error) {
	var r listingResponse
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse listing: %w", err)
	}
	out := make([]types.Post, 0, len(r.Data.Children))
	for _, c := range r.Data.Children {
		out = append(out, c.Data)
	}
	return out, nil
}

// parseFlairs accepts the array Reddit returns on success and treats the
// `{"json":{"errors":[["USER_REQUIRED",...]]}}` envelope (200 with error
// payload, returned when the endpoint requires auth) as soft-fail → nil.
func parseFlairs(data []byte) ([]types.Flair, error) {
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, fmt.Errorf("parse flairs: %w", err)
	}
	if _, ok := probe.([]any); !ok {
		return nil, nil
	}
	var r []types.Flair
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse flairs: %w", err)
	}
	return r, nil
}
