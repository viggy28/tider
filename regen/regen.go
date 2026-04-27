// Package regen rolls a single piece of an existing draft (titles for one
// angle, or a specific body) back through the LLM with new guidance. The
// rest of the bundle is preserved verbatim so the user can iterate on one
// axis at a time without losing variants they liked.
package regen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/viggy28/tider/internal/llm"
	"github.com/viggy28/tider/internal/types"
	"github.com/viggy28/tider/prompts"
)

var (
	titlesTmpl = template.Must(template.ParseFS(prompts.FS, "regen_titles.tmpl"))
	bodyTmpl   = template.Must(template.ParseFS(prompts.FS, "regen_body.tmpl"))
)

const defaultMaxTokens = 4096

// Titles regenerates the titles for angleID across every provider's draft
// in snap.Bundle. Refused drafts are left untouched (nothing to regen).
// Drafts whose angles don't include angleID are recorded with an Error
// rather than failing the whole call.
func Titles(ctx context.Context, refs []llm.ProviderRef, snap *types.Snapshot, angleID int, note string) (*types.DraftBundle, error) {
	if len(refs) == 0 {
		return nil, errors.New("regen: at least one provider required")
	}
	if snap == nil {
		return nil, errors.New("regen: snapshot is nil")
	}
	out := snap.Bundle
	out.Drafts = append([]types.Draft(nil), snap.Bundle.Drafts...) // shallow copy so we don't mutate input

	var wg sync.WaitGroup
	for i, d := range out.Drafts {
		if d.Risk == types.RiskRefuse || d.Error != "" {
			continue
		}
		ref := pickRef(refs, d.Provider)
		if ref == nil {
			out.Drafts[i].Error = fmt.Sprintf("regen: provider %q not in --providers list", d.Provider)
			continue
		}
		angle := findAngle(d.Angles, angleID)
		if angle == nil {
			out.Drafts[i].Error = fmt.Sprintf("regen: angle %d not in this draft", angleID)
			continue
		}
		wg.Add(1)
		go func(i int, ref llm.ProviderRef, angle types.Angle) {
			defer wg.Done()
			out.Drafts[i] = regenTitlesOne(ctx, ref, snap, out.Drafts[i], angle, note)
		}(i, *ref, *angle)
	}
	wg.Wait()
	out.Generated = time.Now().UTC()
	return &out, nil
}

func regenTitlesOne(ctx context.Context, ref llm.ProviderRef, snap *types.Snapshot, d types.Draft, angle types.Angle, note string) types.Draft {
	prompt, err := renderTitlesPrompt(snap, angle, note)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	resp, err := ref.Provider.Complete(ctx, llm.Request{
		Model:     ref.Model,
		MaxTokens: defaultMaxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.InputTokens = resp.InputTokens
	d.OutputTokens = resp.OutputTokens
	var payload struct {
		Titles []types.Title `json:"titles"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &payload); err != nil {
		d.Error = fmt.Sprintf("parse titles json: %v (model returned: %q)", err, truncate(resp.Content, 200))
		return d
	}
	if len(payload.Titles) == 0 {
		d.Error = "regen: model returned no titles"
		return d
	}
	// Splice: replace titles in the matching angle, leave everything else.
	for ai := range d.Angles {
		if d.Angles[ai].ID == angle.ID {
			d.Angles[ai].Titles = payload.Titles
			break
		}
	}
	d.Generated = time.Now().UTC()
	d.Error = ""
	return d
}

// Body regenerates the single body identified by variantID (e.g. "2.1")
// across every provider's draft.
func Body(ctx context.Context, refs []llm.ProviderRef, snap *types.Snapshot, variantID, note string, lengthHint int) (*types.DraftBundle, error) {
	if len(refs) == 0 {
		return nil, errors.New("regen: at least one provider required")
	}
	if snap == nil {
		return nil, errors.New("regen: snapshot is nil")
	}
	angleID, _, err := splitVariantID(variantID)
	if err != nil {
		return nil, err
	}
	out := snap.Bundle
	out.Drafts = append([]types.Draft(nil), snap.Bundle.Drafts...)

	var wg sync.WaitGroup
	for i, d := range out.Drafts {
		if d.Risk == types.RiskRefuse || d.Error != "" {
			continue
		}
		ref := pickRef(refs, d.Provider)
		if ref == nil {
			out.Drafts[i].Error = fmt.Sprintf("regen: provider %q not in --providers list", d.Provider)
			continue
		}
		angle := findAngle(d.Angles, angleID)
		if angle == nil {
			out.Drafts[i].Error = fmt.Sprintf("regen: angle %d not in this draft", angleID)
			continue
		}
		body := findBody(angle, variantID)
		if body == nil {
			out.Drafts[i].Error = fmt.Sprintf("regen: body %s not in this draft", variantID)
			continue
		}
		title := pickFirstTitle(angle)
		wg.Add(1)
		go func(i int, ref llm.ProviderRef, angle types.Angle, body types.Body, titleText string) {
			defer wg.Done()
			out.Drafts[i] = regenBodyOne(ctx, ref, snap, out.Drafts[i], angle, body, titleText, note, lengthHint)
		}(i, *ref, *angle, *body, title)
	}
	wg.Wait()
	out.Generated = time.Now().UTC()
	return &out, nil
}

func regenBodyOne(ctx context.Context, ref llm.ProviderRef, snap *types.Snapshot, d types.Draft, angle types.Angle, body types.Body, titleText, note string, lengthHint int) types.Draft {
	prompt, err := renderBodyPrompt(snap, angle, body, titleText, note, lengthHint)
	if err != nil {
		d.Error = err.Error()
		return d
	}
	resp, err := ref.Provider.Complete(ctx, llm.Request{
		Model:     ref.Model,
		MaxTokens: defaultMaxTokens,
		JSONMode:  true,
		Messages:  []llm.Message{{Role: llm.RoleUser, Content: prompt}},
	})
	if err != nil {
		d.Error = err.Error()
		return d
	}
	d.InputTokens = resp.InputTokens
	d.OutputTokens = resp.OutputTokens
	var payload struct {
		Body types.Body `json:"body"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &payload); err != nil {
		d.Error = fmt.Sprintf("parse body json: %v (model returned: %q)", err, truncate(resp.Content, 200))
		return d
	}
	if payload.Body.Text == "" {
		d.Error = "regen: model returned empty body"
		return d
	}
	// Force the body ID back to the requested one — model may try to
	// renumber and we want splice-safety.
	payload.Body.ID = body.ID
	for ai := range d.Angles {
		if d.Angles[ai].ID == angle.ID {
			for bi := range d.Angles[ai].Bodies {
				if d.Angles[ai].Bodies[bi].ID == body.ID {
					d.Angles[ai].Bodies[bi] = payload.Body
					break
				}
			}
			break
		}
	}
	d.Generated = time.Now().UTC()
	d.Error = ""
	return d
}

func renderTitlesPrompt(snap *types.Snapshot, angle types.Angle, note string) (string, error) {
	var buf bytes.Buffer
	err := titlesTmpl.Execute(&buf, struct {
		SubName        string
		Subscribers    int
		CuratedNotes   *types.SubNotes
		TopWeek        []types.Post
		AngleID        int
		AnglePremise   string
		AngleHook      string
		ExistingTitles []types.Title
		Note           string
		TitleCount     int
	}{
		SubName:        snap.Research.Sub.Name,
		Subscribers:    snap.Research.Sub.Subscribers,
		CuratedNotes:   snap.Research.Notes,
		TopWeek:        snap.Research.TopWeek,
		AngleID:        angle.ID,
		AnglePremise:   angle.Premise,
		AngleHook:      angle.Hook,
		ExistingTitles: angle.Titles,
		Note:           note,
		TitleCount:     len(angle.Titles),
	})
	if err != nil {
		return "", fmt.Errorf("render titles prompt: %w", err)
	}
	return buf.String(), nil
}

func renderBodyPrompt(snap *types.Snapshot, angle types.Angle, body types.Body, titleText, note string, lengthHint int) (string, error) {
	var buf bytes.Buffer
	err := bodyTmpl.Execute(&buf, struct {
		SubName      string
		Subscribers  int
		CuratedNotes *types.SubNotes
		TopWeek      []types.Post
		AnglePremise string
		AngleHook    string
		TitleText    string
		BodyID       string
		BodyText     string
		BodyTags     []string
		Note         string
		LengthHint   int
	}{
		SubName:      snap.Research.Sub.Name,
		Subscribers:  snap.Research.Sub.Subscribers,
		CuratedNotes: snap.Research.Notes,
		TopWeek:      snap.Research.TopWeek,
		AnglePremise: angle.Premise,
		AngleHook:    angle.Hook,
		TitleText:    titleText,
		BodyID:       body.ID,
		BodyText:     body.Text,
		BodyTags:     body.Tags,
		Note:         note,
		LengthHint:   lengthHint,
	})
	if err != nil {
		return "", fmt.Errorf("render body prompt: %w", err)
	}
	return buf.String(), nil
}

// pickRef returns the ProviderRef whose Provider.Name matches name, so a
// regen on draft N (produced by openai) routes back to the openai
// provider rather than re-rolling through anthropic.
func pickRef(refs []llm.ProviderRef, name string) *llm.ProviderRef {
	for i := range refs {
		if refs[i].Provider.Name() == name {
			return &refs[i]
		}
	}
	return nil
}

func findAngle(angles []types.Angle, id int) *types.Angle {
	for i := range angles {
		if angles[i].ID == id {
			return &angles[i]
		}
	}
	return nil
}

func findBody(a *types.Angle, id string) *types.Body {
	if a == nil {
		return nil
	}
	for i := range a.Bodies {
		if a.Bodies[i].ID == id {
			return &a.Bodies[i]
		}
	}
	return nil
}

// pickFirstTitle returns a title text the body can be framed under. We
// prefer the angle's first title since regen body keeps the title intact;
// if the angle has no titles (shouldn't happen), returns empty string.
func pickFirstTitle(a *types.Angle) string {
	if a == nil || len(a.Titles) == 0 {
		return ""
	}
	return a.Titles[0].Text
}

// splitVariantID parses "A.B" → (A, B). Returns an error for malformed
// IDs so the caller surfaces the issue rather than silently mismatching.
func splitVariantID(id string) (int, string, error) {
	dot := strings.Index(id, ".")
	if dot <= 0 || dot == len(id)-1 {
		return 0, "", fmt.Errorf("regen: bad variant id %q (expected like \"2.1\")", id)
	}
	var angleID int
	if _, err := fmt.Sscanf(id[:dot], "%d", &angleID); err != nil {
		return 0, "", fmt.Errorf("regen: bad angle in variant id %q: %w", id, err)
	}
	return angleID, id, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
