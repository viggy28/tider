package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/viggy28/tider/internal/types"
)

// threadTopComments is the cap on comments returned per thread fetch —
// enough conversational context for reply drafting without flooding the
// LLM prompt. Selected by score across all depths.
const threadTopComments = 20

// FetchThread retrieves a Reddit thread (post + a flattened, score-ranked
// slice of comments). When sub is empty (e.g., from a redd.it short
// link), uses the global /comments/<id>.json endpoint, which returns the
// same payload regardless of subreddit.
//
// Thread fetches are intentionally NOT cached: comment churn matters for
// reply drafting, and each tider reply invocation hits Reddit fresh. The
// retry/backoff in Client.get applies (429/5xx).
func (c *Client) FetchThread(ctx context.Context, sub, postID string) (*types.Thread, error) {
	var path string
	if sub == "" {
		path = "/comments/" + postID + ".json?raw_json=1"
	} else {
		path = "/r/" + sub + "/comments/" + postID + ".json?raw_json=1"
	}
	body, _, err := c.get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetch thread %s: %w", postID, err)
	}
	return parseThread(body)
}

func parseThread(data []byte) (*types.Thread, error) {
	var listings []json.RawMessage
	if err := json.Unmarshal(data, &listings); err != nil {
		return nil, fmt.Errorf("thread: parse top-level: %w", err)
	}
	if len(listings) < 2 {
		return nil, fmt.Errorf("thread: expected 2 listings (post + comments), got %d", len(listings))
	}

	post, err := parsePost(listings[0])
	if err != nil {
		return nil, err
	}

	var commentsListing rawListing
	if err := json.Unmarshal(listings[1], &commentsListing); err != nil {
		return nil, fmt.Errorf("thread: parse comments listing: %w", err)
	}

	var all []types.Comment
	for _, child := range commentsListing.Data.Children {
		if child.Kind != "t1" {
			continue // skip "more" continuation stubs and any non-comment kinds
		}
		flattenComment(child.Data, &all)
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].Score > all[j].Score
	})
	if len(all) > threadTopComments {
		all = all[:threadTopComments]
	}

	outboundURL := ""
	if !post.IsSelf {
		outboundURL = post.URL
	}

	return &types.Thread{
		URL:         "https://www.reddit.com" + post.Permalink,
		Subreddit:   post.Subreddit,
		PostID:      post.ID,
		Title:       post.Title,
		Body:        post.Selftext,
		Author:      post.Author,
		Flair:       post.LinkFlairText,
		OutboundURL: outboundURL,
		Comments:    all,
		FetchedAt:   time.Now().UTC(),
	}, nil
}

func parsePost(raw json.RawMessage) (*rawPost, error) {
	var listing rawListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		return nil, fmt.Errorf("thread: parse post listing: %w", err)
	}
	if len(listing.Data.Children) == 0 {
		return nil, fmt.Errorf("thread: post listing has no children")
	}
	var post rawPost
	if err := json.Unmarshal(listing.Data.Children[0].Data, &post); err != nil {
		return nil, fmt.Errorf("thread: parse post data: %w", err)
	}
	return &post, nil
}

// flattenComment appends a comment plus all its nested replies (any depth)
// to out. Deleted/removed bodies are skipped at the leaf — but we still
// recurse into their replies in case useful content lives below.
func flattenComment(data json.RawMessage, out *[]types.Comment) {
	var c rawComment
	if err := json.Unmarshal(data, &c); err != nil {
		return // malformed → skip silently
	}
	if c.Body != "" && c.Body != "[deleted]" && c.Body != "[removed]" {
		*out = append(*out, types.Comment{
			ID:         c.ID,
			ParentID:   c.ParentID,
			Author:     c.Author,
			Body:       c.Body,
			Score:      c.Score,
			CreatedUTC: c.CreatedUTC,
		})
	}
	// Reddit returns Replies as either a Listing object or an empty string.
	// Distinguish by leading byte.
	if len(c.Replies) > 0 && c.Replies[0] == '{' {
		var nested rawListing
		if err := json.Unmarshal(c.Replies, &nested); err == nil {
			for _, child := range nested.Data.Children {
				if child.Kind == "t1" {
					flattenComment(child.Data, out)
				}
			}
		}
	}
}

// Reddit JSON shapes — kept minimal, only the fields we use.

type rawListing struct {
	Data struct {
		Children []rawChild `json:"children"`
	} `json:"data"`
}

type rawChild struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

type rawPost struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	Selftext      string `json:"selftext"`
	Author        string `json:"author"`
	Subreddit     string `json:"subreddit"`
	LinkFlairText string `json:"link_flair_text"`
	URL           string `json:"url"`
	IsSelf        bool   `json:"is_self"`
	Permalink     string `json:"permalink"`
}

type rawComment struct {
	ID         string          `json:"id"`
	ParentID   string          `json:"parent_id"`
	Author     string          `json:"author"`
	Body       string          `json:"body"`
	Score      int             `json:"score"`
	CreatedUTC float64         `json:"created_utc"`
	Replies    json.RawMessage `json:"replies"` // Listing object or empty string
}
