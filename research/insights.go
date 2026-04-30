package research

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var insightsTmpl = template.Must(template.ParseFS(prompts.FS, "research_insights.tmpl"))

const (
	DefaultInsightsMaxTokens   = 10000
	defaultInsightsMaxPosts    = 12
	defaultInsightsExcerptSize = 500
)

type insightPromptPost struct {
	Source    string
	Title     string
	Score     int
	Comments  int
	Flair     string
	Selftext  string
	Permalink string
}

// RenderInsightsPrompt converts a raw Research bundle into the prompt used
// for pain-point clustering. It trims long post bodies but preserves titles,
// scores, comments, source bucket, and permalinks for auditability.
func RenderInsightsPrompt(r types.Research) (string, error) {
	posts := selectPromptPosts(r, defaultInsightsMaxPosts)
	var buf bytes.Buffer
	err := insightsTmpl.Execute(&buf, struct {
		SubName string
		Rules   []types.Rule
		Posts   []insightPromptPost
	}{
		SubName: r.Sub.Name,
		Rules:   r.Rules,
		Posts:   posts,
	})
	if err != nil {
		return "", fmt.Errorf("render research insights prompt: %w", err)
	}
	return buf.String(), nil
}

// GenerateInsights asks the configured LLM to produce a concise, evidence-
// grounded pain-point report from a raw Research bundle.
func GenerateInsights(ctx context.Context, p llm.Provider, model string, r types.Research, maxTokens int) (*types.ResearchInsights, error) {
	if p == nil {
		return nil, errors.New("research insights: provider is nil")
	}
	if maxTokens <= 0 {
		maxTokens = DefaultInsightsMaxTokens
	}
	prompt, err := RenderInsightsPrompt(r)
	if err != nil {
		return nil, err
	}
	resp, err := p.Complete(ctx, llm.Request{
		Model:     model,
		MaxTokens: maxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("research insights llm: %w", err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		return nil, fmt.Errorf("research insights llm returned empty content (tokens: in=%d out=%d); try a larger --max-tokens or a smaller cached research set with --refresh", resp.InputTokens, resp.OutputTokens)
	}
	var insights types.ResearchInsights
	if err := json.Unmarshal([]byte(resp.Content), &insights); err != nil {
		return nil, fmt.Errorf("parse research insights json: %w (model returned: %q)", err, truncate(resp.Content, 200))
	}
	if insights.Subreddit == "" {
		insights.Subreddit = r.Sub.Name
	}
	insights.InputTokens = resp.InputTokens
	insights.OutputTokens = resp.OutputTokens
	insights.Generated = time.Now().UTC()
	trimInsights(&insights)
	fillTopLevelEvidence(&insights)
	return &insights, nil
}

func selectPromptPosts(r types.Research, limit int) []insightPromptPost {
	seen := map[string]bool{}
	var out []insightPromptPost
	add := func(source string, posts []types.Post) {
		for _, p := range posts {
			key := p.ID
			if key == "" {
				key = p.Permalink
			}
			if key == "" {
				key = source + ":" + p.Title
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, insightPromptPost{
				Source:    source,
				Title:     p.Title,
				Score:     p.Score,
				Comments:  p.NumComments,
				Flair:     p.LinkFlairText,
				Selftext:  excerpt(p.Selftext, defaultInsightsExcerptSize),
				Permalink: p.Permalink,
			})
			if len(out) >= limit {
				return
			}
		}
	}
	add("top_week", r.TopWeek)
	if len(out) < limit {
		add("top_month", r.TopMonth)
	}
	if len(out) < limit {
		add("hot", r.Hot)
	}
	return out
}

func trimInsights(i *types.ResearchInsights) {
	if len(i.PainPoints) > 5 {
		i.PainPoints = i.PainPoints[:5]
	}
	if len(i.RepeatedAsks) > 6 {
		i.RepeatedAsks = i.RepeatedAsks[:6]
	}
	if len(i.Opportunity) > 5 {
		i.Opportunity = i.Opportunity[:5]
	}
	if len(i.Language) > 12 {
		i.Language = i.Language[:12]
	}
	if len(i.Evidence) > 5 {
		i.Evidence = i.Evidence[:5]
	}
	for pi := range i.PainPoints {
		if len(i.PainPoints[pi].Evidence) > 2 {
			i.PainPoints[pi].Evidence = i.PainPoints[pi].Evidence[:2]
		}
	}
}

func fillTopLevelEvidence(i *types.ResearchInsights) {
	if len(i.Evidence) > 0 {
		return
	}
	seen := map[string]bool{}
	for _, p := range i.PainPoints {
		for _, e := range p.Evidence {
			key := e.Title + "|" + e.Permalink
			if seen[key] {
				continue
			}
			seen[key] = true
			i.Evidence = append(i.Evidence, e)
			if len(i.Evidence) >= 5 {
				return
			}
		}
	}
}

func excerpt(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "..."
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
