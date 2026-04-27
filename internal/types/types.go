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

// BriefSource records how a Brief was created.
type BriefSource struct {
	Mode  string `json:"mode"`  // "url" | "file" | "topic"
	Value string `json:"value"` // the URL, file path, or topic string
}

// Brief is the structured intake output: source material distilled into
// fields that drafting downstream consumes. Title/Summary/Highlights/
// Audience/Links are LLM-extracted from the raw input; RawContent is
// preserved verbatim so later steps can pull additional context.
type Brief struct {
	Source     BriefSource `json:"source"`
	Title      string      `json:"title"`
	Summary    string      `json:"summary"`
	Highlights []string    `json:"highlights"`
	Audience   string      `json:"audience,omitempty"`
	Links      []string    `json:"links,omitempty"`
	RawContent string      `json:"raw_content,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
}

// Risk ratings on a Draft. "refuse" means the LLM declined to generate
// content because the sub's culture would reject this kind of post; the
// reason goes in RiskReason and Angles must be empty.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
	RiskRefuse = "refuse"
)

type Title struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type Body struct {
	ID   string   `json:"id"`
	Text string   `json:"text"`
	Tags []string `json:"tags,omitempty"` // e.g., "opener:question", "close:invite-stories"
}

type Angle struct {
	ID      int     `json:"id"`
	Premise string  `json:"premise"`
	Hook    string  `json:"hook"`
	Titles  []Title `json:"titles"`
	Bodies  []Body  `json:"bodies"`
}

type Recommendation struct {
	AngleID   int    `json:"angle_id"`
	TitleID   string `json:"title_id"`
	BodyID    string `json:"body_id"`
	Reasoning string `json:"reasoning,omitempty"`
}

type FlairRec struct {
	Required  bool   `json:"required"`
	Suggested string `json:"suggested,omitempty"`
}

// Draft is one provider's output: angles + titles + bodies + meta. The
// full DraftBundle holds N of these (one per provider) for side-by-side
// comparison.
type Draft struct {
	Sub                 string         `json:"sub"`
	Provider            string         `json:"provider"`
	Model               string         `json:"model"`
	Risk                string         `json:"risk"`
	RiskReason          string         `json:"risk_reason,omitempty"`
	Angles              []Angle        `json:"angles"`
	Recommendation      Recommendation `json:"recommendation"`
	Flair               FlairRec       `json:"flair"`
	SuggestedWindow     string         `json:"suggested_window,omitempty"`
	MediaRecommendation string         `json:"media_recommendation,omitempty"`
	InputTokens         int            `json:"input_tokens,omitempty"`
	OutputTokens        int            `json:"output_tokens,omitempty"`
	Generated           time.Time      `json:"generated"`
	// Error is set if this provider's generation failed; the bundle keeps
	// the entry so the user can see which provider broke and why.
	Error string `json:"error,omitempty"`
}

// DraftBundle holds drafts produced by N providers for one (Brief, Sub)
// pair. Drafts are intentionally a slice — comparing across providers is
// the entire point of this step.
type DraftBundle struct {
	Sub       string    `json:"sub"`
	Brief     Brief     `json:"brief"`
	Drafts    []Draft   `json:"drafts"`
	Generated time.Time `json:"generated"`
}

// Snapshot is what we cache between `tider draft` and `tider regen`. It
// holds everything regen needs to reconstruct the original context: the
// Brief, the per-sub Research, and the most recent DraftBundle. Each
// successful regen overwrites it so subsequent regens iterate on the
// latest state.
type Snapshot struct {
	Brief    Brief       `json:"brief"`
	Research Research    `json:"research"`
	Bundle   DraftBundle `json:"bundle"`
	SavedAt  time.Time   `json:"saved_at"`
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
