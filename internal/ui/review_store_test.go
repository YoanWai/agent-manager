package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	"path/filepath"
	"testing"
)

// A reviewed mark placed on a path in one repo must not bleed onto a
// same-named path in a sibling repo when cycling with r.
func TestReviewMarksIsolatedPerRepo(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)
	if got := filepath.Base(m.diff.repoSel); got != dirtyName {
		t.Fatalf("want %q selected, got %q", dirtyName, got)
	}
	if fd := m.currentFileDiff(); fd == nil || fd.File.Path != "a.go" {
		t.Fatalf("want a.go under review in the dirty repo, got %v", fd)
	}
	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed("a.go") {
		t.Fatal("a.go should be reviewed in the dirty repo")
	}

	m.pickRepo(t, "alpha")
	if filepath.Base(m.diff.repoSel) != "alpha" {
		t.Fatalf("picker should select alpha, got %q", m.diff.repoSel)
	}
	if m.fileReviewed("a.go") {
		t.Fatal("a.go reviewed mark leaked into the sibling repo")
	}

	m.pickRepo(t, dirtyName)
	if !m.fileReviewed("a.go") {
		t.Fatal("picking back should restore the dirty repo's reviewed mark")
	}
}

// The selected repo is pinned by path, so a reload whose fresh ranking would
// put a different repo first keeps the user on the repo they chose.
func TestRepoSelectionSurvivesReload(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)

	m.pickRepo(t, "alpha")
	if filepath.Base(m.diff.repoSel) != "alpha" {
		t.Fatalf("want alpha selected, got %q", m.diff.repoSel)
	}
	// A scope cycle reloads through ResolveRepos, which ranks the dirty repo
	// first; the path pin must keep alpha selected regardless.
	m.pressDiffKey(t, 's')
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("reload should keep alpha pinned, got %q", got)
	}
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("repoSel should track the pinned repo after re-rank, got %q", got)
	}
}

func TestSavedReviewRoundsGainStableIDsAndPointNumbers(t *testing.T) {
	m := buildModel(t)
	m.diff.sessID = "abc123"
	m.diff.repoSel = "/repo"
	// A synthetic session skips openDiff, which is what lays these out.
	m.diff.reviewed = map[string]map[string]uint64{}
	m.diff.annotations = map[string][]annotation{}
	m.diff.rounds = map[string]store.ReviewRound{}
	m.diff.stateLoaded = map[string]bool{}
	if err := m.services.store.SetReviewState(m.diff.sessID, m.diff.repoSel, store.ReviewState{
		Comments: []store.ReviewComment{
			{File: "a.go", Line: 2, Text: "first", Round: 3},
			{File: "b.go", Line: 4, Text: "second", Round: 3},
		},
		Round: store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReviewState(m.services.store, m.diff.sessID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if !m.restoreReviewState(state) {
		t.Fatal("saved review state was not loaded")
	}
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 2 || len(notes[0].id) != 16 || len(notes[1].id) != 16 ||
		notes[0].id == notes[1].id || notes[0].point != 1 || notes[1].point != 2 {
		t.Fatalf("migrated comments = %+v", notes)
	}
	state, err = m.services.store.ReviewState(m.diff.sessID, m.diff.repoSel)
	if err != nil || state.Comments[0].ID == "" || state.Comments[1].Point != 2 {
		t.Fatalf("persisted migration = %+v, %v", state.Comments, err)
	}
}

func TestReviewProgressAndDraftsRestoreFromStore(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "restore", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("keep this feedback")
	m.applyCmd(t, m.saveAnnotation())
	path := m.currentFileDiff().File.Path
	m.drainCmds(t, m.toggleReviewed())
	wantHash := m.diff.reviewed[m.reviewKey()][m.reviewedMarkKey(path)]
	if wantHash == 0 {
		t.Fatal("reviewed hash was not recorded")
	}

	key := m.reviewKey()
	delete(m.diff.reviewed, key)
	delete(m.diff.annotations, key)
	delete(m.diff.rounds, key)
	delete(m.diff.stateLoaded, key)
	state, err := readReviewState(m.services.store, m.diff.sessID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if !m.restoreReviewState(state) {
		t.Fatal("review state was not restored")
	}
	if got := m.diff.reviewed[key][m.reviewedMarkKey(path)]; got != wantHash {
		t.Fatalf("restored reviewed hash = %d, want %d", got, wantHash)
	}
	notes := m.diff.annotations[key]
	if len(notes) != 1 || notes[0].text != "keep this feedback" || notes[0].round != 0 {
		t.Fatalf("restored draft = %+v", notes)
	}
}

// Marks persisted before marks were scope-keyed carry a bare path; they can
// never match a scoped lookup, so restore drops them and the next save
// clears them from the row.
func TestRestoreDropsPreScopeReviewedMarks(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "baremarks", gitTestRepo(t))
	key := m.reviewKey()
	delete(m.diff.reviewed, key)
	delete(m.diff.stateLoaded, key)
	if !m.restoreReviewState(store.ReviewState{Reviewed: map[string]uint64{"main.go": 42}}) {
		t.Fatal("review state was not restored")
	}
	if len(m.diff.reviewed[key]) != 0 {
		t.Fatalf("a bare-path mark survived restore: %v", m.diff.reviewed[key])
	}
}
