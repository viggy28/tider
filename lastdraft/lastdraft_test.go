package lastdraft

import (
	"errors"
	"testing"
	"time"

	"github.com/viggy28/tider/internal/types"
)

func sampleSnapshot() *types.Snapshot {
	return &types.Snapshot{
		Brief: types.Brief{Title: "Streambed", Summary: "WAL CDC."},
		Research: types.Research{
			Sub: types.Subreddit{Name: "databases", Subscribers: 100000},
		},
		Bundle: types.DraftBundle{
			Sub: "databases",
			Drafts: []types.Draft{
				{
					Provider: "openai",
					Model:    "gpt-5",
					Risk:     "low",
					Angles: []types.Angle{
						{ID: 1, Premise: "p", Hook: "h", Titles: []types.Title{{ID: "1.1", Text: "T"}}, Bodies: []types.Body{{ID: "1.1", Text: "B"}}},
					},
					Recommendation: types.Recommendation{AngleID: 1, TitleID: "1.1", BodyID: "1.1"},
				},
			},
		},
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	snap := sampleSnapshot()
	if err := Save(dir, "databases", snap); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, "databases")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Brief.Title != "Streambed" {
		t.Errorf("brief title lost: %q", loaded.Brief.Title)
	}
	if loaded.Research.Sub.Name != "databases" {
		t.Errorf("research lost: %+v", loaded.Research.Sub)
	}
	if len(loaded.Bundle.Drafts) != 1 || loaded.Bundle.Drafts[0].Provider != "openai" {
		t.Errorf("bundle lost: %+v", loaded.Bundle)
	}
	if loaded.SavedAt.IsZero() {
		t.Error("SavedAt not stamped")
	}
}

func TestLoadMissingReturnsErrNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir, "doesnotexist")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestSaveOverwrites(t *testing.T) {
	dir := t.TempDir()
	snap := sampleSnapshot()
	snap.Brief.Title = "v1"
	if err := Save(dir, "databases", snap); err != nil {
		t.Fatal(err)
	}
	snap.Brief.Title = "v2"
	if err := Save(dir, "databases", snap); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(dir, "databases")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Brief.Title != "v2" {
		t.Errorf("expected v2, got %q", loaded.Brief.Title)
	}
}

func TestSaveStampsSavedAt(t *testing.T) {
	dir := t.TempDir()
	snap := sampleSnapshot()
	before := time.Now().UTC().Add(-time.Second)
	if err := Save(dir, "databases", snap); err != nil {
		t.Fatal(err)
	}
	if snap.SavedAt.Before(before) {
		t.Errorf("SavedAt not updated: %v before %v", snap.SavedAt, before)
	}
}

func TestSaveAtomicLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, "databases", sampleSnapshot()); err != nil {
		t.Fatal(err)
	}
	// .tmp file should be cleaned up after rename
	if _, err := Load(dir, "databases.tmp"); !errors.Is(err, ErrNotFound) {
		t.Errorf(".tmp file lingered after save")
	}
}
