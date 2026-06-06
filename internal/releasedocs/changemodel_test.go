package releasedocs_test

import (
	"testing"
	"time"

	"github.com/payamqorbanpour/cadoo/internal/config"
	"github.com/payamqorbanpour/cadoo/internal/releasedocs"
	"github.com/payamqorbanpour/cadoo/internal/vcs"
)

// TestGroupedModel verifies that the grouped change model correctly parses
// Conventional Commit prefixes into ordered sections, and that label-based
// grouping correctly assigns merged PRs to their configured sections.
func TestGroupedModel(t *testing.T) {
	t.Run("conventional grouping", func(t *testing.T) {
		t.Run("feat maps to Features", func(t *testing.T) {
			commits := []vcs.Commit{
				{SHA: "aaa", Message: "feat: add new login flow", Author: "alice", Date: time.Now()},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"},
				},
			}
			model := releasedocs.BuildGroupedModel(commits, nil, cfg)
			if len(model.Sections) == 0 {
				t.Fatal("expected at least one section")
			}
			found := false
			for _, s := range model.Sections {
				if s.Title == "Features" {
					found = true
					if len(s.Entries) != 1 {
						t.Errorf("Features section: want 1 entry, got %d", len(s.Entries))
					}
					break
				}
			}
			if !found {
				t.Error("expected a 'Features' section")
			}
		})

		t.Run("fix maps to Bug Fixes", func(t *testing.T) {
			commits := []vcs.Commit{
				{SHA: "bbb", Message: "fix: correct nil pointer in auth handler", Author: "bob", Date: time.Now()},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"},
				},
			}
			model := releasedocs.BuildGroupedModel(commits, nil, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Bug Fixes" {
					found = true
					if len(s.Entries) != 1 {
						t.Errorf("Bug Fixes section: want 1 entry, got %d", len(s.Entries))
					}
					break
				}
			}
			if !found {
				t.Error("expected a 'Bug Fixes' section")
			}
		})

		t.Run("perf maps to Performance", func(t *testing.T) {
			commits := []vcs.Commit{
				{SHA: "ccc", Message: "perf: speed up DB query", Author: "carol", Date: time.Now()},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"},
				},
			}
			model := releasedocs.BuildGroupedModel(commits, nil, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Performance" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected a 'Performance' section")
			}
		})

		t.Run("feat! maps to Breaking Changes", func(t *testing.T) {
			commits := []vcs.Commit{
				{SHA: "ddd", Message: "feat!: rename API endpoint", Author: "dave", Date: time.Now()},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"},
				},
			}
			model := releasedocs.BuildGroupedModel(commits, nil, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Breaking Changes" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected a 'Breaking Changes' section")
			}
		})

		t.Run("BREAKING CHANGE in body maps to Breaking Changes", func(t *testing.T) {
			commits := []vcs.Commit{
				{
					SHA:     "eee",
					Message: "refactor: restructure config\n\nBREAKING CHANGE: the old format is no longer supported",
					Author:  "eve",
					Date:    time.Now(),
				},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"},
				},
			}
			model := releasedocs.BuildGroupedModel(commits, nil, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Breaking Changes" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected a 'Breaking Changes' section when body contains BREAKING CHANGE:")
			}
		})

		t.Run("unknown prefix falls to Other", func(t *testing.T) {
			commits := []vcs.Commit{
				{SHA: "fff", Message: "chore: bump dependencies", Author: "frank", Date: time.Now()},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance", "Other"},
				},
			}
			model := releasedocs.BuildGroupedModel(commits, nil, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Other" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected an 'Other' section for unknown prefixes")
			}
		})

		t.Run("section ordering is canonical and deterministic", func(t *testing.T) {
			commits := []vcs.Commit{
				{SHA: "g1", Message: "perf: fast path", Author: "a", Date: time.Now()},
				{SHA: "g2", Message: "feat: new feature", Author: "b", Date: time.Now()},
				{SHA: "g3", Message: "fix: bug", Author: "c", Date: time.Now()},
				{SHA: "g4", Message: "feat!: breaking change", Author: "d", Date: time.Now()},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "conventional",
					Sections: []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"},
				},
			}
			model1 := releasedocs.BuildGroupedModel(commits, nil, cfg)
			model2 := releasedocs.BuildGroupedModel(commits, nil, cfg)

			if len(model1.Sections) != len(model2.Sections) {
				t.Fatalf("section count differs between runs: %d vs %d", len(model1.Sections), len(model2.Sections))
			}
			for i := range model1.Sections {
				if model1.Sections[i].Title != model2.Sections[i].Title {
					t.Errorf("position %d: first run=%q, second run=%q", i, model1.Sections[i].Title, model2.Sections[i].Title)
				}
			}
			// Verify canonical order: Breaking Changes before Features before Bug Fixes before Performance.
			titles := sectionTitles(model1.Sections)
			wantOrder := []string{"Breaking Changes", "Features", "Bug Fixes", "Performance"}
			checkRelativeOrder(t, titles, wantOrder)
		})
	})

	t.Run("labels grouping", func(t *testing.T) {
		t.Run("mapped label assigns PR to configured section", func(t *testing.T) {
			prs := []vcs.MergedPR{
				{Number: 10, Title: "Add feature X", Labels: []string{"enhancement"}, Author: "alice"},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "labels",
					Sections: []string{"Features", "Bug Fixes"},
					Labels: map[string]string{
						"enhancement": "Features",
						"bug":         "Bug Fixes",
					},
				},
			}
			model := releasedocs.BuildGroupedModel(nil, prs, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Features" {
					found = true
					if len(s.Entries) != 1 {
						t.Errorf("Features section: want 1 entry, got %d", len(s.Entries))
					}
					break
				}
			}
			if !found {
				t.Error("expected a 'Features' section for label 'enhancement'")
			}
		})

		t.Run("unmapped label falls to Other", func(t *testing.T) {
			prs := []vcs.MergedPR{
				{Number: 11, Title: "Random change", Labels: []string{"wontfix"}, Author: "bob"},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "labels",
					Sections: []string{"Features", "Bug Fixes", "Other"},
					Labels: map[string]string{
						"enhancement": "Features",
						"bug":         "Bug Fixes",
					},
				},
			}
			model := releasedocs.BuildGroupedModel(nil, prs, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Other" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected 'Other' section for unmapped label")
			}
		})

		t.Run("PR with no labels falls to Other", func(t *testing.T) {
			prs := []vcs.MergedPR{
				{Number: 12, Title: "Unlabelled PR", Labels: nil, Author: "carol"},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "labels",
					Sections: []string{"Features", "Other"},
					Labels: map[string]string{
						"enhancement": "Features",
					},
				},
			}
			model := releasedocs.BuildGroupedModel(nil, prs, cfg)
			found := false
			for _, s := range model.Sections {
				if s.Title == "Other" {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected 'Other' section for PR with no labels")
			}
		})

		t.Run("section ordering is canonical", func(t *testing.T) {
			prs := []vcs.MergedPR{
				{Number: 13, Title: "Fix bug", Labels: []string{"bug"}, Author: "alice"},
				{Number: 14, Title: "New feat", Labels: []string{"enhancement"}, Author: "bob"},
			}
			cfg := config.ReleaseDocs{
				Grouping: config.ReleaseGrouping{
					Source:   "labels",
					Sections: []string{"Features", "Bug Fixes"},
					Labels: map[string]string{
						"enhancement": "Features",
						"bug":         "Bug Fixes",
					},
				},
			}
			model1 := releasedocs.BuildGroupedModel(nil, prs, cfg)
			model2 := releasedocs.BuildGroupedModel(nil, prs, cfg)
			for i := range model1.Sections {
				if i >= len(model2.Sections) {
					break
				}
				if model1.Sections[i].Title != model2.Sections[i].Title {
					t.Errorf("non-deterministic section order at pos %d: %q vs %q",
						i, model1.Sections[i].Title, model2.Sections[i].Title)
				}
			}
			// Features before Bug Fixes.
			if len(model1.Sections) >= 2 {
				if model1.Sections[0].Title != "Features" || model1.Sections[1].Title != "Bug Fixes" {
					t.Errorf("order: want Features,Bug Fixes; got %v", sectionTitles(model1.Sections))
				}
			}
		})
	})

	t.Run("llm source falls back to conventional with warning", func(t *testing.T) {
		// grouping.source=llm is not implemented this phase; it should silently
		// fall back to conventional (Open Question 1 resolution).
		commits := []vcs.Commit{
			{SHA: "hhh", Message: "feat: llm fallback test", Author: "alice", Date: time.Now()},
		}
		cfg := config.ReleaseDocs{
			Grouping: config.ReleaseGrouping{
				Source:   "llm",
				Sections: []string{"Features"},
			},
		}
		model := releasedocs.BuildGroupedModel(commits, nil, cfg)
		found := false
		for _, s := range model.Sections {
			if s.Title == "Features" {
				found = true
				break
			}
		}
		if !found {
			t.Error("llm source should fall back to conventional, expected 'Features' section")
		}
	})
}

// sectionTitles extracts section titles from a slice of ChangeSection.
func sectionTitles(sections []releasedocs.ChangeSection) []string {
	out := make([]string, len(sections))
	for i, s := range sections {
		out[i] = s.Title
	}
	return out
}

// checkRelativeOrder verifies that present elements of wantOrder appear in
// the specified relative order within titles.
func checkRelativeOrder(t *testing.T, titles, wantOrder []string) {
	t.Helper()
	for i := 0; i < len(wantOrder); i++ {
		for j := i + 1; j < len(wantOrder); j++ {
			earlier, later := wantOrder[i], wantOrder[j]
			posI, posJ := -1, -1
			for k, got := range titles {
				if got == earlier {
					posI = k
				}
				if got == later {
					posJ = k
				}
			}
			if posI == -1 || posJ == -1 {
				continue // one of them isn't present; skip
			}
			if posI > posJ {
				t.Errorf("section %q (pos %d) must come before %q (pos %d)", earlier, posI, later, posJ)
			}
		}
	}
}
