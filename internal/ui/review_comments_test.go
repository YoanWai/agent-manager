package ui

import (
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestDiffAnnotateAndSend(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	m.diff.sideBySide = false

	for i, fd := range m.diff.set.Files {
		if fd.File.Path == "main.go" {
			m.diff.fileIdx = i
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
	m.diff.cursorLine = target
	m.openAnnotate()
	m.diff.annInput.SetValue("use fmt.Println here")
	m.applyCmd(t, m.saveAnnotation())
	if len(m.diff.annotations[m.reviewKey()]) != 1 {
		t.Fatalf("annotations = %+v", m.diff.annotations)
	}

	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 1 || notes[0].round != 1 || notes[0].point != 1 || len(notes[0].id) != 16 {
		t.Fatalf("sent annotations = %+v, want one comment in round 1", notes)
	}
	if !strings.Contains(m.diff.notice, "review round 1 (1 comment)") {
		t.Fatalf("notice = %q (err=%q)", m.diff.notice, m.errBar.text)
	}
	sess := m.sessionRows()[0]
	state, err := m.services.store.ReviewState(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 1 || state.Round.Fingerprint != m.diff.fingerprint ||
		len(state.Comments) != 1 || state.Comments[0].Round != 1 || state.Comments[0].Point != 1 || state.Comments[0].ID != notes[0].id {
		t.Fatalf("persisted review round = %+v", state)
	}
	originalFingerprint := m.diff.fingerprint
	m.diff.fingerprint++
	if header := ansi.Strip(m.viewDiffHeader(sess.Name)); !strings.Contains(header, "Review round 1 · changed") {
		t.Fatalf("changed-since-round marker missing: %q", header)
	}
	m.diff.fingerprint = originalFingerprint
	originalScope := m.diff.scope
	m.diff.scope = originalScope.Next()
	if header := ansi.Strip(m.viewDiffHeader(sess.Name)); !strings.Contains(header, "Review round 1 · changed") {
		t.Fatalf("scope change did not mark the round changed: %q", header)
	}
	m.diff.scope = originalScope
	// Join wrapped lines so the delivery check does not depend on where the
	// pane's width breaks the prompt; the session sizes to the model width.
	out, err := tmuxCmd("capture-pane", "-p", "-J", "-t", "am_"+sess.ID).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	pane := string(out)
	if !strings.Contains(pane, "use fmt.Println here") || !strings.Contains(pane, "main.go:3") ||
		!strings.Contains(pane, "[comment "+notes[0].id+"]") || !strings.Contains(pane, "review_comment") {
		t.Fatalf("prompt not delivered:\n%s", pane)
	}

	m.openAnnotate()
	m.diff.annInput.SetValue("second pass")
	m.applyCmd(t, m.saveAnnotation())
	_, cmd = m.sendAnnotations()
	m.applyCmd(t, cmd)
	notes = m.diff.annotations[m.reviewKey()]
	if len(notes) != 2 || notes[0].round != 1 || notes[1].round != 2 {
		t.Fatalf("review history = %+v, want rounds 1 and 2", notes)
	}
	state, err = m.services.store.ReviewState(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 2 {
		t.Fatalf("second persisted review round = %+v", state)
	}
}

func TestSendAnnotationsDoesNotDeliverAnUnpersistedRound(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "persist-first", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("do not deliver without durable state")
	m.applyCmd(t, m.saveAnnotation())
	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}

	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 1 || notes[0].round != 0 || m.diff.rounds[m.reviewKey()].Number != 0 {
		t.Fatalf("failed send did not restore the draft: notes=%+v round=%+v", notes, m.diff.rounds[m.reviewKey()])
	}
	if m.diff.reviewSendPending || m.diff.notice != "" || !strings.Contains(m.errBar.text, "saving review round") {
		t.Fatalf("failed send state: pending=%v notice=%q err=%q", m.diff.reviewSendPending, m.diff.notice, m.errBar.text)
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
	m.diff.sideBySide = false

	for i, fd := range m.diff.set.Files {
		if fd.File.Path == "main.go" {
			m.diff.fileIdx = i
		}
	}
	m.drainCmds(t, m.loadCurrentDiffFile())
	fd := m.currentFileDiff()
	for i, line := range fd.Lines {
		if line.NewNum > 0 && strings.Contains(line.Text, "println") {
			m.diff.cursorLine = i
		}
	}
	m.openAnnotate()
	m.diff.annInput.SetValue("use fmt.Println here")
	m.applyCmd(t, m.saveAnnotation())

	m.diff.sideBySide = false
	if view := ansi.Strip(m.View()); !strings.Contains(view, "use fmt.Println here") {
		t.Fatalf("comment missing in unified layout:\n%s", view)
	}
	m.diff.sideBySide = true
	if view := ansi.Strip(m.View()); !strings.Contains(view, "use fmt.Println here") {
		t.Fatalf("comment missing in split layout:\n%s", view)
	}
}

func TestHandledCommentsStayVisibleWithAMutedColor(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	m := &Model{diff: diffState{
		sessID: "abc123", repoSel: "/repo",
		annotations: map[string][]annotation{
			"abc123\x00/repo": {
				{id: "0123456789abcdef", file: "main.go", line: 1, text: "still open", round: 2, point: 1},
				{id: "fedcba9876543210", file: "main.go", line: 1, text: "already fixed", round: 1, point: 3, handled: true},
			},
		},
	}}
	fd := &diff.FileDiff{File: git.ChangedFile{Path: "main.go"}, Lines: []diff.Line{{NewNum: 1, Text: "line"}}}
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
	m.diff.annInput.SetValue("note")
	m.applyCmd(t, m.saveAnnotation())
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 1 || notes[0].line != 3 {
		t.Fatalf("annotation = %+v, want line 3", notes)
	}

	shifted := "package a\n\n// pushed down\nfunc A() int { return 10 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	if notes = m.diff.annotations[m.reviewKey()]; len(notes) != 1 || notes[0].line != 4 {
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
	m.diff.annInput.SetValue("verify this return value")
	m.applyCmd(t, m.saveAnnotation())
	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)

	shifted := "package a\n\n// pushed down\nfunc A() int { return 10 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(shifted), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 1 || notes[0].line != 4 || notes[0].outdated {
		t.Fatalf("re-anchored round comment = %+v", notes)
	}

	replaced := "package a\n\n// pushed down\nfunc A() int { return 11 }\n"
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte(replaced), 0o644); err != nil {
		t.Fatal(err)
	}
	m.refreshDiff(t)
	notes = m.diff.annotations[m.reviewKey()]
	if !notes[0].outdated {
		t.Fatalf("changed comment should be outdated: %+v", notes[0])
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "Review round 1 · point 1 · open · outdated") {
		t.Fatalf("outdated round comment is not visible:\n%s", view)
	}

	fd := m.currentFileDiff()
	for i, line := range fd.Lines {
		if line.NewNum == notes[0].line && !notes[0].deleted {
			m.setCursorDiffLine(i)
			break
		}
	}
	m.applyCmd(t, m.discardOrToggleAnnotation())
	if !m.diff.annotations[m.reviewKey()][0].handled {
		t.Fatal("d should mark a sent comment handled")
	}
	state, err := m.services.store.ReviewState(m.diff.sessID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || !state.Comments[0].Resolved || !state.Comments[0].Outdated {
		t.Fatalf("persisted handled comment = %+v", state.Comments)
	}

	note := m.diff.annotations[m.reviewKey()][0]
	m.handleReviewCommentHandled(reviewCommentHandledMsg{
		sessID: m.diff.sessID, repoRoot: m.diff.repoSel,
		commentID: note.id, handled: note.handled, previous: false,
	})
	if m.diff.annotations[m.reviewKey()][0].handled {
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
	m.diff.annInput.SetValue("fix this")
	m.applyCmd(t, m.saveAnnotation())
	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	note := m.diff.annotations[m.reviewKey()][0]
	if found, err := m.services.store.SetReviewCommentHandled(m.diff.sessID, note.id, true); err != nil || !found {
		t.Fatalf("agent update = %v, %v", found, err)
	}
	m.diff.annotations[m.reviewKey()] = append(m.diff.annotations[m.reviewKey()], annotation{
		id: "localdraft000001", file: note.file, line: note.line, text: "keep this draft",
	})
	m.applyCmd(t, m.reviewStatusesCmd())
	notes := m.diff.annotations[m.reviewKey()]
	if len(notes) != 2 || !notes[0].handled || notes[0].round != 1 || notes[0].point != 1 || notes[1].text != "keep this draft" {
		t.Fatalf("reloaded history = %+v", notes)
	}
}

// An ambiguous excerpt (blank line, or several identical lines) never moves the
// comment, and re-anchoring never stacks two comments onto one line.
func TestReanchorKeepsAmbiguousAndAvoidsCollapse(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	m.diff.sessID = "s1"
	m.diff.annotations = map[string][]annotation{m.reviewKey(): {
		{file: "f.go", line: 2, excerpt: "", text: "blank"},
		{file: "f.go", line: 5, excerpt: "}", text: "first brace"},
		{file: "f.go", line: 9, excerpt: "}", text: "second brace"},
		{file: "f.go", line: 12, excerpt: "unique()", text: "moves"},
	}}
	lineOf := func(kind diff.LineKind, num int, text string) diff.Line {
		return diff.Line{Kind: kind, NewNum: num, Text: text}
	}
	m.diff.set = diff.Set{Files: []diff.FileDiff{{
		File: git.ChangedFile{Path: "f.go"},
		Lines: []diff.Line{
			lineOf(diff.Same, 1, ""),
			lineOf(diff.Same, 2, "}"), // one of the two braces survived
			lineOf(diff.Same, 3, "unique()"),
		},
	}}}
	m.reanchorAnnotationsFor("")
	notes := m.diff.annotations[m.reviewKey()]
	if notes[0].line != 2 {
		t.Errorf("blank excerpt should not move: line=%d", notes[0].line)
	}
	// Two '}' notes, one surviving brace: unique match, but the second must not
	// collapse onto the first's new anchor.
	if notes[1].line == notes[2].line {
		t.Errorf("two comments collapsed onto line %d", notes[1].line)
	}
	if notes[3].line != 3 {
		t.Errorf("unique excerpt should move to line 3: line=%d", notes[3].line)
	}
}

func TestExcerptKeepsRuneBoundary(t *testing.T) {
	line := "  " + strings.Repeat("ש", 70)
	excerpt := excerptOf(line)
	if !utf8.ValidString(excerpt) {
		t.Fatalf("excerpt split a rune: %q", excerpt)
	}
	if got := len([]rune(excerpt)); got != 60 {
		t.Fatalf("excerpt rune count = %d, want 60", got)
	}
	if short := excerptOf("  short  "); short != "short" {
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

	m.diff.annotations[m.reviewKey()] = []annotation{{file: "main.go", line: 3, text: "use fmt.Println here"}}
	if _, cmd := m.sendAnnotations(); cmd != nil {
		t.Fatal("a refused send must not return a command")
	}
	if m.errBar.text != shellPromptHint(sess.Name) {
		t.Fatalf("err = %q, want the shell refusal", m.errBar.text)
	}
	if len(m.diff.annotations[m.reviewKey()]) != 1 {
		t.Fatal("a refused send should keep the comments")
	}
}

func TestDiffSendConfirmIgnoresMotionKeys(t *testing.T) {
	m := &Model{
		mode: modeDiff,
		diff: diffState{
			active:      true,
			sendConfirm: true,
			annotations: map[string][]annotation{
				"\x00": {{file: "main.go", line: 1, text: "keep me"}},
			},
		},
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if !m.diff.sendConfirm {
		t.Fatal("j should leave the send prompt up")
	}
	if len(m.diff.annotations[m.reviewKey()]) != 1 {
		t.Fatal("j must not send or drop comments")
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.diff.sendConfirm {
		t.Fatal("esc should cancel the send prompt")
	}
	if len(m.diff.annotations[m.reviewKey()]) != 1 {
		t.Fatal("cancel should keep the comments")
	}
}

func TestDiffCommentBoxGrowsWithText(t *testing.T) {
	m := &Model{width: 100, height: 30, mode: modeDiff}
	m.diff.annInput = textarea.New()
	m.diff.annotating = true
	if m.annotationInputHeight(40) != 1 {
		t.Fatal("empty comment should stay one row")
	}
	m.diff.annInput.SetValue(strings.Repeat("word ", 40))
	got := m.annotationInputHeight(40)
	if got <= 1 {
		t.Fatalf("long comment stayed %d rows", got)
	}
	if got > annotationInputMaxRows {
		t.Fatalf("comment box grew past the cap: %d", got)
	}
}

func TestAnotherScopeDoesNotOutdateARoundsComments(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scopeoutdated", gitRepoWithTwoChangedFiles(t))
	key := m.reviewKey()
	m.diff.annotations[key] = []annotation{{
		id: "0123456789abcdef", file: "gone-from-this-scope.go", line: 1,
		text: "look at this", round: 1, point: 1,
	}}
	m.diff.rounds[key] = store.ReviewRound{Number: 1, Scope: m.diff.scope.String()}

	if !m.markMissingRoundCommentsOutdated() {
		t.Fatal("a file missing from the scope the round was sent in should read outdated")
	}
	m.diff.annotations[key][0].outdated = false

	m.diff.rounds[key] = store.ReviewRound{Number: 1, Scope: m.diff.scope.Next().String()}
	if m.markMissingRoundCommentsOutdated() {
		t.Fatal("another scope's file list marked the round outdated")
	}
	if m.diff.annotations[key][0].outdated {
		t.Fatal("the comment was labelled outdated by a scope it was not sent in")
	}
}

// Each round's comments are judged against the scope that round was sent in:
// an older round from another scope stays untouched even when the latest
// round was sent in the current scope.
func TestOlderRoundFromAnotherScopeIsNotOutdated(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "roundscopes", gitRepoWithTwoChangedFiles(t))
	key := m.reviewKey()
	m.diff.annotations[key] = []annotation{
		{id: "aaaaaaaaaaaaaaa1", file: "gone-from-this-scope.go", line: 1,
			text: "older round", round: 1, point: 1, scope: m.diff.scope.Next().String()},
		{id: "aaaaaaaaaaaaaaa2", file: "also-gone.go", line: 1,
			text: "latest round", round: 2, point: 1, scope: m.diff.scope.String()},
	}
	m.diff.rounds[key] = store.ReviewRound{Number: 2, Scope: m.diff.scope.String()}

	if !m.markMissingRoundCommentsOutdated() {
		t.Fatal("the current scope's round comment on a missing file should read outdated")
	}
	notes := m.diff.annotations[key]
	if notes[0].outdated {
		t.Fatal("a round sent in another scope was outdated by this scope's file list")
	}
	if !notes[1].outdated {
		t.Fatal("the current scope's round comment kept its standing")
	}
}

// A scope cycle re-judges the arriving scope's comments even without a
// refresh: a comment whose file that scope no longer lists reads outdated.
func TestScopeCycleOutdatesTheArrivingScopesComments(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "cycleoutdate", gitTestRepo(t))
	key := m.reviewKey()
	m.diff.annotations[key] = []annotation{{
		id: "aaaaaaaaaaaaaaa1", file: "gone.go", line: 1,
		text: "from staged", round: 1, point: 1, scope: git.ScopeStaged.String(),
	}}
	m.diff.rounds[key] = store.ReviewRound{Number: 1, Scope: git.ScopeStaged.String()}

	for m.diff.scope != git.ScopeStaged {
		m.drainCmds(t, m.cycleDiffScope())
	}
	if !m.diff.annotations[key][0].outdated {
		t.Fatal("arriving at staged should outdate its round comment on a missing file")
	}
}

// A same-scope refresh re-anchors only that scope's comments: another
// scope renders the same file differently, so its comment keeps the line
// and hash it was made against.
func TestRefreshDoesNotReanchorOtherScopesComments(t *testing.T) {
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scopereanchor", gitTestRepo(t))
	key := m.reviewKey()
	other := m.diff.scope.Next().String()
	m.diff.annotations[key] = []annotation{{
		id: "aaaaaaaaaaaaaaa1", file: "main.go", line: 999,
		excerpt: "func main() { println(1) }", text: "from another scope",
		round: 1, point: 1, scope: other, hash: 12345,
	}}
	m.diff.rounds[key] = store.ReviewRound{Number: 1, Scope: other}

	if m.reanchorAnnotationsFor("main.go") {
		t.Fatal("a refresh in this scope re-anchored another scope's comment")
	}
	if note := m.diff.annotations[key][0]; note.line != 999 || note.hash != 12345 {
		t.Fatalf("the comment moved: line=%d hash=%d", note.line, note.hash)
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
	if got := withoutControlBytes(escape); strings.ContainsFunc(got, func(r rune) bool {
		return r != '\n' && unicode.IsControl(r)
	}) {
		t.Fatalf("control bytes survived: %q", got)
	}
	if got := withoutControlBytes("keep\tthe\nshape"); got != "keep the\nshape" {
		t.Fatalf("tab and newline handling = %q", got)
	}
	if got := excerptOf("\x1b[2Jfunc main() {"); got != "[2Jfunc main() {" {
		t.Fatalf("excerpt = %q, want the escape introducer gone", got)
	}
}

func TestAnnotateSkipsHunkGaps(t *testing.T) {
	fd := bigEditedFile(t)
	m := &Model{diff: diffState{active: true, set: diff.Set{Files: []diff.FileDiff{fd}}}}
	for _, split := range []bool{false, true} {
		m.diff.sideBySide = split
		m.diff.cursorLine = 0
		m.openAnnotate()
		if m.diff.annotating {
			t.Fatalf("split=%v: gap marker must not accept comments", split)
		}
		m.diff.cursorLine = 1
		m.openAnnotate()
		if !m.diff.annotating {
			t.Fatalf("split=%v: real context line should accept comments", split)
		}
		m.diff.annotating = false
	}
}
