package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Ctrl+C quits from the comment editor and the send-confirm prompt, not just
// the base review keymap. The pickers are surfaces: review_adapter_test.go
// shows ctrl+c closing the card and leaving Review as it was.
func TestReviewCtrlCQuitsFromSubmodes(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "subquit", gitRepoWithTwoChangedFiles(t))
	m.openAnnotate()
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while annotating")
	}
	m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.review.SetSendConfirm(true)
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit from the send-confirm prompt")
	}
}

// Ctrl+C quits from review mode like it does from the list.
func TestReviewCtrlCQuits(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "quitter", gitRepoWithTwoChangedFiles(t))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in review should return a command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c in review should quit")
	}
}

func TestDiffHelpReturnsToReview(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := gitTestRepo(t)
	openReviewOn(t, m, "helper", dir)
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if !helpOpen(m) {
		t.Fatalf("? should open the key map, screen = %v", m.screenID())
	}
	if !m.review.Snapshot().Active {
		t.Fatal("opening help must not close the review")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.base != baseReview || !m.review.Snapshot().Active {
		t.Fatalf("esc should return to review, screen = %v active = %v", m.screenID(), m.review.Snapshot().Active)
	}
}

func TestDiffHelpKeepsLoaderRunningOnReturn(t *testing.T) {
	m := helpModel()
	m.clearSurfaces()
	m.base = baseReview
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader")
	}
	m.openHelp()
	m.Update(uistartup.TickMsg{})
	if !m.startup.Animating {
		t.Fatal("review is still loading under help, so the loader keeps ticking")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.startup.Animating {
		t.Fatal("returning to a loading review should leave the loader running")
	}
}

// reviewModel opens Review on a session whose repo has two changed files.
func reviewModel(t *testing.T) *Model {
	t.Helper()
	dir := gitRepoWithLongFile(t, 40)
	if err := os.WriteFile(filepath.Join(dir, "second.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := buildModel(t)
	m.layout.width, m.layout.height = 160, 40
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if state := m.review.Snapshot(); state.Loading || len(state.Set.Files) < 2 {
		t.Fatalf("test setup: the diff should list two files, %+v", state.Error)
	}
	return m
}

func TestClickOnAReviewFileOpensIt(t *testing.T) {
	m := reviewModel(t)
	files := m.review.Snapshot().Set.Files
	other := 1 - m.review.Snapshot().FileIndex
	x, y := paintedCell(t, m, filepath.Base(files[other].File.Path))
	updated, cmd := m.handleMouse(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = updated.(*Model)
	m.drainCmds(t, cmd)
	if got := m.review.Snapshot().FileIndex; got != other {
		t.Fatalf("a click on %s should open it, file index %d", files[other].File.Path, got)
	}
}

func TestClickOnACodeLineMovesTheCursor(t *testing.T) {
	m := reviewModel(t)
	for m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path != "long.txt" {
		m.drainCmds(t, m.switchDiffFile(1))
	}
	for _, split := range []bool{true, false} {
		setReviewSideBySide(m, split)
		m.review.First()
		x, y := paintedCell(t, m, "line-007")
		m = leftPress(m, x, y)
		fd, _ := m.review.CurrentFile()
		if got := m.review.CursorDiffLine(); fd.Lines[got].Text != "line-007" {
			t.Fatalf("split=%v: a click on line-007 should move the cursor there, at %q", split, fd.Lines[got].Text)
		}
	}
}

func TestClickOnAReviewHeaderPillRunsItsKey(t *testing.T) {
	m := reviewModel(t)
	side := m.review.Snapshot().SideBySide
	header := ansi.Strip(strings.Split(preparedView(m), "\n")[0])
	col := strings.Index(header, "u  ")
	if col < 0 {
		t.Fatalf("test setup: no layout pill in %q", header)
	}
	if m = leftClick(m, ansi.StringWidth(header[:col]), 0); m.review.Snapshot().SideBySide == side {
		t.Fatal("a click on the layout pill should switch the layout")
	}
}

func TestClickOnTheSendQuestionAnswersIt(t *testing.T) {
	m := reviewModel(t)
	m.openAnnotate()
	typeReviewAnnotation(m, "fix this")
	m.applyCmd(t, m.saveAnnotation())
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	if !m.review.Snapshot().SendConfirm {
		t.Fatalf("test setup: C should ask to send, err %q", m.errBar.text)
	}
	x, y := paintedCell(t, m, "↵/y send")
	if _, ok := m.layout.keyHits.At(x, y); !ok {
		t.Fatal("the send question's own keys should be click targets")
	}
	if m = clickLabel(t, m, "esc cancel"); m.review.Snapshot().SendConfirm || m.base != baseReview {
		t.Fatalf("esc in the question should cancel it, confirm %v mode %v", m.review.Snapshot().SendConfirm, m.screenID())
	}
}

// Ctrl+R inside a session opens review and remembers the session, so leaving
// review returns to it rather than dropping to the list.
func TestInSessionReviewRemembersOriginAndReattaches(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m, sess.ID)

	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	m.drainEffects(t)
	*m = *updated.(*Model)

	if m.base != baseReview {
		t.Fatalf("marker set should enter review, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if m.reviewNav.Ret().Kind != uireviewsource.ReturnAttach || m.reviewNav.Ret().SessionID != sess.ID {
		t.Fatalf("review origin = %+v, want attach %q", m.reviewNav.Ret(), sess.ID)
	}

	// esc leaves review; the live origin session re-attaches.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if !m.onList() {
		t.Fatalf("esc should leave review, screen = %v", m.screenID())
	}
	if m.reviewNav.Ret() != (uireviewsource.Return{}) {
		t.Fatalf("reattach origin should be consumed, got %+v", m.reviewNav.Ret())
	}
	if cmd == nil {
		t.Fatal("esc from in-session review should re-attach the session, got nil command")
	}
}

// Review opened from the list has no origin, so esc returns to the list with
// no re-attach.
func TestListReviewLeavesToListWithoutReattach(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "listreview", t.TempDir(), "")
	m.selectSessionRow(t, "listreview")

	m.drainCmds(t, m.openDiff())
	if m.base != baseReview {
		t.Fatalf("openDiff should enter review, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}
	if m.reviewNav.Ret().Kind != uireviewsource.ReturnList || m.reviewNav.Ret().SessionID != "" {
		t.Fatalf("list review should return to list, got %+v", m.reviewNav.Ret())
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if !m.onList() {
		t.Fatalf("esc should return to list, screen = %v", m.screenID())
	}
	if cmd != nil {
		t.Fatal("list review esc should not re-attach")
	}
}

// Leaving review back into a session acknowledges a finished alert, matching
// what entering the session from the list does.
func TestReattachAcknowledgesFinished(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "finisher", t.TempDir(), "")
	m.selectSessionRow(t, "finisher")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m, sess.ID)

	if err := m.services.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if _, err := sessionTmuxCmd(t, sess.ID, "set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	m.drainEffects(t)
	*m = *updated.(*Model)
	if m.base != baseReview {
		t.Fatalf("expected review, screen = %v, err = %q", m.screenID(), m.errBar.text)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("esc should re-attach, err = %q", m.errBar.text)
	}
	if foreground := m.foregroundTestCmd(t, cmd); foreground == nil {
		t.Fatal("prepared reattach did not emit terminal command")
	}
	got, err := m.services.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("re-attach should acknowledge finished: status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestStaleReattachDoesNotInterruptReopenedReview(t *testing.T) {
	m := &Model{screen: screen{base: baseReview}}
	seedReviewForTest(m, uireview.Target{ID: "new"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	m.effects.Main.Active = &uieffects.Job{ID: 1, Request: uilifecycle.AttachRequest{ID: "old", Reattach: true, Generation: 0}}
	updated, cmd := m.Update(uieffects.Completed{ID: 1, Result: uilifecycle.AttachResult{}})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale re-attach should not return an attach command")
	}
	if m.base != baseReview || !m.review.Snapshot().Active {
		t.Fatal("stale re-attach should leave the reopened review untouched")
	}
}

func TestReviewCloseReleasesStateAndIgnoresLateLoad(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "close", gitRepoWithTwoChangedFiles(t))
	load := m.switchDiffFile(1)
	if load == nil {
		t.Fatal("switch should leave a file load in flight")
	}

	if cmd := m.closeDiff(); cmd != nil {
		t.Fatal("list-opened review should close without re-attaching")
	}
	if !m.onList() || m.review.Snapshot().Active || m.review.Snapshot().SessionID != "" {
		t.Fatalf("review did not close cleanly: screen = %v active=%v session=%q",
			m.screenID(), m.review.Snapshot().Active, m.review.Snapshot().SessionID)
	}
	if len(m.review.Snapshot().Set.Files) != 0 || m.review.CurrentHighlight() != nil {
		t.Fatal("close should release diff and pending highlight state immediately")
	}

	late := load()
	updated, next := m.Update(late)
	*m = *updated.(*Model)
	if next != nil || len(m.review.Snapshot().Set.Files) != 0 || m.review.Snapshot().Active {
		t.Fatal("a file load landing after close must be ignored")
	}
}

func TestCtrlRFromListOpensReview(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "ctrlr", gitRepoWithTwoChangedFiles(t), "")
	m.selectSessionRow(t, "ctrlr")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.base != baseReview {
		t.Fatalf("ctrl+r from the list should open review, screen = %v (err=%q)", m.screenID(), m.errBar.text)
	}
	if m.reviewNav.Ret().Kind != uireviewsource.ReturnList || m.reviewNav.Ret().SessionID != "" {
		t.Fatal("review opened from the list should return to the list, not re-attach")
	}
}

type reviewReadProbe struct {
	calls int
	err   error
}

func (r *reviewReadProbe) ReviewScope(string) (string, error) { r.calls++; return "branch", r.err }
func (r *reviewReadProbe) ReviewRepo(string) (string, error)  { r.calls++; return "", r.err }

func TestReviewOpeningDefersPreferencesAndKeepsReadErrors(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-read", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	reader := &reviewReadProbe{err: errors.New("read denied")}
	cmd := m.openDiffWithReader(reader)
	if reader.calls != 0 {
		t.Fatal("opening review read preferences on Update")
	}
	msg := cmd().(uireviewsource.PreferencesMsg)
	_, _ = m.handleReviewPreferences(msg)
	if m.review.Scope() != git.ScopeUncommitted {
		t.Fatal("failed preference read fabricated a scope")
	}
	if !strings.Contains(m.errBar.text, "read denied") {
		t.Fatalf("read error lost: %q", m.errBar.text)
	}
}

func TestReviewPreferencesCannotRetargetReopenedReview(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-stale", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	cmd := m.openDiffWithReader(&reviewReadProbe{})
	m.review.Close()
	m.review.Open(uireviewsource.Target(s), git.ScopeStaged, "")
	gen := m.review.Generation()
	_, next := m.handleReviewPreferences(cmd().(uireviewsource.PreferencesMsg))
	if next != nil || m.review.Generation() != gen || m.review.Scope() != git.ScopeStaged {
		t.Fatal("stale preferences replaced reopened review")
	}
}

func TestReviewPreferencesCannotOpenDuringQuitOrAfterRelaunch(t *testing.T) {
	for _, quitting := range []bool{false, true} {
		t.Run(map[bool]string{false: "relaunch", true: "quit"}[quitting], func(t *testing.T) {
			m := buildModel(t)
			if m.services.gitDrv == nil {
				t.Skip("git unavailable")
			}
			s := store.Session{ID: "review-changed", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
			m.workspace.sessions = []store.Session{s}
			m.rebuildRows()
			m.selectSessionRow(t, s.Name)
			cmd := m.openDiffWithReader(&reviewReadProbe{})
			gen := m.review.Generation()
			if quitting {
				m.effects.Quitting = true
			} else {
				m.workspace.sessions[0].AgentLaunchedAt = time.Now()
			}
			_, next := m.handleReviewPreferences(cmd().(uireviewsource.PreferencesMsg))
			if next != nil || m.review.Generation() != gen {
				t.Fatal("late preferences initiated a load")
			}
		})
	}
}

func TestReviewPreferencesFinishBehindHelpWithoutStealingIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-help", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	cmd := m.openDiffWithReader(&reviewReadProbe{})
	m.openHelp()
	_, load := m.handleReviewPreferences(cmd().(uireviewsource.PreferencesMsg))
	if !helpOpen(m) || load == nil || m.review.Scope() != git.ScopeBranch {
		t.Fatal("help interrupted review preparation or was replaced")
	}
}

func TestReviewPreferencesFinishBehindPickerWithoutLeavingLoadingStuck(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git unavailable")
	}
	s := store.Session{ID: "review-picker-prefs", Name: "read", Cwd: t.TempDir(), Tool: "claude"}
	m.workspace.sessions = []store.Session{s}
	m.rebuildRows()
	m.selectSessionRow(t, s.Name)
	m.ledger.lifecycle.PickedRepos = map[string]string{s.ID: s.Cwd}
	cmd := m.openDiffWithReader(&reviewReadProbe{})
	state := m.review.Snapshot()
	m.openReviewPicker([]uireview.PickerRow{{Label: "branch", Root: s.Cwd}}, "branches", uireview.PickRepo, s.Cwd, uireview.PickerSource{Generation: state.Generation, TargetID: s.ID, RepoRoot: s.Cwd}, "")
	_, load := m.handleReviewPreferences(cmd().(uireviewsource.PreferencesMsg))
	if !m.pickerOpen() || load == nil {
		t.Fatal("picker stranded the sole review load")
	}
	if m.review.PickerSourceCurrent(m.pickerOf(t).Source()) {
		t.Fatal("old picker survived review preparation")
	}
}

func TestReviewFocusReturnKeepsItsCapturedSession(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	a := store.Session{ID: "return-a", Name: "A", Tool: "claude"}
	b := store.Session{ID: "return-b", Name: "B", Tool: "claude"}
	m.workspace.sessions = []store.Session{a, b}
	m.rebuildRows()
	m.selectSessionRow(t, b.Name)
	m.review.Open(uireviewsource.Target(a), git.ScopeUncommitted, "")
	m.base = baseReview
	m.reviewNav.Set(uireviewsource.Return{Kind: uireviewsource.ReturnFocus, SessionID: a.ID})
	m.closeDiff()
	if m.effects.Main.Active == nil || m.effects.Main.Active.Request.(focusruntime.ProbeRequest).SessionID != a.ID {
		t.Fatal("review returned into a different session")
	}
}

// The cursor's line has to be inside the rows review actually paints. A
// viewport taller than its painted area lets the cursor walk past the last
// visible row: the selection is at the end of the file, the screen is not.
func TestDiffCursorStaysOnScreen(t *testing.T) {
	t.Parallel()
	const lines = 400
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.review.Snapshot().Loading || len(m.review.Snapshot().Set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.review.Snapshot().Error)
	}

	for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}, {120, 40}, {160, 50}} {
		m.layout.width, m.layout.height = size.w, size.h
		m.review.First()
		m.moveDiffCursor(lines*2, m.diffCodeHeight())
		m.prepareReviewLayout()

		fd := m.currentFileDiff()
		if fd == nil {
			t.Fatal("no file diff")
		}
		want := fd.Lines[m.review.CursorDiffLine()]
		if want.Text == "" {
			continue
		}
		view := ansi.Strip(m.viewDiffFull())
		if !strings.Contains(view, strings.TrimSpace(want.Text)) {
			t.Errorf("%dx%d: cursor sits on %q, which the frame never paints",
				size.w, size.h, strings.TrimSpace(want.Text))
		}
	}
}

// The end of a file has to be reachable in review. A viewport sized larger
// than the rows the frame actually paints leaves a tail of lines the cursor
// can address but the screen never shows, which is how the last screenful
// silently went missing once before.
func TestDiffReviewReachesLastLine(t *testing.T) {
	t.Parallel()
	const lines = 400
	for _, layout := range []struct {
		name  string
		split bool
	}{{"unified", false}, {"side-by-side", true}} {
		t.Run(layout.name, func(t *testing.T) {
			m := buildModel(t)
			dir := gitRepoWithLongFile(t, lines)
			createSession(t, m, "coder", dir, "")
			m.selectSessionRow(t, "coder")
			m.drainCmds(t, m.openDiff())
			if m.review.Snapshot().Loading || len(m.review.Snapshot().Set.Files) == 0 {
				t.Fatalf("diff did not load: %q", m.review.Snapshot().Error)
			}
			setReviewSideBySide(m, layout.split)

			last := fmt.Sprintf("line-%03d", lines)
			view := ansi.Strip(preparedView(m))
			if strings.Contains(view, last) {
				t.Fatalf("the file's end should start off screen, got:\n%s", view)
			}

			// G jumps to the end; the final line must be painted, not merely
			// selected, and the cursor must sit on it.
			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
			m.prepareReviewLayout()
			view = ansi.Strip(preparedView(m))
			if !strings.Contains(view, last) {
				t.Fatalf("G should paint the last line %q, got:\n%s", last, view)
			}
			// The whole tail has to be on screen, not just the final line:
			// a viewport that outruns its painted rows shows the last line
			// and silently drops the ones just above it.
			for n := lines; n > lines-6; n-- {
				if !strings.Contains(view, fmt.Sprintf("line-%03d", n)) {
					t.Fatalf("line %d missing from the end of the file, got:\n%s", n, view)
				}
			}
		})
	}
}

// Stepping down one line at a time has to arrive at the same place G does:
// if the viewport is taller than the painted rows, the walk stops short and
// the tail of the file becomes unreachable by keyboard.
func TestDiffReviewStepsDownToTheEnd(t *testing.T) {
	t.Parallel()
	const lines = 120
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.review.Snapshot().Loading || len(m.review.Snapshot().Set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.review.Snapshot().Error)
	}

	for i := 0; i < lines*2; i++ {
		m.handleDiffKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	m.prepareReviewLayout()
	last := fmt.Sprintf("line-%03d", lines)
	if view := ansi.Strip(preparedView(m)); !strings.Contains(view, last) {
		t.Fatalf("stepping down should reach the last line %q, got:\n%s", last, view)
	}
}

// Long lines wrap onto several painted rows, but the cursor and scroll
// count logical lines. If the window math ignores the wraps, every wrapped
// line on screen pushes one more line of the file's tail off the bottom:
// the cursor lands on the last line and the screen never shows it.
func TestDiffReviewReachesEndWithWrappedLines(t *testing.T) {
	t.Parallel()
	const lines = 80
	for _, layout := range []struct {
		name  string
		split bool
	}{{"unified", false}, {"side-by-side", true}} {
		t.Run(layout.name, func(t *testing.T) {
			m := buildModel(t)
			dir := gitRepoWithWideFile(t, lines, 220)
			createSession(t, m, "coder", dir, "")
			m.selectSessionRow(t, "coder")
			m.drainCmds(t, m.openDiff())
			if m.review.Snapshot().Loading || len(m.review.Snapshot().Set.Files) == 0 {
				t.Fatalf("diff did not load: %q", m.review.Snapshot().Error)
			}
			m.layout.width, m.layout.height = 120, 34
			setReviewSideBySide(m, layout.split)

			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
			m.prepareReviewLayout()
			last := fmt.Sprintf("wide-%03d", lines)
			if view := ansi.Strip(preparedView(m)); !strings.Contains(view, last) {
				t.Fatalf("G should paint the last line %q, got:\n%s", last, view)
			}

			// Stepping down must keep the cursor painted the whole way.
			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
			for i := 0; i < lines+5; i++ {
				m.handleDiffKey(tea.KeyMsg{Type: tea.KeyDown})
				m.prepareReviewLayout()
				fd := m.currentFileDiff()
				lineIdx := m.review.CursorDiffLine()
				if fd == nil || lineIdx >= len(fd.Lines) {
					t.Fatal("cursor out of range")
				}
				marker := strings.Fields(fd.Lines[lineIdx].Text)[0]
				if view := ansi.Strip(preparedView(m)); !strings.Contains(view, marker) {
					t.Fatalf("step %d: cursor on %q but the frame never paints it:\n%s", i, marker, view)
				}
			}
		})
	}
}

// Marking a file reviewed advances to the next unreviewed file; the advanced
// file must still get its syntax highlighting.
func TestSpaceAdvanceKeepsHighlight(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "hl", gitRepoWithTwoChangedFiles(t))
	if len(m.review.Snapshot().Set.Files) != 2 {
		t.Fatalf("want 2 files, got %d (err=%q)", len(m.review.Snapshot().Set.Files), m.review.Snapshot().Error)
	}
	if m.review.CurrentHighlight() == nil {
		t.Fatal("first file should be highlighted after open")
	}
	m.pressDiffKey(t, ' ')
	if m.review.Snapshot().FileIndex != 1 {
		t.Fatalf("space should advance to the next file, idx = %d", m.review.Snapshot().FileIndex)
	}
	if m.review.CurrentHighlight() == nil {
		t.Error("advanced file lost its highlight: switch command was dropped")
	}
}

// Scroll positions are per session and per scope; a second session touching
// the same path must open the file at the top.
func TestScrollDoesNotLeakAcrossSessions(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "one", dir)
	firstFile := m.review.Snapshot().Set.Files[0].File.Path
	m.review.SetCursorDiffLine(2, 1)
	m.drainCmds(t, m.switchDiffFile(1)) // persists scroll for file one

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	openReviewOn(t, m, "two", dir)
	m.drainCmds(t, m.switchDiffFile(1))
	m.drainCmds(t, m.switchDiffFile(1)) // wraps back to the first file
	if fd := m.currentFileDiff(); fd == nil || fd.File.Path != firstFile {
		t.Fatalf("expected to land back on %q", firstFile)
	}
	if m.review.Snapshot().Scroll != 0 {
		t.Errorf("session two inherited session one's scroll: %d", m.review.Snapshot().Scroll)
	}
}

// f drops the files git calls binary out of the review, and neither the
// selection nor a file switch is left on one of them.
func TestReviewCodeOnlyHidesBinaryFiles(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "filter", gitRepoWithBinaryBetweenTextFiles(t))

	binary := -1
	for i := range m.review.Snapshot().Set.Files {
		if m.review.Snapshot().Set.Files[i].File.Path == "b.dat" {
			binary = i
		}
	}
	if binary < 0 {
		t.Fatalf("b.dat missing from the diff set: %+v", m.review.Snapshot().Set.Files)
	}
	if !m.review.Snapshot().Set.Files[binary].Stat.Binary {
		t.Fatal("numstat should mark b.dat binary before its content is read")
	}
	m.drainCmds(t, selectReviewFile(m, binary))

	m.pressFilterKey(t)
	if !m.review.Snapshot().CodeOnly {
		t.Fatal("f should turn the code-only filter on")
	}
	if m.review.Snapshot().FileIndex == binary {
		t.Fatal("the selection should leave a file the filter hides")
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "b.dat") {
		t.Fatalf("b.dat should be filtered out of the file list:\n%s", list)
	}
	if !strings.Contains(list, "a.go") || !strings.Contains(list, "c.go") {
		t.Fatalf("the code files should stay listed:\n%s", list)
	}

	for i := range m.review.Snapshot().Set.Files {
		if m.review.Snapshot().Set.Files[i].File.Path == "a.go" {
			m.drainCmds(t, selectReviewFile(m, i))
		}
	}
	m.drainCmds(t, m.switchDiffFile(1))
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "c.go" {
		t.Fatalf("the file after a.go = %q, want c.go", got)
	}
	m.drainCmds(t, m.switchDiffFile(-1))
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "a.go" {
		t.Fatalf("the file before c.go = %q, want a.go", got)
	}

	m.pressFilterKey(t)
	if m.review.Snapshot().CodeOnly {
		t.Fatal("f again should show the binary files")
	}
	if list := ansi.Strip(m.viewDiffFileList(60, 20)); !strings.Contains(list, "b.dat") {
		t.Fatalf("b.dat should be back in the file list:\n%s", list)
	}
}

// A newly written image and a regenerated lock file are the common case, and
// neither has a git verdict to go on: the filter has to settle them by name so
// they go the moment the key is pressed and stay gone across a silent reload.
func TestReviewCodeOnlyHidesUntrackedAndLockFiles(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := writeGitRepo(t,
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 1 }\n",
			"go.sum": "mod v1.0.0 h1:aaa=\n",
			"src.go": "package a\n\nfunc S() int { return 2 }\n",
		},
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 10 }\n",
			"go.sum": "mod v1.0.1 h1:bbb=\n",
			"src.go": "package a\n\nfunc S() int { return 20 }\n",
			"z.png":  "\x89PNG\r\n\x1a\n\x00\x00\x00\x00new",
		})
	openReviewOn(t, m, "blobs", dir)

	untracked := m.fileDiffByPath("z.png")
	if untracked == nil {
		t.Fatalf("z.png missing from the diff set: %+v", m.review.Snapshot().Set.Files)
	}
	if untracked.Loaded() {
		t.Fatal("an untracked file the cursor never reached should stay unloaded")
	}
	if !untracked.StatKnown() || !untracked.Stat.Binary {
		t.Fatal("an untracked image should already count as binary in the file list")
	}

	m.pressFilterKey(t)
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "z.png") || strings.Contains(list, "go.sum") {
		t.Fatalf("the image and the lock file should be off the list:\n%s", list)
	}
	if !strings.Contains(list, "a.go") || !strings.Contains(list, "src.go") {
		t.Fatalf("the code files should stay listed:\n%s", list)
	}

	// Reverting the file under the cursor drops it from the reloaded set, so the
	// selection falls on go.sum unless the reload settles it on the spot. The
	// load it schedules is drained afterwards: the list is rendered between the
	// two, and a cursor parked on a hidden row draws no cursor at all.
	writeRepoFile(t, dir, "a.go", "package a\n\nfunc A() int { return 1 }\n")
	m.refreshDiff(t)
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "src.go" {
		t.Fatalf("the reload should settle the selection itself, landed on %q", got)
	}
	if list := ansi.Strip(m.viewDiffFileList(60, 20)); strings.Contains(list, "z.png") {
		t.Fatalf("the filter should survive a silent reload:\n%s", list)
	}
}

// A blob whose name gives nothing away is only outed when its load sniffs a
// NUL, so the cursor has to be carried off it the way every other file switch
// moves: with the file it lands on scrolled back where the user left it.
func TestReviewCodeOnlyCarriesCursorOffASniffedBlob(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	var long, edited strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&long, "line %d\n", i)
		fmt.Fprintf(&edited, "line %d\n", i)
	}
	edited.WriteString("tail\n")
	dir := writeGitRepo(t,
		map[string]string{
			"a.txt": long.String(),
			"c.go":  "package a\n\nfunc C() int { return 3 }\n",
		},
		map[string]string{
			"a.txt":    edited.String(),
			"c.go":     "package a\n\nfunc C() int { return 30 }\n",
			"blob.dat": "\x00\x01\x02new",
		})
	openReviewOn(t, m, "sniff", dir)
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "a.txt" {
		t.Fatalf("review should open on a.txt, got %q", got)
	}

	m.pressFilterKey(t)
	m.review.SetCursorDiffLine(150, 1)
	m.drainCmds(t, m.switchDiffFile(1))
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "c.go" {
		t.Fatalf("the file after a.txt = %q, want c.go", got)
	}

	m.pressDiffKey(t, 'J')
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "a.txt" {
		t.Fatalf("the load should carry the cursor off blob.dat, landed on %q", got)
	}
	if m.review.Snapshot().Scroll != 150 {
		t.Fatalf("a.txt scroll = %d, want the 150 it was left at", m.review.Snapshot().Scroll)
	}
}

// space walks to the next file still to review, and a file the filter hides is
// not one the user can review.
func TestReviewCodeOnlySpaceSkipsHiddenFile(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "queue", writeGitRepo(t,
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 1 }\n",
			"b.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00one",
			"c.go":  "package a\n\nfunc C() int { return 3 }\n",
			"d.go":  "package a\n\nfunc D() int { return 4 }\n",
		},
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 10 }\n",
			"b.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00two",
			"c.go":  "package a\n\nfunc C() int { return 30 }\n",
			"d.go":  "package a\n\nfunc D() int { return 40 }\n",
		}))
	paths := make([]string, len(m.review.Snapshot().Set.Files))
	for i := range m.review.Snapshot().Set.Files {
		paths[i] = m.review.Snapshot().Set.Files[i].File.Path
	}
	want := []string{"a.go", "b.png", "c.go", "d.go"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("file order = %v, want %v", paths, want)
	}

	m.pressFilterKey(t)
	m.drainCmds(t, m.switchDiffFile(2))
	m.pressDiffKey(t, ' ')
	if !m.fileReviewed("c.go") {
		t.Fatal("space should mark c.go reviewed")
	}
	m.drainCmds(t, m.switchDiffFile(-3))
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "a.go" {
		t.Fatalf("the cursor should be back on a.go, got %q", got)
	}

	m.pressDiffKey(t, ' ')
	if got := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path; got != "d.go" {
		t.Fatalf("space should walk past the hidden b.png and the reviewed c.go to d.go, landed on %q", got)
	}
}

// Filtering a review whose every change is a blob leaves both panes with
// nothing to show, which has to read as a state rather than as a broken pane,
// and leaves tab with nowhere to go.
func TestReviewCodeOnlyWithNoCodeFiles(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "onlybin", writeGitRepo(t,
		map[string]string{
			"b.dat":    "\x00\x01\x02one",
			"logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00one",
		},
		map[string]string{
			"b.dat":    "\x00\x01\x02two",
			"logo.png": "\x89PNG\r\n\x1a\n\x00\x00\x00\x00two",
		}))

	m.pressFilterKey(t)
	if list := ansi.Strip(m.viewDiffFileList(30, 20)); !strings.Contains(list, "no code files") {
		t.Fatalf("the file list should say why it is empty:\n%s", list)
	}
	code := ansi.Strip(m.viewDiffCode(60, 20))
	if strings.Contains(code, "b.dat") {
		t.Fatalf("the code pane should not render a hidden file:\n%s", code)
	}
	if !strings.Contains(code, "no code files") {
		t.Fatalf("the code pane should say why it is empty:\n%s", code)
	}

	m.pressDiffKey(t, 'J')
	if m.review.Snapshot().FileIndex != 0 {
		t.Fatalf("tab should not walk the selection through hidden files, fileIdx = %d", m.review.Snapshot().FileIndex)
	}

	path := m.review.Snapshot().Set.Files[m.review.Snapshot().FileIndex].File.Path
	m.pressDiffKey(t, ' ')
	if m.fileReviewed(path) {
		t.Fatal("space should not mark a file the filter is hiding")
	}
	m.pressDiffKey(t, 'c')
	if m.review.Snapshot().Annotating {
		t.Fatal("c should not comment a file the filter is hiding")
	}
}

func TestDiffPageKeysMoveAViewport(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, 80)
	openReviewOn(t, m, "pager", dir)
	before := m.review.Snapshot().CursorLine
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.review.Snapshot().CursorLine <= before {
		t.Fatalf("pgdown left cursor at %d", m.review.Snapshot().CursorLine)
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.review.Snapshot().CursorLine != before {
		t.Fatalf("pgup should return to %d, got %d", before, m.review.Snapshot().CursorLine)
	}
}

func TestNarrowReviewKeepsBothPanesMeasurable(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	if m.services.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "narrow", gitTestRepo(t))
	for _, width := range []int{29, 24, 10, 1} {
		m.layout.width = width
		fileWidth, codeWidth := m.diffPaneWidths()
		if fileWidth < 0 || codeWidth < 0 {
			t.Fatalf("width %d gave panes %d and %d", width, fileWidth, codeWidth)
		}
		if fileWidth+codeWidth > width {
			t.Fatalf("width %d gave panes wider than the screen: %d and %d", width, fileWidth, codeWidth)
		}
		if lines := uitheme.SplitLines(preparedView(m)); len(lines) == 0 {
			t.Fatalf("width %d rendered nothing", width)
		}
	}
}
