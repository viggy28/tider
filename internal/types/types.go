// Package types holds the shared domain types used across tider packages.
package types

import "time"

type Subreddit struct {
	Name              string `json:"name"`
	Subscribers       int    `json:"subscribers"`
	Description       string `json:"description,omitempty"`
	PublicDescription string `json:"public_description,omitempty"`
	Over18            bool   `json:"over_18"`
	URL               string `json:"url,omitempty"`
}

type Rule struct {
	ShortName   string `json:"short_name"`
	Description string `json:"description"`
	Kind        string `json:"kind,omitempty"`
	Priority    int    `json:"priority"`
}

type Post struct {
	ID            string  `json:"id"`
	Title         string  `json:"title"`
	Author        string  `json:"author,omitempty"`
	Score         int     `json:"score"`
	NumComments   int     `json:"num_comments"`
	URL           string  `json:"url,omitempty"`
	Permalink     string  `json:"permalink,omitempty"`
	Selftext      string  `json:"selftext,omitempty"`
	IsSelf        bool    `json:"is_self"`
	Stickied      bool    `json:"stickied"`
	LinkFlairText string  `json:"link_flair_text,omitempty"`
	CreatedUTC    float64 `json:"created_utc"`
}

type Flair struct {
	ID           string `json:"id"`
	Text         string `json:"text"`
	TextEditable bool   `json:"text_editable"`
}

// SubNotes is the curated knowledge layer for a single subreddit, loaded
// from subreddits.yaml. All fields are optional; free-form Notes is the
// catch-all for observations that don't fit a structured field.
type SubNotes struct {
	Name               string     `yaml:"name" json:"name"`
	Tone               string     `yaml:"tone,omitempty" json:"tone,omitempty"`
	SelfPromoTolerance string     `yaml:"self_promo_tolerance,omitempty" json:"self_promo_tolerance,omitempty"`
	FormatPreferences  []string   `yaml:"format_preferences,omitempty" json:"format_preferences,omitempty"`
	Flair              FlairNotes `yaml:"flair" json:"flair"`
	DoNot              []string   `yaml:"do_not,omitempty" json:"do_not,omitempty"`
	Notes              string     `yaml:"notes,omitempty" json:"notes,omitempty"`
	ExemplarURLs       []string   `yaml:"exemplar_urls,omitempty" json:"exemplar_urls,omitempty"`
}

type FlairNotes struct {
	Required bool     `yaml:"required" json:"required"`
	Common   []string `yaml:"common,omitempty" json:"common,omitempty"`
}

type SubsConfig struct {
	Subs []SubNotes `yaml:"subs"`
}

// Research is the assembled per-sub bundle: live Reddit data + curated notes.
type Research struct {
	Sub       Subreddit `json:"sub"`
	Notes     *SubNotes `json:"notes,omitempty"`
	Rules     []Rule    `json:"rules"`
	WikiRules string    `json:"wiki_rules,omitempty"`
	TopWeek   []Post    `json:"top_week"`
	TopMonth  []Post    `json:"top_month"`
	Hot       []Post    `json:"hot"`
	Stickies  []Post    `json:"stickies"`
	Flairs    []Flair   `json:"flairs"`
	Generated time.Time `json:"generated"`
}
