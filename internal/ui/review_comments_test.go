package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestDiffAnnotateAndSend(t *testing.T) {
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
	out, err := tmuxCmd("capture-pane", "-p", "-J", "-t", "am_"+sess.ID).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	pane := string(out)
	if !strings.Contains(pane, "use fmt.Println here") || !strings.Contains(pane, "main.go:3") ||
		!strings.Contains(pane, "[comment "+notes[0].ID+"]") || !strings.Contains(pane, "review_comment") {
		t.Fatalf("prompt not delivered:\n%s", pane)
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
	m := buildModel(t)
	openReviewOn(t, m, "persist-first", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	typeReviewAnnotation(m, "do not deliver without durable state")
	m.applyCmd(t, m.saveAnnotation())
	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
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
	out, err := tmuxCmd("capture-pane", "-p", "-J", "-t", "am_"+sess.ID).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "do not deliver without durable state") {
		t.Fatalf("unpersisted review reached the pane:\n%s", out)
	}
}

func TestDiffCommentVisibleInBothLayouts(t *testing.T) {
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
	fd := &diff.FileDiff{File: git.ChangedFile{Path: "main.go"}, Lines: []diff.Line{{NewNum: 1, Text: "line"}}}
	seedReviewStateForTest(m, uireview.Target{ID: "abc123"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{*fd}}, uireview.SavedState{Comments: []uireview.Comment{
			{ID: "0123456789abcdef", File: "main.go", Line: 1, Text: "still open", Round: 2, Point: 1},
			{ID: "fedcba9876543210", File: "main.go", Line: 1, Text: "already fixed", Round: 1, Point: 3, Resolved: true},
		}})
	rows := m.annotationRows(fd, 0, 80)
	rendered := strings.Join(rows, "\n")
	plain := ansi.Strip(rendered)
	for _, want := range []string{"Review round 2 · point 1 · open still open", "Review round 1 · point 3 · handled already fixed"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("comment history is missing %q:\n%s", want, plain)
		}
	}
	if annotationBg() == handledAnnotationBg() || !strings.Contains(rendered, annotationBg()) || !strings.Contains(rendered, handledAnnotationBg()) {
		t.Fatalf("open and handled comments should use different washes: %q", rendered)
	}
	if handledAnnotationBg() != bgSeq(mix(current.Bg, current.Finished, 0.14)) || !strings.Contains(rendered, fgSeq(current.Finished)) {
		t.Fatalf("handled comment should use the theme's finished green: %q", rendered)
	}
}

// A silent same-scope reload that shifts line numbers re-points saved comments
// at the line carrying their excerpt, so the agent gets the location meant.
func TestAnnotationsReanchorAfterRefresh(t *testing.T) {
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

func TestExcerptKeepsRuneBoundary(t *testing.T) {
	line := "  " + strings.Repeat("ש", 70)
	excerpt := uireview.Excerpt(line)
	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt split a rune: %q", excerpt)
	}
	if got := len([]rune(excerpt)); got != 60 {
		t.Fatalf("excerpt rune count = %d, want 60", got)
	}
	if short := uireview.Excerpt("  short  "); short != "short" {
		t.Fatalf("short excerpt = %q", short)
	}
}

// Sending review comments writes into the pane the same way the quick
// prompt does, so it refuses a shell for the same reason: the prompt is an
// English sentence, and a shell would run it.
func TestSendAnnotationsRefusesAShell(t *testing.T) {
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
	if m.errBar.text != shellPromptHint(sess.Name) {
		t.Fatalf("err = %q, want the shell refusal", m.errBar.text)
	}
	if len(m.review.Annotations()) != 1 {
		t.Fatal("a refused send should keep the comments")
	}
}

func TestDiffSendConfirmIgnoresMotionKeys(t *testing.T) {
	m := &Model{mode: modeDiff}
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

func TestDiffCommentBoxGrowsWithText(t *testing.T) {
	m := &Model{width: 100, height: 30, mode: modeDiff}
	seedReviewForTest(m, uireview.Target{ID: "s1"}, git.ScopeUncommitted, "/repo",
		diff.Set{Repo: git.Repo{Root: "/repo"}, Files: []diff.FileDiff{diff.BuildFile(nil, []byte("line\n"), git.ChangedFile{Path: "main.go"}, git.FileStat{})}}, true)
	m.openAnnotate()
	if m.annotationInputHeight(40) != 1 {
		t.Fatal("empty comment should stay one row")
	}
	typeReviewAnnotation(m, strings.Repeat("word ", 40))
	got := m.annotationInputHeight(40)
	if got <= 1 {
		t.Fatalf("long comment stayed %d rows", got)
	}
	if got > annotationInputMaxRows {
		t.Fatalf("comment box grew past the cap: %d", got)
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

	state, err := readReviewState(m.services.store, "pts123", repo)
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
