package ui

import (
	"database/sql"
	"errors"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode"
)

// A reviewed mark placed on a path in one repo must not bleed onto a
// same-named path in a sibling repo when cycling with r.
func TestReviewMarksIsolatedPerRepo(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	state, err := uireviewsource.ReadState(m.services.store, sessionID, repo)
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
	t.Parallel()
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
	state, err := uireviewsource.ReadState(m.services.store, snapshot.SessionID, snapshot.RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	seedReviewStateForTest(m, uireviewsource.Target(sess), snapshot.Scope, snapshot.RepoSelected, m.review.SetCopy(), uireviewsource.StateFromStore(state))
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
	t.Parallel()
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
	seedReviewStateForTest(m, uireviewsource.Target(sess), snapshot.Scope, snapshot.RepoSelected, m.review.SetCopy(), uireview.SavedState{Reviewed: map[string]uint64{"main.go": 42}})
	if marks := m.review.SavedState().Reviewed; len(marks) != 0 {
		t.Fatalf("a bare-path mark survived restore: %v", marks)
	}
}

func TestDiffAnnotateAndSend(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	setReviewSideBySide(m, false)

	for i, fd := range m.review.Snapshot().Set.Files {
		if fd.File.Path == "main.go" {
			m.drainCmds(t, selectReviewFile(m, i))
		}
	}
	m.drainCmds(t, m.loadCurrentDiffFile())
	fd := m.currentFileDiff()
	target := -1
	for i, line := range fd.Lines {
		if line.NewNum > 0 && strings.Contains(line.Text, "println") {
			target = i
		}
	}
	if target < 0 {
		t.Fatalf("no add line found: %+v", fd.Lines)
	}
	m.review.SetCursorDiffLine(target, m.diffCodeHeight())
	m.openAnnotate()
	typeReviewAnnotation(m, "use fmt.Println here")
	m.applyCmd(t, m.saveAnnotation())
	if notes := m.review.Annotations(); len(notes) != 1 {
		t.Fatalf("annotations = %+v", notes)
	}

	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	notes := m.review.Annotations()
	if len(notes) != 1 || notes[0].Round != 1 || notes[0].Point != 1 || len(notes[0].ID) != 16 {
		t.Fatalf("sent annotations = %+v, want one comment in round 1", notes)
	}
	if !strings.Contains(m.review.Snapshot().Notice, "review round 1 (1 comment)") {
		t.Fatalf("notice = %q (err=%q)", m.review.Snapshot().Notice, m.errBar.text)
	}
	sess := m.sessionRows()[0]
	state, err := m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 1 || state.Round.Fingerprint != m.review.Snapshot().Fingerprint ||
		len(state.Comments) != 1 || state.Comments[0].Round != 1 || state.Comments[0].Point != 1 || state.Comments[0].ID != notes[0].ID {
		t.Fatalf("persisted review round = %+v", state)
	}
	// Join wrapped lines so the delivery check does not depend on where the
	// pane's width breaks the prompt; the session sizes to the model width.
	// psmux's -J only trims each row, so on Windows keep every row at the
	// full pane width (-N) and join them by hand.
	capture := []string{"capture-pane", "-p", "-J", "-t", "am_" + sess.ID}
	if runtime.GOOS == "windows" {
		capture = []string{"capture-pane", "-p", "-N", "-t", "am_" + sess.ID}
	}
	// The pane echoes the paste when its shell gets to it, which on a
	// loaded machine outlasts the second the send waits for the echo.
	delivered := func(pane string) bool {
		return strings.Contains(pane, "use fmt.Println here") && strings.Contains(pane, "main.go:3") &&
			strings.Contains(pane, "[comment "+notes[0].ID+"]") && strings.Contains(pane, "review_comment") &&
			strings.Contains(pane, "Code review of the uncommitted changes on `main` (`") &&
			strings.Contains(pane, "in `"+m.review.Snapshot().RepoSelected+"`")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, err := tmuxCmd(t, capture...).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		pane := string(out)
		if runtime.GOOS == "windows" {
			pane = strings.NewReplacer("\r", "", "\n", "").Replace(pane)
		}
		if delivered(pane) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("prompt not delivered:\n%s", pane)
		}
		time.Sleep(50 * time.Millisecond)
	}

	m.openAnnotate()
	typeReviewAnnotation(m, "second pass")
	m.applyCmd(t, m.saveAnnotation())
	_, cmd = m.sendAnnotations()
	m.applyCmd(t, cmd)
	notes = m.review.Annotations()
	if len(notes) != 2 || notes[0].Round != 1 || notes[1].Round != 2 {
		t.Fatalf("review history = %+v, want rounds 1 and 2", notes)
	}
	state, err = m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 2 {
		t.Fatalf("second persisted review round = %+v", state)
	}
	current := m.review.Snapshot()
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: current.SessionID, Scope: current.Scope, Generation: current.Generation,
		RepoRoot: current.RepoSelected, RepoRoots: current.RepoRoots, Set: m.review.SetCopy(),
		Fingerprint: current.Fingerprint + 1,
	})
	if header := ansi.Strip(m.viewDiffHeader(sess.Name)); !strings.Contains(header, "Review round 2 · changed") {
		t.Fatalf("changed-since-round marker missing: %q", header)
	}
	set := m.review.SetCopy()
	request, ok := m.review.CycleScope()
	if !ok {
		t.Fatal("scope cycle was rejected")
	}
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: request.Target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: current.RepoSelected, RepoRoots: current.RepoRoots, Set: set,
		Fingerprint: current.Fingerprint + 1,
	})
	if header := ansi.Strip(m.viewDiffHeader(sess.Name)); !strings.Contains(header, "Review round 2 · changed") {
		t.Fatalf("scope change did not mark the round changed: %q", header)
	}
}

func TestSendAnnotationsDoesNotDeliverAnUnpersistedRound(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	m := buildModelWithStorePath(t, dbPath)
	openReviewOn(t, m, "persist-first", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	typeReviewAnnotation(m, "do not deliver without durable state")
	m.applyCmd(t, m.saveAnnotation())
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER refuse_review_round BEFORE UPDATE ON review_states BEGIN SELECT RAISE(ABORT, 'review round write refused'); END`); err != nil {
		t.Fatal(err)
	}

	m.sendAnnotations()
	m.drainEffects(t)
	notes := m.review.Annotations()
	if len(notes) != 1 || notes[0].Round != 0 || m.review.Round().Number != 0 {
		t.Fatalf("failed send did not restore the draft: notes=%+v round=%+v", notes, m.review.Round())
	}
	if m.review.Snapshot().SendPending || m.review.Snapshot().Notice != "" || !strings.Contains(m.errBar.text, "saving review round") {
		t.Fatalf("failed send state: pending=%v notice=%q err=%q", m.review.Snapshot().SendPending, m.review.Snapshot().Notice, m.errBar.text)
	}
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("review session disappeared")
	}
	out, err := tmuxCmd(t, "capture-pane", "-p", "-J", "-t", "am_"+sess.ID).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "do not deliver without durable state") {
		t.Fatalf("unpersisted review reached the pane:\n%s", out)
	}
}

func TestDiffCommentVisibleInBothLayouts(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	setReviewSideBySide(m, false)

	for i, fd := range m.review.Snapshot().Set.Files {
		if fd.File.Path == "main.go" {
			m.drainCmds(t, selectReviewFile(m, i))
		}
	}
	m.drainCmds(t, m.loadCurrentDiffFile())
	fd := m.currentFileDiff()
	for i, line := range fd.Lines {
		if line.NewNum > 0 && strings.Contains(line.Text, "println") {
			m.review.SetCursorDiffLine(i, m.diffCodeHeight())
		}
	}
	m.openAnnotate()
	typeReviewAnnotation(m, "use fmt.Println here")
	m.applyCmd(t, m.saveAnnotation())

	setReviewSideBySide(m, false)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "use fmt.Println here") {
		t.Fatalf("comment missing in unified layout:\n%s", view)
	}
	setReviewSideBySide(m, true)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "use fmt.Println here") {
		t.Fatalf("comment missing in split layout:\n%s", view)
	}
}

func TestHandledCommentsStayVisibleWithAMutedColor(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	m := &Model{}
	fd := &diff.FileDiff{File: git.ChangedFile{Path: "main.go"}, Lines: []diff.Line{{NewNum: 1, Text: "line"}}, IsLoaded: true}
	seedReviewStateForTest(m, uireview.Target{ID: "abc123"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{*fd}}, uireview.SavedState{Comments: []uireview.Comment{
			{ID: "0123456789abcdef", File: "main.go", Line: 1, Text: "still open", Round: 2, Point: 1},
			{ID: "fedcba9876543210", File: "main.go", Line: 1, Text: "already fixed", Round: 1, Point: 3, Resolved: true},
		}})
	rendered := m.viewDiffCode(80, 10)
	plain := ansi.Strip(rendered)
	for _, want := range []string{"Review round 2 · point 1 · open still open", "Review round 1 · point 3 · handled already fixed"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("comment history is missing %q:\n%s", want, plain)
		}
	}
	if uitheme.AnnotationBg() == uitheme.HandledAnnotationBg() || !strings.Contains(rendered, uitheme.AnnotationBg()) || !strings.Contains(rendered, uitheme.HandledAnnotationBg()) {
		t.Fatalf("open and handled comments should use different washes: %q", rendered)
	}
	if uitheme.HandledAnnotationBg() != uitheme.BgSeq(uitheme.Mix(uitheme.Current().Bg, uitheme.Current().Finished, 0.14)) || !strings.Contains(rendered, uitheme.FgSeq(uitheme.Current().Finished)) {
		t.Fatalf("handled comment should use the theme's finished green: %q", rendered)
	}
}

// A silent same-scope reload that shifts line numbers re-points saved comments
// at the line carrying their excerpt, so the agent gets the location meant.
func TestAnnotationsReanchorAfterRefresh(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "anchor", dir)
	m.pressDiffKey(t, 'n') // jump to the changed line (return 10)
	m.openAnnotate()
	typeReviewAnnotation(m, "note")
	m.applyCmd(t, m.saveAnnotation())
	notes := m.review.Annotations()
	if len(notes) != 1 || notes[0].Line != 3 {
		t.Fatalf("annotation = %+v, want line 3", notes)
	}

	shifted := "package a\n\n// pushed down\nfunc A() int { return 10 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	if notes = m.review.Annotations(); len(notes) != 1 || notes[0].Line != 4 {
		t.Fatalf("annotation after refresh = %+v, want line 4", notes)
	}
}

func TestReviewRoundTracksOutdatedAndHandledComments(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "rounds", dir)
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	typeReviewAnnotation(m, "verify this return value")
	m.applyCmd(t, m.saveAnnotation())
	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)

	shifted := "package a\n\n// pushed down\nfunc A() int { return 10 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	notes := m.review.Annotations()
	if len(notes) != 1 || notes[0].Line != 4 || notes[0].Outdated {
		t.Fatalf("re-anchored round comment = %+v", notes)
	}

	replaced := "package a\n\n// pushed down\nfunc A() int { return 11 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	notes = m.review.Annotations()
	if !notes[0].Outdated {
		t.Fatalf("changed comment should be outdated: %+v", notes[0])
	}
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, "Review round 1 · point 1 · open · outdated") {
		t.Fatalf("outdated round comment is not visible:\n%s", view)
	}

	fd := m.currentFileDiff()
	for i, line := range fd.Lines {
		if line.NewNum == notes[0].Line && !notes[0].Deleted {
			m.setCursorDiffLine(i)
			break
		}
	}
	m.applyCmd(t, m.discardOrToggleAnnotation())
	if !m.review.Annotations()[0].Resolved {
		t.Fatal("d should mark a sent comment handled")
	}
	state, err := m.services.store.ReviewState(m.review.Snapshot().SessionID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || !state.Comments[0].Resolved || !state.Comments[0].Outdated {
		t.Fatalf("persisted handled comment = %+v", state.Comments)
	}

	note := m.review.Annotations()[0]
	m.handleReviewComment(uireview.HandleCommentResult{
		TargetID: m.review.Snapshot().SessionID, RepoRoot: m.review.Snapshot().RepoSelected,
		CommentID: note.ID, Handled: note.Resolved, Previous: false,
	})
	if m.review.Annotations()[0].Resolved {
		t.Fatal("a comment the store no longer holds should drop back to open")
	}
	if m.errBar.text == "" {
		t.Fatal("a failed handled toggle should reach the status line")
	}
}

func TestAgentHandledUpdateReloadsWithoutDroppingTheComment(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "handled", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	typeReviewAnnotation(m, "fix this")
	m.applyCmd(t, m.saveAnnotation())
	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	note := m.review.Annotations()[0]
	if found, err := m.services.store.SetReviewCommentHandled(m.review.Snapshot().SessionID, note.ID, true); err != nil || !found {
		t.Fatalf("agent update = %v, %v", found, err)
	}
	m.review.SetCursorDiffLine(m.review.CursorDiffLine(), m.diffCodeHeight())
	m.openAnnotate()
	typeReviewAnnotation(m, "keep this draft")
	m.applyCmd(t, m.saveAnnotation())
	m.applyCmd(t, m.reviewStatusesCmd())
	notes := m.review.Annotations()
	if len(notes) != 2 || !notes[0].Resolved || notes[0].Round != 1 || notes[0].Point != 1 || notes[1].Text != "keep this draft" {
		t.Fatalf("reloaded history = %+v", notes)
	}
}

// An ambiguous excerpt (blank line, or several identical lines) never moves the
// comment, and re-anchoring never stacks two comments onto one line.
func TestReanchorKeepsAmbiguousAndAvoidsCollapse(t *testing.T) {
	m := &Model{}
	lineOf := func(kind diff.LineKind, num int, text string) diff.Line {
		return diff.Line{Kind: kind, NewNum: num, Text: text}
	}
	set := diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{{
		File: git.ChangedFile{Path: "f.go"},
	}}}
	request := seedReviewStateForTest(m, uireview.Target{ID: "s1"}, git.ScopeUncommitted, "/repo", set, uireview.SavedState{Comments: []uireview.Comment{
		{File: "f.go", Line: 2, Text: "blank"},
		{File: "f.go", Line: 5, Excerpt: "}", Text: "first brace"},
		{File: "f.go", Line: 9, Excerpt: "}", Text: "second brace"},
		{File: "f.go", Line: 12, Excerpt: "unique()", Text: "moves"},
	}})
	m.review.ApplyFile(uireview.FileResult{
		TargetID: "s1", Scope: git.ScopeUncommitted, Generation: request.Generation,
		RepoRoot: "/repo", Index: 0, Path: "f.go", File: diff.FileDiff{
			File:     git.ChangedFile{Path: "f.go"},
			IsLoaded: true,
			Lines: []diff.Line{
				lineOf(diff.Same, 1, ""),
				lineOf(diff.Same, 2, "}"), // one of the two braces survived
				lineOf(diff.Same, 3, "unique()"),
			},
		}})
	notes := m.review.Annotations()
	if notes[0].Line != 2 {
		t.Errorf("blank excerpt should not move: line=%d", notes[0].Line)
	}
	// Two '}' notes, one surviving brace: unique match, but the second must not
	// collapse onto the first's new anchor.
	if notes[1].Line == notes[2].Line {
		t.Errorf("two comments collapsed onto line %d", notes[1].Line)
	}
	if notes[3].Line != 3 {
		t.Errorf("unique excerpt should move to line 3: line=%d", notes[3].Line)
	}
}

// Sending review comments writes into the pane the same way the quick
// prompt does, so it refuses a shell for the same reason: the prompt is an
// English sentence, and a shell would run it.
func TestSendAnnotationsRefusesAShell(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := gitTestRepo(t)
	if err := m.services.store.CreateGroup("work", dir); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	sess := spawnTerminal(t, m)
	m.selectSessionRow(t, sess.Name)
	m.drainCmds(t, m.openDiff())

	fd := m.currentFileDiff()
	for i, line := range fd.Lines {
		if line.Kind != diff.Gap {
			m.review.SetCursorDiffLine(i, m.diffCodeHeight())
			break
		}
	}
	m.openAnnotate()
	typeReviewAnnotation(m, "use fmt.Println here")
	m.applyCmd(t, m.saveAnnotation())
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("a refused send must not return a command")
	}
	if m.errBar.text != uisession.ShellPromptHint(sess.Name) {
		t.Fatalf("err = %q, want the shell refusal", m.errBar.text)
	}
	if len(m.review.Annotations()) != 1 {
		t.Fatal("a refused send should keep the comments")
	}
}

func TestDiffSendConfirmIgnoresMotionKeys(t *testing.T) {
	m := &Model{screen: screen{base: baseReview}}
	seedReviewStateForTest(m, uireview.Target{ID: "s1"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{diff.BuildFile(nil, []byte("line\n"), git.ChangedFile{Path: "main.go"}, git.FileStat{})}},
		uireview.SavedState{Comments: []uireview.Comment{{File: "main.go", Line: 1, Text: "keep me"}}})
	m.review.SetSendConfirm(true)
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if !m.review.Snapshot().SendConfirm {
		t.Fatal("j should leave the send prompt up")
	}
	if len(m.review.Annotations()) != 1 {
		t.Fatal("j must not send or drop comments")
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.review.Snapshot().SendConfirm {
		t.Fatal("esc should cancel the send prompt")
	}
	if len(m.review.Annotations()) != 1 {
		t.Fatal("cancel should keep the comments")
	}
}

func TestAnotherScopeDoesNotOutdateARoundsComments(t *testing.T) {
	m := &Model{}
	target := uireview.Target{ID: "scopeoutdated"}
	comment := uireview.Comment{ID: "0123456789abcdef", File: "gone-from-this-scope.go", Line: 1, Text: "look at this", Round: 1, Point: 1}
	seedReviewStateForTest(m, target, git.ScopeUncommitted, "/repo", diff.Set{Repo: git.Repo{Root: "/repo"}}, uireview.SavedState{
		Comments: []uireview.Comment{comment}, Round: uireview.Round{Number: 1, Scope: git.ScopeUncommitted.String()},
	})
	if !m.review.Annotations()[0].Outdated {
		t.Fatal("a file missing from the scope the round was sent in should read outdated")
	}
	seedReviewStateForTest(m, target, git.ScopeUncommitted, "/repo", diff.Set{Repo: git.Repo{Root: "/repo"}}, uireview.SavedState{
		Comments: []uireview.Comment{comment}, Round: uireview.Round{Number: 1, Scope: git.ScopeBranch.String()},
	})
	if m.review.Annotations()[0].Outdated {
		t.Fatal("the comment was labelled outdated by a scope it was not sent in")
	}
}

// Each round's comments are judged against the scope that round was sent in:
// an older round from another scope stays untouched even when the latest
// round was sent in the current scope.
func TestOlderRoundFromAnotherScopeIsNotOutdated(t *testing.T) {
	m := &Model{}
	seedReviewStateForTest(m, uireview.Target{ID: "roundscopes"}, git.ScopeUncommitted, "/repo", diff.Set{Repo: git.Repo{Root: "/repo"}}, uireview.SavedState{
		Comments: []uireview.Comment{
			{ID: "aaaaaaaaaaaaaaa1", File: "gone-from-this-scope.go", Line: 1, Text: "older round", Round: 1, Point: 1, Scope: git.ScopeBranch.String()},
			{ID: "aaaaaaaaaaaaaaa2", File: "also-gone.go", Line: 1, Text: "latest round", Round: 2, Point: 1, Scope: git.ScopeUncommitted.String()},
		},
		Round: uireview.Round{Number: 2, Scope: git.ScopeUncommitted.String()},
	})
	notes := m.review.Annotations()
	if notes[0].Outdated {
		t.Fatal("a round sent in another scope was outdated by this scope's file list")
	}
	if !notes[1].Outdated {
		t.Fatal("the current scope's round comment kept its standing")
	}
}

// A scope cycle re-judges the arriving scope's comments even without a
// refresh: a comment whose file that scope no longer lists reads outdated.
func TestScopeCycleOutdatesTheArrivingScopesComments(t *testing.T) {
	m := &Model{}
	seedReviewStateForTest(m, uireview.Target{ID: "cycleoutdate"}, git.ScopeUncommitted, "/repo", diff.Set{Repo: git.Repo{Root: "/repo"}}, uireview.SavedState{
		Comments: []uireview.Comment{{ID: "aaaaaaaaaaaaaaa1", File: "gone.go", Line: 1, Text: "from staged", Round: 1, Point: 1, Scope: git.ScopeStaged.String()}},
		Round:    uireview.Round{Number: 1, Scope: git.ScopeStaged.String()},
	})
	for m.review.Snapshot().Scope != git.ScopeStaged {
		request, ok := m.review.CycleScope()
		if !ok {
			t.Fatal("scope cycle rejected")
		}
		m.review.ApplyLoad(uireview.LoadResult{
			TargetID: request.Target.ID, Scope: request.Scope, Generation: request.Generation,
			RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: diff.Set{Repo: git.Repo{Root: "/repo"}},
		})
	}
	if !m.review.Annotations()[0].Outdated {
		t.Fatal("arriving at staged should outdate its round comment on a missing file")
	}
}

// A same-scope refresh re-anchors only that scope's comments: another
// scope renders the same file differently, so its comment keeps the line
// and hash it was made against.
func TestRefreshDoesNotReanchorOtherScopesComments(t *testing.T) {
	m := &Model{}
	other := git.ScopeBranch.String()
	request := seedReviewStateForTest(m, uireview.Target{ID: "scopereanchor"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}},
		uireview.SavedState{
			Comments: []uireview.Comment{{ID: "aaaaaaaaaaaaaaa1", File: "main.go", Line: 999, Excerpt: "func main() { println(1) }", Text: "from another scope", Round: 1, Point: 1, Scope: other, ContentHash: 12345}},
			Round:    uireview.Round{Number: 1, Scope: other},
		})
	m.review.ApplyFile(uireview.FileResult{
		TargetID: request.Target.ID, Scope: request.Scope, Generation: request.Generation,
		RepoRoot: "/repo", Index: 0, Path: "main.go",
		File: diff.BuildFile(nil, []byte("func main() { println(1) }\n"), git.ChangedFile{Path: "main.go"}, git.FileStat{}),
	})
	if note := m.review.Annotations()[0]; note.Line != 999 || note.ContentHash != 12345 {
		t.Fatalf("the comment moved: line=%d hash=%d", note.Line, note.ContentHash)
	}
}

func TestMigratedPointsNeverRepeatWithinARound(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	const repo = "/repo"
	if err := m.services.store.SetReviewState("pts123", repo, store.ReviewState{
		Comments: []store.ReviewComment{
			{ID: "aaaaaaaaaaaaaaa1", File: "a.go", Line: 1, Text: "no point", Round: 1},
			{ID: "aaaaaaaaaaaaaaa2", File: "a.go", Line: 2, Text: "point one", Round: 1, Point: 1},
			{ID: "aaaaaaaaaaaaaaa3", File: "a.go", Line: 3, Text: "also none", Round: 1},
		},
		Round: store.ReviewRound{Number: 1},
	}); err != nil {
		t.Fatal(err)
	}

	state, err := uireviewsource.ReadState(m.services.store, "pts123", repo)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int]string{}
	for _, note := range state.Comments {
		if note.Point == 0 {
			t.Fatalf("comment %s kept point 0", note.ID)
		}
		if other, taken := seen[note.Point]; taken {
			t.Fatalf("comments %s and %s share point %d", other, note.ID, note.Point)
		}
		seen[note.Point] = note.ID
	}
}

func TestAnnotationsDropControlBytes(t *testing.T) {
	const escape = "\x1b[31mred\x07\x1b]0;title\x07"
	if got := uireview.SanitizeText(escape); strings.ContainsFunc(got, func(r rune) bool {
		return r != '\n' && unicode.IsControl(r)
	}) {
		t.Fatalf("control bytes survived: %q", got)
	}
	if got := uireview.SanitizeText("keep\tthe\nshape"); got != "keep the\nshape" {
		t.Fatalf("tab and newline handling = %q", got)
	}
	if got := uireview.Excerpt("\x1b[2Jfunc main() {"); got != "[2Jfunc main() {" {
		t.Fatalf("excerpt = %q, want the escape introducer gone", got)
	}
}

func TestAnnotateSkipsHunkGaps(t *testing.T) {
	fd := bigEditedFile(t)
	m := &Model{}
	seedReviewForTest(m, uireview.Target{ID: "s1"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{fd}}, true)
	for _, split := range []bool{false, true} {
		setReviewSideBySide(m, split)
		m.review.SetCursorDiffLine(0, 10)
		m.openAnnotate()
		if m.review.Snapshot().Annotating {
			t.Fatalf("split=%v: gap marker must not accept comments", split)
		}
		m.review.SetCursorDiffLine(1, 10)
		m.openAnnotate()
		if !m.review.Snapshot().Annotating {
			t.Fatalf("split=%v: real context line should accept comments", split)
		}
		m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
}

func TestReviewedMarkClearsOnContentChange(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "reset", dir)
	if len(m.review.Snapshot().Set.Files) == 0 {
		t.Fatal("want at least one changed file")
	}
	path := m.review.Snapshot().Set.Files[0].File.Path

	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed(path) {
		t.Fatal("file should be reviewed after toggle")
	}

	original, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil {
		t.Fatal(err)
	}
	changed := string(original) + "\nfunc Added() {}"
	if err := os.WriteFile(filepath.Join(dir, path), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	m.refreshDiff(t)
	if m.fileReviewed(path) {
		t.Fatal("reviewed mark should reset after content changes")
	}
}

// A scope that does not list a file says nothing about whether it changed,
// so its mark waits for the scope that shows it again.
func TestAScopeMissingAFileKeepsItsReviewedMark(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scopemarks", gitTestRepo(t))
	fd := m.currentFileDiff()
	if fd == nil {
		t.Fatal("review has no selected file")
	}
	path := fd.File.Path
	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed(path) {
		t.Fatalf("%s was not marked reviewed", path)
	}

	state := m.review.SavedState()
	seedReviewStateForTest(m, uireview.Target{ID: "scopemarks"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}}, state)
	if !m.fileReviewed(path) {
		t.Fatalf("the mark for %s did not survive a scope without it", path)
	}
}

// One trip around the scope cycle must not destroy a saved reviewed mark:
// the same file renders different diff lines - so a different content hash -
// under another scope, and only the scope a mark was taken in may judge it
// stale.
func TestScopeCycleKeepsReviewedMark(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitTestRepo(t)
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// Stage the current edit and add another on top, so the staged and
	// uncommitted scopes both list main.go with different rendered diffs.
	runGit("add", "main.go")
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() { println(2) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "scopecycle", dir)
	target := -1
	for i, fd := range m.review.Snapshot().Set.Files {
		if fd.File.Path == "main.go" {
			target = i
		}
	}
	if target < 0 {
		t.Fatalf("main.go missing from the uncommitted scope: %+v", m.review.Snapshot().Set.Files)
	}
	m.drainCmds(t, m.switchDiffFile(target-m.review.Snapshot().FileIndex))
	m.drainCmds(t, m.toggleReviewed())
	if !m.fileReviewed("main.go") {
		t.Fatal("main.go was not marked reviewed")
	}

	for cycle := 0; cycle < 4; cycle++ {
		m.drainCmds(t, m.cycleDiffScope())
		if m.review.Snapshot().Scope != git.ScopeUncommitted && m.fileReviewed("main.go") {
			t.Fatalf("a mark taken under uncommitted read as reviewed under %q", m.review.Snapshot().Scope)
		}
	}
	if m.review.Snapshot().Scope != git.ScopeUncommitted {
		t.Fatalf("four cycles should return to uncommitted, got %q", m.review.Snapshot().Scope)
	}
	if !m.fileReviewed("main.go") {
		t.Fatal("cycling scopes destroyed the reviewed mark")
	}
}

func TestReviewSendCommandCapturesSessionIncarnation(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	createSession(t, m, "review-incarnation", t.TempDir(), "")
	session := reviewSessionByName(t, m, "review-incarnation")
	m.review = uireview.New(false)
	m.review.Open(uireview.Target{ID: session.ID, Name: session.Name, Tool: session.Tool, Cwd: session.Cwd}, 0, "/repo")
	m.base = baseReview
	m.reviewSendCmd(uireview.SendRequest{
		Target: uireview.Target{ID: session.ID, Name: session.Name}, RepoRoot: "/repo", Prompt: "prompt",
		State: uireview.SavedState{Round: uireview.Round{Number: 1}}, Round: 1, Count: 1,
	})
	if err := m.services.store.SetAgentLaunchedAt(session.ID, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	m.drainEffects(t)
	state, err := m.services.store.ReviewState(session.ID, "/repo")
	if err != nil || state.Round.Number != 0 {
		t.Fatalf("queued send reached replacement incarnation: state=%+v err=%v", state, err)
	}
	if !strings.Contains(m.errBar.text, "target changed") {
		t.Fatalf("relaunch refusal was not surfaced: %q", m.errBar.text)
	}
}

func TestReviewSaveDefersToEffectLane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "savelane", gitRepoWithTwoChangedFiles(t))
	sess := reviewSessionByName(t, m, "savelane")
	path := m.currentFileDiff().File.Path
	mark := m.reviewedMarkKey(path)

	cmd := m.toggleReviewed()
	state, err := m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := state.Reviewed[mark]; set {
		t.Fatal("save wrote the store on the update path")
	}
	m.applyCmd(t, cmd)
	state, err = m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if _, set := state.Reviewed[mark]; !set {
		t.Fatal("accepted save did not persist through the effect lane")
	}
}

func TestReviewSaveFailureSurfacesAfterReviewChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Model)
	}{
		{"closed", func(m *Model) { m.review.Close(); m.base = baseList }},
		{"retargeted", func(m *Model) { _, _ = m.review.SelectRepo("/other") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{}
			m.review = uireview.New(false)
			m.review.Open(uireview.Target{ID: "s1", Cwd: "/repo"}, 0, "/repo")
			m.base = baseReview
			tc.mutate(m)
			before := m.screenID()

			cmd := m.handleReviewSave(uireview.SaveResult{TargetID: "s1", RepoRoot: "/repo", Err: errors.New("disk full")})
			if cmd != nil {
				t.Fatal("save failure started navigation")
			}
			if m.screenID() != before || !strings.Contains(m.errBar.text, "saving review state: disk full") {
				t.Fatalf("accepted save failure hidden or navigated: screen = %v err=%q", m.screenID(), m.errBar.text)
			}
		})
	}
}

func TestReviewStatusSeesPriorSave(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "statuslane", gitRepoWithTwoChangedFiles(t))
	sess := reviewSessionByName(t, m, "statuslane")
	repo := m.review.Snapshot().RepoSelected

	if err := m.services.store.MergeReviewState(sess.ID, repo, store.ReviewState{
		Comments: []store.ReviewComment{{ID: "sent1", File: "a.go", Line: 1, Text: "t", Round: 2, Resolved: true}},
		Round:    store.ReviewRound{Number: 2},
	}); err != nil {
		t.Fatal(err)
	}
	m.toggleReviewed() // enqueues a save behind which the status rides
	m.reviewStatusCmd(uireview.StatusRequest{TargetID: sess.ID, RepoRoot: repo, Generation: m.review.Snapshot().Generation})
	m.drainEffects(t)

	state, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Reviewed) == 0 {
		t.Fatal("save did not persist")
	}
	if m.errBar.text != "" {
		t.Fatalf("status reconciliation failed: %q", m.errBar.text)
	}
}

func TestReviewSendDrainsOnQuit(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "sendquit", gitRepoWithTwoChangedFiles(t))
	sess := reviewSessionByName(t, m, "sendquit")

	m.openAnnotate()
	typeReviewAnnotation(m, "ship it")
	m.saveAnnotation()
	m.drainEffects(t)

	m.sendAnnotations()
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 1 {
		t.Fatalf("send not queued: active=%v pending=%d", m.effects.Main.Active, len(m.effects.Main.Pending))
	}
	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded an accepted review send")
	}
	m.drainEffects(t)
	if len(m.effects.Main.Pending) != 0 {
		t.Fatalf("quit left review work in the queue: %d pending", len(m.effects.Main.Pending))
	}
	state, err := m.services.store.ReviewState(sess.ID, m.review.Snapshot().RepoSelected)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number == 0 {
		t.Fatal("accepted send did not drain to a persisted round on quit")
	}
	if _, ok := m.nextEffectCmd()().(tea.QuitMsg); !ok {
		t.Fatal("drained queue did not quit")
	}
}

func TestReviewLoadNormalizesThroughLane(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := uireviewsource.Canonical(gitRepoWithTwoChangedFiles(t))
	createSession(t, m, "norm", repo, "")
	sess := reviewSessionByName(t, m, "norm")
	if err := m.services.store.SetReviewState(sess.ID, repo, store.ReviewState{
		Comments: []store.ReviewComment{
			{File: "a.go", Line: 2, Text: "first", Round: 3},
			{File: "b.go", Line: 4, Text: "second", Round: 3},
		},
		Round: store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	m.selectSessionRow(t, "norm")
	m.drainCmds(t, m.openDiff())
	if m.base != baseReview {
		t.Fatalf("openDiff should enter review, err = %q", m.errBar.text)
	}

	state, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 2 || state.Comments[0].ID == "" || state.Comments[0].Point != 1 || state.Comments[1].Point != 2 {
		t.Fatalf("stored state not normalized through the lane: %+v", state.Comments)
	}
	if len(m.review.SavedState().Comments) != 2 || m.review.SavedState().Comments[0].ID == "" {
		t.Fatalf("in-memory state not normalized: %+v", m.review.SavedState())
	}

	m.drainCmds(t, m.cycleDiffScope())
	m.drainEffects(t)
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 0 {
		t.Fatalf("reload left review work in the lane: active=%v pending=%d", m.effects.Main.Active, len(m.effects.Main.Pending))
	}
	state, err = m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 2 || state.Comments[0].ID == "" {
		t.Fatalf("reload changed the normalized state: %+v", state.Comments)
	}
}

func TestReviewSendOnceRefusesDuplicateSubmit(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "sendonce", gitRepoWithTwoChangedFiles(t))

	m.openAnnotate()
	typeReviewAnnotation(m, "once")
	m.saveAnnotation()
	m.drainEffects(t)

	_, cmd := m.sendAnnotations()
	if m.effects.Main.Active != nil || len(m.effects.Main.Pending) != 1 {
		t.Fatalf("send did not reach the effect lane: active=%v pending=%d", m.effects.Main.Active, len(m.effects.Main.Pending))
	}
	second := m.review.BeginSend()
	if second.Error == "" || second.Requests.Send != nil {
		t.Fatalf("duplicate send accepted: %+v", second)
	}
	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("duplicate submit enqueued a second job: %d pending", len(m.effects.Main.Pending))
	}
	m.applyCmd(t, cmd)
}

func TestReviewNormalizeDispatchesAsNormalizeOp(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := uireviewsource.Canonical(gitRepoWithTwoChangedFiles(t))
	createSession(t, m, "normop", repo, "")
	sess := reviewSessionByName(t, m, "normop")
	if err := m.services.store.SetReviewState(sess.ID, repo, store.ReviewState{
		Comments: []store.ReviewComment{{File: "a.go", Line: 2, Text: "first", Round: 3}},
		Round:    store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	m.selectSessionRow(t, "normop")
	open := m.openDiff()
	_, ready := m.handleReviewPreferences(open().(uireviewsource.PreferencesMsg))
	batch, ok := ready().(tea.BatchMsg)
	if !ok {
		t.Fatalf("openDiff command = %T, want batch", open)
	}
	var load uireviewsource.LoadMsg
	for _, child := range batch {
		if child == nil {
			continue
		}
		if msg, ok := child().(uireviewsource.LoadMsg); ok {
			load = msg
		}
	}
	if load.Normalize == nil {
		t.Fatal("load did not capture a normalization to persist")
	}
	if load.Normalize.TargetID != sess.ID || load.Normalize.RepoRoot != repo {
		t.Fatalf("normalize captured %s in %s, want %s in %s", load.Normalize.TargetID, load.Normalize.RepoRoot, sess.ID, repo)
	}

	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, load)
	m.drainEffects(t)
	if !strings.Contains(m.errBar.text, "persisting review normalization") {
		t.Fatalf("failed normalize surfaced as: %q", m.errBar.text)
	}
	if strings.Contains(m.errBar.text, "saving review round") {
		t.Fatalf("normalize failure invoked send/save reconciliation: %q", m.errBar.text)
	}
}

func TestStaleNormalizeDoesNotOverwriteNewerSave(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	repo := uireviewsource.Canonical(gitRepoWithTwoChangedFiles(t))
	createSession(t, m, "stalenorm", repo, "")
	sess := reviewSessionByName(t, m, "stalenorm")

	oldShape := store.ReviewState{
		Comments: []store.ReviewComment{{File: "a.go", Line: 1, Text: "old", Round: 3}},
		Round:    store.ReviewRound{Number: 3},
	}
	newer := store.ReviewState{
		Comments: []store.ReviewComment{{ID: "n1", File: "b.go", Line: 9, Text: "new", Round: 4, Point: 1}},
		Round:    store.ReviewRound{Number: 4},
	}
	if err := m.services.store.SetReviewState(sess.ID, repo, oldShape); err != nil {
		t.Fatal(err)
	}
	m.enqueueEffect(uireviewsource.Change{Request: uireviewsource.Request{Op: uireviewsource.Save, TargetID: sess.ID, RepoRoot: repo, State: uireviewsource.StateFromStore(newer)}}, 0, false)
	stale, _ := uireview.NormalizeSavedState(uireviewsource.StateFromStore(oldShape))
	m.enqueueEffect(uireviewsource.Change{Request: uireviewsource.Request{Op: uireviewsource.Normalize, TargetID: sess.ID, RepoRoot: repo, State: stale}}, 0, false)
	m.drainEffects(t)

	got, err := m.services.store.ReviewState(sess.ID, repo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Round.Number != 4 || len(got.Comments) != 1 || got.Comments[0].ID != "n1" || got.Comments[0].Text != "new" {
		t.Fatalf("stale normalize overwrote the newer state: %+v", got)
	}
}

func reviewSessionByName(t *testing.T, m *Model, name string) store.Session {
	t.Helper()
	for _, sess := range m.sessionRows() {
		if sess.Name == name {
			return sess
		}
	}
	t.Fatalf("session %q not found", name)
	return store.Session{}
}
