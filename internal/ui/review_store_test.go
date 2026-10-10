package ui

import (
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
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
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != dirtyName {
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
	if filepath.Base(m.review.Snapshot().RepoSelected) != "alpha" {
		t.Fatalf("picker should select alpha, got %q", m.review.Snapshot().RepoSelected)
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
	if filepath.Base(m.review.Snapshot().RepoSelected) != "alpha" {
		t.Fatalf("want alpha selected, got %q", m.review.Snapshot().RepoSelected)
	}
	// A scope cycle reloads through ResolveRepos, which ranks the dirty repo
	// first; the path pin must keep alpha selected regardless.
	m.pressDiffKey(t, 's')
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("reload should keep alpha pinned, got %q", got)
	}
	if got := filepath.Base(m.review.Snapshot().RepoSelected); got != "alpha" {
		t.Fatalf("repoSel should track the pinned repo after re-rank, got %q", got)
	}
}

func TestSavedReviewRoundsGainStableIDsAndPointNumbers(t *testing.T) {
	m := buildModel(t)
	const sessionID, repo = "abc123", "/repo"
	if err := m.services.store.SetReviewState(sessionID, repo, store.ReviewState{
		Comments: []store.ReviewComment{
			{File: "a.go", Line: 2, Text: "first", Round: 3},
			{File: "b.go", Line: 4, Text: "second", Round: 3},
		},
		Round: store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReviewState(m.services.store, sessionID, repo)
	if err != nil {
		t.Fatal(err)
	}
	notes := state.Comments
	if len(notes) != 2 || len(notes[0].ID) != 16 || len(notes[1].ID) != 16 ||
		notes[0].ID == notes[1].ID || notes[0].Point != 1 || notes[1].Point != 2 {
		t.Fatalf("migrated comments = %+v", notes)
	}
	state, err = m.services.store.ReviewState(sessionID, repo)
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
	typeReviewAnnotation(m, "keep this feedback")
	m.applyCmd(t, m.saveAnnotation())
	path := m.currentFileDiff().File.Path
	m.drainCmds(t, m.toggleReviewed())
	wantHash := m.review.SavedState().Reviewed[m.reviewedMarkKey(path)]
	if wantHash == 0 {
		t.Fatal("reviewed hash was not recorded")
	}

	snapshot := m.review.Snapshot()
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("review session disappeared")
	}
	state, err := readReviewState(m.services.store, snapshot.SessionID, snapshot.RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	seedReviewStateForTest(m, reviewTarget(sess), snapshot.Scope, snapshot.RepoSelected, m.review.SetCopy(), reviewStateFromStore(state))
	if got := m.review.SavedState().Reviewed[m.reviewedMarkKey(path)]; got != wantHash {
		t.Fatalf("restored reviewed hash = %d, want %d", got, wantHash)
	}
	notes := m.review.Annotations()
	if len(notes) != 1 || notes[0].Text != "keep this feedback" || notes[0].Round != 0 {
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
	snapshot := m.review.Snapshot()
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("review session disappeared")
	}
	seedReviewStateForTest(m, reviewTarget(sess), snapshot.Scope, snapshot.RepoSelected, m.review.SetCopy(), uireview.SavedState{Reviewed: map[string]uint64{"main.go": 42}})
	if marks := m.review.SavedState().Reviewed; len(marks) != 0 {
		t.Fatalf("a bare-path mark survived restore: %v", marks)
	}
}
