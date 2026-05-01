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

// ResearchEvidence points to a Reddit post used as evidence for an insight.
// The insight layer intentionally keeps evidence compact so human output stays
// reviewable while the raw Research bundle remains available for audit.
type ResearchEvidence struct {
	Title     string `json:"title"`
	Score     int    `json:"score"`
	Comments  int    `json:"comments"`
	Source    string `json:"source"` // "top_week" | "top_month" | "hot"
	Permalink string `json:"permalink,omitempty"`
}

type PainPointCluster struct {
	Name       string             `json:"name"`
	Summary    string             `json:"summary"`
	Confidence string             `json:"confidence"` // "high" | "medium" | "low"
	Evidence   []ResearchEvidence `json:"evidence"`
}

type SpecificFriction struct {
	Name       string             `json:"name"`
	Summary    string             `json:"summary"`
	Confidence string             `json:"confidence"` // "medium" | "low"
	Evidence   []ResearchEvidence `json:"evidence"`
}

// ResearchInsights is the human-oriented output of `tider research`: factual
// pain-point clusters, repeated asks, opportunity signals, and language found
// in recent subreddit posts.
type ResearchInsights struct {
	Subreddit        string             `json:"subreddit"`
	Takeaway         string             `json:"takeaway,omitempty"`
	PainPoints       []PainPointCluster `json:"pain_points"`
	SpecificFriction []SpecificFriction `json:"specific_friction,omitempty"`
	RepeatedAsks     []string           `json:"repeated_asks"`
	Opportunity      []string           `json:"opportunity"`
	Language         []string           `json:"language"`
	Evidence         []ResearchEvidence `json:"evidence"`
	Limitations      []string           `json:"limitations,omitempty"`
	InputTokens      int                `json:"input_tokens,omitempty"`
	OutputTokens     int                `json:"output_tokens,omitempty"`
	Generated        time.Time          `json:"generated"`
}

// ResearchReport pairs the raw Reddit bundle with the synthesized insight
// report. Raw is stored so insights can be audited or regenerated without
// another Reddit fetch when the assembled cache is fresh.
type ResearchReport struct {
	Raw       Research         `json:"raw"`
	Insights  ResearchInsights `json:"insights"`
	Generated time.Time        `json:"generated"`
}

// Thread is a fetched Reddit submission plus a slice of selected comments.
// Used by `tider reply` for both mode detection (uses post fields only)
// and reply drafting (uses post + comments).
type Thread struct {
	URL         string    `json:"url"`
	Subreddit   string    `json:"subreddit"`
	PostID      string    `json:"post_id"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	Author      string    `json:"author"`
	Flair       string    `json:"flair,omitempty"`
	OutboundURL string    `json:"outbound_url,omitempty"`
	Comments    []Comment `json:"comments"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// Comment is a single Reddit comment, flattened from its position in the
// reply tree. ParentID is preserved so callers can reconstruct hierarchy
// if needed (LLM prompt rendering, for example).
type Comment struct {
	ID         string  `json:"id"`
	ParentID   string  `json:"parent_id,omitempty"`
	Author     string  `json:"author"`
	Body       string  `json:"body"`
	Score      int     `json:"score"`
	CreatedUTC float64 `json:"created_utc"`
}

// ReplyMode is one of "reply" or "review", per `tider reply` mode detection.
type ReplyMode string

const (
	ReplyModeReply  ReplyMode = "reply"
	ReplyModeReview ReplyMode = "review"
)

// ReplyModeResult is the output of the LLM-driven mode classifier. Mode
// detection uses only the OP fields (title/flair/body/outbound URL).
// TargetURLs are what the classifier identified plus any URLs the
// post-extraction pass found in the body — deduplicated.
type ReplyModeResult struct {
	Mode       ReplyMode `json:"mode"`
	Reason     string    `json:"reason,omitempty"`
	TargetURLs []string  `json:"target_urls,omitempty"`
}

// LoadedReplyContext is a snapshot of a context-bank entry at the time
// `tider reply` was run. Source is "bank" (loaded by id) or "path"
// (loaded by direct file path). Body is the verbatim markdown contents
// — preserved in the session so future re-runs against the same session
// see exactly what the LLM saw.
type LoadedReplyContext struct {
	ID     string `json:"id,omitempty"`
	Source string `json:"source"`
	Path   string `json:"path"`
	Body   string `json:"body"`
}

// ReplyDraft is one variant produced by the reply drafter.
//
// Common labels: "best", "short", "thread-aware", "personal-story",
// "question-first", "detailed". Not exhaustive — labels are free-form
// strings on the wire so the prompt can evolve without a schema change.
// The renderer title-cases hyphenated labels for display.
type ReplyDraft struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Text      string `json:"text"`
	Reasoning string `json:"reasoning,omitempty"`
}

// ReplyBundle holds the variants a single reply-drafting call produced
// plus the pick the LLM recommends. Same shape regardless of mode
// (reply vs review). Inspection is populated only in review mode and
// drives the inspection-depth header in the rendered output.
type ReplyBundle struct {
	ThreadURL  string             `json:"thread_url"`
	Subreddit  string             `json:"subreddit"`
	Mode       ReplyMode          `json:"mode"`
	Drafts     []ReplyDraft       `json:"drafts"`
	PickID     string             `json:"pick_id,omitempty"`
	Inspection *InspectionSummary `json:"inspection,omitempty"`
	Generated  time.Time          `json:"generated"`
}

// Inspection is the structured signal we extract from a review target's
// page. Two extraction backends populate this struct, identified by
// Source:
//
//   - "html"      — stdlib net/http + golang.org/x/net/html. Always
//                   available. Title/meta/headings/snippets only.
//   - "firecrawl" — firecrawl.dev API. Used when FIRECRAWL_API_KEY is in
//                   the env. Adds Markdown (cleaner than Snippets),
//                   ScreenshotURL (full-page PNG), and ImageURLs.
//
// Downstream steps (review notes, drafter) read whatever's present and
// gracefully degrade. The ScreenshotURL/ImageURLs unlock visual review
// observations once a vision-capable LLM call is wired in (separate
// follow-up — current notes step is text-only).
type Inspection struct {
	URL             string    `json:"url"`
	Status          int       `json:"status"`
	Source          string    `json:"source"` // "html" | "firecrawl"
	Title           string    `json:"title,omitempty"`
	MetaDescription string    `json:"meta_description,omitempty"`
	OGTitle         string    `json:"og_title,omitempty"`
	OGDescription   string    `json:"og_description,omitempty"`
	Headings        []Heading `json:"headings,omitempty"`
	Snippets        []string  `json:"snippets,omitempty"`

	// Populated only by the firecrawl backend.
	Markdown       string   `json:"markdown,omitempty"`
	ScreenshotURL  string   `json:"screenshot_url,omitempty"`
	ScreenshotPath string   `json:"screenshot_path,omitempty"` // local path under session/screenshots if downloaded
	ImageURLs      []string `json:"image_urls,omitempty"`

	FetchedAt time.Time `json:"fetched_at"`
}

// Heading is one h1/h2/h3 from the inspected page.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

// ReviewNotes is the structured observations the review drafter consumes.
// LLM-generated from an Inspection, kept honest by being grounded in
// inspection content rather than priors.
type ReviewNotes struct {
	TargetURL     string    `json:"target_url"`
	Strengths     []string  `json:"strengths,omitempty"`
	Weaknesses    []string  `json:"weaknesses,omitempty"`
	Suggestions   []string  `json:"suggestions,omitempty"`
	OpenQuestions []string  `json:"open_questions,omitempty"`
	Generated     time.Time `json:"generated"`
}

// VisualReviewNotes is the LLM's analysis of the captured screenshot
// (and optionally selected product/page images). Sits alongside
// ReviewNotes — text and visual observations are kept in separate
// artifacts so the review drafter can quote each one specifically and
// the user can audit which findings came from where.
//
// ShopType is the analyzer's classification of the inspected page; it
// gates the KovaSignals slot. KovaSignals is populated only when
// ShopType is "handmade" or "boutique" (where the Kova thesis applies).
// For B2B / SaaS / dropship / services / portfolio / unclear pages,
// KovaSignals stays empty so the drafter doesn't recommend "show your
// maker process" to an industrial supplier.
type VisualReviewNotes struct {
	TargetURL    string              `json:"target_url"`
	ShopType     string              `json:"shop_type"` // handmade|boutique|dropship|b2b_industrial|saas|services|portfolio|unclear
	Summary      string              `json:"summary"`
	Observations []VisualObservation `json:"observations"`
	KovaSignals  []string            `json:"kova_signals,omitempty"`
	Questions    []string            `json:"questions,omitempty"`
	Limitations  []string            `json:"limitations,omitempty"`
	Generated    time.Time           `json:"generated"`
}

// VisualObservation is one finding from the visual analyzer. Each
// observation must cite visible evidence so the drafter can quote it
// directly without re-inspecting the page.
type VisualObservation struct {
	Area           string `json:"area"` // above_fold|product_images|trust|navigation|pricing_cta|mobile_risk|brand|generic_risk
	Finding        string `json:"finding"`
	Evidence       string `json:"evidence"`
	Severity       string `json:"severity"` // high|medium|low
	Recommendation string `json:"recommendation"`
}

// VisualInputRecord is what gets persisted to visual-input.json — the
// exact materials sent to the visual analyzer. Saved alongside
// visual-notes.json so a session can be replayed without re-fetching.
type VisualInputRecord struct {
	TargetURL           string                `json:"target_url"`
	ScreenshotPath      string                `json:"screenshot_path"`
	ScreenshotSourceURL string                `json:"screenshot_source_url,omitempty"`
	ImageRefs           []VisualImageRef      `json:"image_refs,omitempty"`
	PageTitle           string                `json:"page_title,omitempty"`
	ContextIDs          []string              `json:"context_ids,omitempty"`
	Generated           time.Time             `json:"generated"`
}

type VisualImageRef struct {
	URL       string `json:"url"`
	LocalPath string `json:"local_path,omitempty"`
	Alt       string `json:"alt,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// InspectionSummary is a small render-time payload describing what was
// inspected for a review. Populated only in review mode; reply mode
// ignores it. Surfaces in the rendered output so users can see the
// inspection depth before reading the draft.
type InspectionSummary struct {
	Source         string   `json:"source"`              // "firecrawl" — HTML inspection isn't valid for review-mode rendering
	ScreenshotPath string   `json:"screenshot_path,omitempty"`
	ImagesAnalyzed int      `json:"images_analyzed"`
	ShopType       string   `json:"shop_type,omitempty"`
	Limitations    []string `json:"limitations,omitempty"`
}
