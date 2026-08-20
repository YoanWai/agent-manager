package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func gitTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiffReviewShowsWholeFile(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")

	m.drainCmds(t, m.openDiff())
	if !m.diff.active || m.mode != modeDiff || m.diff.loading {
		t.Fatalf("diff should be loaded fullscreen, active=%v mode=%v err=%q", m.diff.active, m.mode, m.diff.errText)
	}
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("files = %+v", m.diff.set.Files)
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "review · coder") || !strings.Contains(view, "files") {
		t.Fatalf("fullscreen review layout missing:\n%s", view)
	}
	if !strings.Contains(view, "package main") || !strings.Contains(view, "println(1)") {
		t.Fatalf("whole-file content missing:\n%s", view)
	}
	if !strings.Contains(view, "func main() {}") {
		t.Fatalf("deleted line should interleave:\n%s", view)
	}
}

func TestDiffScopeCycleAndLayout(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())

	m.applyCmd(t, m.cycleDiffScope())
	if m.diff.scope.String() != "vs target" {
		t.Fatalf("scope = %q", m.diff.scope)
	}

	m.diff.sideBySide = true
	if view := ansi.Strip(m.View()); !strings.Contains(view, "split") {
		t.Fatalf("split pill missing:\n%s", view)
	}
}

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
	m.saveAnnotation()
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
	state, err := m.store.ReviewState(sess.ID, m.diff.repoSel)
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
	m.saveAnnotation()
	_, cmd = m.sendAnnotations()
	m.applyCmd(t, cmd)
	notes = m.diff.annotations[m.reviewKey()]
	if len(notes) != 2 || notes[0].round != 1 || notes[1].round != 2 {
		t.Fatalf("review history = %+v, want rounds 1 and 2", notes)
	}
	state, err = m.store.ReviewState(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if state.Round.Number != 2 || len(state.Comments) != 2 {
		t.Fatalf("second persisted review round = %+v", state)
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
	m.saveAnnotation()

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

// Review has to paint exactly the terminal it was given. A frame with more
// rows than the terminal scrolls the top away; a line wider than the
// terminal wraps and pushes every row below it off the bottom, which is how
// the end of a file ends up selected but off screen.
func TestDiffFrameFitsTerminal(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, 400)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	// Narrow widths matter as much as short ones: the footer wraps onto
	// extra lines there, and every row the footer takes is a row the body
	// must give back.
	sizes := []struct{ w, h int }{
		{60, 16}, {60, 20}, {70, 18}, {80, 24}, {90, 22}, {96, 28}, {100, 30},
		{110, 26}, {120, 40}, {132, 34}, {160, 50}, {200, 60}, {240, 70},
	}
	for _, split := range []bool{false, true} {
		for _, annotating := range []bool{false, true} {
			for _, size := range sizes {
				m.width, m.height = size.w, size.h
				m.diff.sideBySide = split
				m.diff.annotating = false
				if annotating {
					m.openAnnotate()
					m.diff.annInput.SetValue("note")
				}

				raw := strings.Split(m.viewDiffFull(), "\n")
				if len(raw) != size.h {
					t.Errorf("split=%v annotating=%v %dx%d: frame paints %d rows",
						split, annotating, size.w, size.h, len(raw))
				}
				for i, line := range raw {
					if got := ansi.StringWidth(line); got > size.w {
						t.Errorf("split=%v annotating=%v %dx%d: line %d is %d wide: %q",
							split, annotating, size.w, size.h, i, got, ansi.Strip(line))
					}
				}
			}
		}
	}
}

// The cursor's line has to be inside the rows review actually paints. A
// viewport taller than its painted area lets the cursor walk past the last
// visible row: the selection is at the end of the file, the screen is not.
func TestDiffCursorStaysOnScreen(t *testing.T) {
	const lines = 400
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	for _, size := range []struct{ w, h int }{{80, 24}, {100, 30}, {120, 40}, {160, 50}} {
		m.width, m.height = size.w, size.h
		m.diff.scroll = 0
		m.diff.cursorLine = 0
		m.moveDiffCursor(lines*2, m.diffCodeHeight())

		fd := m.currentFileDiff()
		if fd == nil {
			t.Fatal("no file diff")
		}
		want := fd.Lines[m.cursorDiffLine()]
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

// gitRepoWithLongFile is a repo whose single change is far taller than any
// terminal, so the review viewport has to scroll to reach the end.
func gitRepoWithLongFile(t *testing.T, lines int) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")

	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line-%03d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "long.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// The end of a file has to be reachable in review. A viewport sized larger
// than the rows the frame actually paints leaves a tail of lines the cursor
// can address but the screen never shows, which is how the last screenful
// silently went missing once before.
func TestDiffReviewReachesLastLine(t *testing.T) {
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
			if m.diff.loading || len(m.diff.set.Files) == 0 {
				t.Fatalf("diff did not load: %q", m.diff.errText)
			}
			m.diff.sideBySide = layout.split

			last := fmt.Sprintf("line-%03d", lines)
			view := ansi.Strip(m.View())
			if strings.Contains(view, last) {
				t.Fatalf("the file's end should start off screen, got:\n%s", view)
			}

			// G jumps to the end; the final line must be painted, not merely
			// selected, and the cursor must sit on it.
			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
			view = ansi.Strip(m.View())
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
	const lines = 120
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, lines)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) == 0 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	for i := 0; i < lines*2; i++ {
		m.handleDiffKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	last := fmt.Sprintf("line-%03d", lines)
	if view := ansi.Strip(m.View()); !strings.Contains(view, last) {
		t.Fatalf("stepping down should reach the last line %q, got:\n%s", last, view)
	}
}

// gitRepoWithWideFile is a repo whose changed lines are far wider than any
// pane, so every line soft-wraps onto several painted rows.
func gitRepoWithWideFile(t *testing.T, lines, lineWidth int) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "wide.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-m", "init")

	var b strings.Builder
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "wide-%03d %s\n", i, strings.Repeat("x", lineWidth))
	}
	if err := os.WriteFile(filepath.Join(dir, "wide.txt"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Long lines wrap onto several painted rows, but the cursor and scroll
// count logical lines. If the window math ignores the wraps, every wrapped
// line on screen pushes one more line of the file's tail off the bottom:
// the cursor lands on the last line and the screen never shows it.
func TestDiffReviewReachesEndWithWrappedLines(t *testing.T) {
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
			if m.diff.loading || len(m.diff.set.Files) == 0 {
				t.Fatalf("diff did not load: %q", m.diff.errText)
			}
			m.width, m.height = 120, 34
			m.diff.sideBySide = layout.split

			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
			last := fmt.Sprintf("wide-%03d", lines)
			if view := ansi.Strip(m.View()); !strings.Contains(view, last) {
				t.Fatalf("G should paint the last line %q, got:\n%s", last, view)
			}

			// Stepping down must keep the cursor painted the whole way.
			m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
			for i := 0; i < lines+5; i++ {
				m.handleDiffKey(tea.KeyMsg{Type: tea.KeyDown})
				fd := m.currentFileDiff()
				lineIdx := m.cursorDiffLine()
				if fd == nil || lineIdx >= len(fd.Lines) {
					t.Fatal("cursor out of range")
				}
				marker := strings.Fields(fd.Lines[lineIdx].Text)[0]
				if view := ansi.Strip(m.View()); !strings.Contains(view, marker) {
					t.Fatalf("step %d: cursor on %q but the frame never paints it:\n%s", i, marker, view)
				}
			}
		})
	}
}

// Ctrl+R inside a session opens review and remembers the session, so leaving
// review returns to it rather than dropping to the list.
func TestInSessionReviewRemembersOriginAndReattaches(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "reviewme", t.TempDir(), "")
	m.selectSessionRow(t, "reviewme")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m)

	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)

	if m.mode != modeDiff {
		t.Fatalf("marker set should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if m.diff.reattachID != sess.ID {
		t.Fatalf("review origin = %q, want %q", m.diff.reattachID, sess.ID)
	}

	// esc leaves review; the live origin session re-attaches.
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("esc should leave review, mode = %v", m.mode)
	}
	if m.diff.reattachID != "" {
		t.Fatalf("reattach origin should be consumed, got %q", m.diff.reattachID)
	}
	if cmd == nil {
		t.Fatal("esc from in-session review should re-attach the session, got nil command")
	}
}

// Review opened from the list has no origin, so esc returns to the list with
// no re-attach.
func TestListReviewLeavesToListWithoutReattach(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "listreview", t.TempDir(), "")
	m.selectSessionRow(t, "listreview")

	m.drainCmds(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, mode = %v, err = %q", m.mode, m.errBar.text)
	}
	if m.diff.reattachID != "" {
		t.Fatalf("list review should not set a reattach origin, got %q", m.diff.reattachID)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeList {
		t.Fatalf("esc should return to list, mode = %v", m.mode)
	}
	if cmd != nil {
		t.Fatal("list review esc should not re-attach")
	}
}

// Leaving review back into a session acknowledges a finished alert, matching
// what entering the session from the list does.
func TestReattachAcknowledgesFinished(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "finisher", t.TempDir(), "")
	m.selectSessionRow(t, "finisher")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no session selected")
	}
	clearRequestOnCleanup(t, m)

	if err := m.store.UpdateStatus(sess.ID, status.Finished); err != nil {
		t.Fatalf("set finished: %v", err)
	}
	if _, err := tmuxCmd("set-option", "-g", "@am_request", tmux.RequestReview).CombinedOutput(); err != nil {
		t.Fatalf("set marker: %v", err)
	}
	updated, _ := m.Update(attachDoneMsg{sessID: sess.ID})
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("expected review, mode = %v, err = %q", m.mode, m.errBar.text)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if cmd == nil {
		t.Fatalf("esc should re-attach, err = %q", m.errBar.text)
	}
	prepared, ok := cmd().(reattachPreparedMsg)
	if !ok {
		t.Fatal("re-attach preparation should run in the returned command")
	}
	if prepared.err != nil {
		t.Fatalf("prepare re-attach: %v", prepared.err)
	}
	got, err := m.store.Get(sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != status.Idle || !got.Acked {
		t.Fatalf("re-attach should acknowledge finished: status = %q acked = %v", got.Status, got.Acked)
	}
}

func TestStaleReattachDoesNotInterruptReopenedReview(t *testing.T) {
	m := &Model{mode: modeDiff, diff: diffState{active: true, gen: 9}}
	updated, cmd := m.Update(reattachPreparedMsg{sessID: "old", diffGen: 8})
	m = updated.(*Model)
	if cmd != nil {
		t.Fatal("stale re-attach should not return an attach command")
	}
	if m.mode != modeDiff || !m.diff.active {
		t.Fatal("stale re-attach should leave the reopened review untouched")
	}
}

func gitRepoWithTwoChangedFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "-b", "main")
	write("a.go", "package a\n\nfunc A() int { return 1 }\n")
	write("b.go", "package a\n\nfunc B() int { return 2 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	write("a.go", "package a\n\nfunc A() int { return 10 }\n")
	write("b.go", "package a\n\nfunc B() int { return 20 }\n")
	return dir
}

// umbrellaWithTwoRepos makes a dir that is not itself a repo but holds two
// nested repos, the second one dirty so it ranks first.
func umbrellaWithTwoRepos(t *testing.T) (umbrella, dirtyName string) {
	t.Helper()
	umbrella = t.TempDir()
	run := func(dir string, args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	for _, name := range []string{"alpha", "bravo"} {
		dir := filepath.Join(umbrella, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		run(dir, "git", "init")
		run(dir, "git", "add", ".")
		run(dir, "git", "commit", "-m", "init")
	}
	dirty := filepath.Join(umbrella, "bravo")
	if err := os.WriteFile(filepath.Join(dirty, "a.go"), []byte("package a\n\nfunc A() int { return 99 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return umbrella, "bravo"
}

// A session whose cwd is an umbrella of several repos opens review on the
// most-active repo, shows the repo in the header, and the r key picks another.
func TestReviewPicksRepoUnderUmbrella(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "umbrella", umbrella)

	if len(m.diff.repoRoots) != 2 {
		t.Fatalf("want 2 repos resolved, got %v (err=%q)", m.diff.repoRoots, m.diff.errText)
	}
	if got := filepath.Base(m.diff.repoSel); got != dirtyName {
		t.Fatalf("want dirty repo %q selected first, got %q", dirtyName, got)
	}
	if !strings.Contains(m.viewDiffHeader("umbrella"), dirtyName) {
		t.Fatalf("header should name the selected repo %q", dirtyName)
	}

	m.pickRepo(t, "alpha")
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("picker should select the other repo, got %q", got)
	}
	if !strings.Contains(m.viewDiffHeader("umbrella"), "alpha") {
		t.Fatal("header should follow the picked repo")
	}
	m.pickRepo(t, dirtyName)
	if got := filepath.Base(m.diff.repoSel); got != dirtyName {
		t.Fatalf("picker should select back, got %q", got)
	}
}

func TestRepoPickerFiltersAndSelects(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "picker", umbrella)
	if filepath.Base(m.diff.repoSel) != dirtyName {
		t.Fatalf("expected to start on %q", dirtyName)
	}

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range "alph" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := m.filteredRows(); len(got) != 1 || got[0].label != "alpha" {
		t.Fatalf("filter should narrow to alpha, got %v", got)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("enter should select alpha, got %q", got)
	}
}

func TestRepoPickerEscapeKeepsRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "escpick", umbrella)
	before := m.diff.repoSel

	m.pressDiffKey(t, 'r')
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	if m.mode != modeDiff {
		t.Fatalf("esc should return to review, mode = %v", m.mode)
	}
	if m.diff.repoSel != before || filepath.Base(m.diff.repoSel) != dirtyName {
		t.Fatalf("esc should not change the repo, got %q", m.diff.repoSel)
	}
}

func TestBranchPickerListsWorktreesAndSwitches(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	alpha := filepath.Join(umbrella, "alpha")
	outside := filepath.Join(t.TempDir(), "wt-feature")
	cmd := exec.Command("git", "worktree", "add", "-b", "feature/pick-me", outside)
	cmd.Dir = alpha
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v: %s", err, out)
	}
	openReviewOn(t, m, "branches", umbrella)
	m.drainCmds(t, m.selectRepo(alpha))

	m.pressDiffKey(t, 'b')
	if m.mode != modeRepoPick {
		t.Fatalf("b should open the branch picker, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	rendered := m.viewRepoPick()
	if !strings.Contains(rendered, "feature/pick-me") {
		t.Fatalf("picker should show the worktree branch, got:\n%s", rendered)
	}
	for _, r := range "pick-me" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	updated, cmdSel := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmdSel)
	resolved, _ := filepath.EvalSymlinks(outside)
	sel, _ := filepath.EvalSymlinks(m.diff.repoSel)
	if sel != resolved {
		t.Fatalf("enter should switch to the worktree, got %q", m.diff.repoSel)
	}
}

// The b picker must seed its cursor on the worktree under review even when
// that worktree was declared via a /tmp path that git resolves to
// /private/tmp, since /tmp is a symlink on macOS.
func TestBranchPickerSeedsCursorForSymlinkedWorktree(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	alpha := filepath.Join(umbrella, "alpha")

	linkedParent, err := os.MkdirTemp("/tmp", "am-p2-symlink-seed-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(linkedParent) })
	if resolved, _ := filepath.EvalSymlinks(linkedParent); resolved == linkedParent {
		t.Skip("/tmp does not resolve to a different path on this system")
	}
	rawWorktree := filepath.Join(linkedParent, "wt-declared")

	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit(alpha, "worktree", "add", "-b", "feature/declared-symlinked", rawWorktree)

	createSession(t, m, "symseed", umbrella, "")
	m.selectSessionRow(t, "symseed")
	sess, _ := m.selected()
	if err := m.store.SetReviewRepo(sess.ID, rawWorktree); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if m.errBar.text != "" {
		t.Fatalf("declared worktree must not be reported missing, err = %q", m.errBar.text)
	}
	if m.diff.repoSel != rawWorktree {
		t.Fatalf("repoSel should stay the raw declared path, got %q", m.diff.repoSel)
	}

	m.pressDiffKey(t, 'b')
	if m.mode != modeRepoPick {
		t.Fatalf("b should open the branch picker, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	resolvedWorktree, _ := filepath.EvalSymlinks(rawWorktree)
	rows := m.filteredRows()
	wantCursor := -1
	for i, row := range rows {
		if resolved, _ := filepath.EvalSymlinks(row.root); resolved == resolvedWorktree {
			wantCursor = i
			break
		}
	}
	if wantCursor == -1 {
		t.Fatalf("declared worktree should appear among picker rows, got %v", rows)
	}
	if wantCursor == 0 {
		t.Fatal("test setup invalid: declared worktree must not already be row 0")
	}
	if m.repoPick.cursor != wantCursor {
		t.Fatalf("cursor should seed on the declared worktree row %d, got %d", wantCursor, m.repoPick.cursor)
	}
}

// A reviewed mark placed on a path in one repo must not bleed onto a
// same-named path in a sibling repo when cycling with r.
func TestReviewMarksIsolatedPerRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	if m.gitDrv == nil {
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

// drainCmds runs a command chain to exhaustion, feeding every message back
// into Update, so async follow-ups (diff loads, highlights) all land.
func (m *Model) drainCmds(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	for i := 0; cmd != nil && i < 20; i++ {
		msg := cmd()
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, child := range batch {
				m.drainCmds(t, child)
			}
			return
		}
		if _, ok := msg.(startupTickMsg); ok {
			updated, _ := m.Update(msg)
			*m = *updated.(*Model)
			return
		}
		updated, next := m.Update(msg)
		*m = *updated.(*Model)
		cmd = next
	}
}

func (m *Model) pressDiffKey(t *testing.T, key rune) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
}

func (m *Model) pressFilterKey(t *testing.T) {
	t.Helper()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
}

// pickRepo drives the repo picker the way a human would: r, type the repo
// name, enter.
func (m *Model) pickRepo(t *testing.T, name string) {
	t.Helper()
	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range name {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
}

func openReviewOn(t *testing.T, m *Model, name, dir string) {
	t.Helper()
	createSession(t, m, name, dir, "")
	m.selectSessionRow(t, name)
	m.drainCmds(t, m.openDiff())
	if m.mode != modeDiff {
		t.Fatalf("openDiff should enter review, err = %q", m.errBar.text)
	}
}

func TestReviewLoadsFilesOnDemand(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "lazy", gitRepoWithTwoChangedFiles(t))
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("want 2 files, got %d", len(m.diff.set.Files))
	}
	if !m.diff.set.Files[0].Loaded() {
		t.Fatal("selected file should be loaded after its background command lands")
	}
	if m.diff.set.Files[1].Loaded() {
		t.Fatal("unselected file should remain unloaded")
	}

	cmd := m.switchDiffFile(1)
	if cmd == nil {
		t.Fatal("switching to an unloaded file should schedule a load")
	}
	if m.currentFileDiff().Loaded() {
		t.Fatal("file loading should not block the navigation handler")
	}
	if body := ansi.Strip(m.viewDiffCode(80, 20)); !strings.Contains(body, "loading file") {
		t.Fatalf("unloaded file should render a loading state, got %q", body)
	}
	m.drainCmds(t, cmd)
	if !m.currentFileDiff().Loaded() {
		t.Fatal("file should install after its background command lands")
	}
}

func TestReviewCloseReleasesStateAndIgnoresLateLoad(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	if m.mode != modeList || m.diff.active || m.diff.sessID != "" {
		t.Fatalf("review did not close cleanly: mode=%v active=%v session=%q",
			m.mode, m.diff.active, m.diff.sessID)
	}
	if len(m.diff.set.Files) != 0 || m.diff.hlPending != (hlKey{}) {
		t.Fatal("close should release diff and pending highlight state immediately")
	}

	late := load()
	updated, next := m.Update(late)
	*m = *updated.(*Model)
	if next != nil || len(m.diff.set.Files) != 0 || m.diff.active {
		t.Fatal("a file load landing after close must be ignored")
	}
}

func TestRefreshFileLoadsRunSerially(t *testing.T) {
	var active atomic.Int32
	var peak atomic.Int32
	cmds := make([]tea.Cmd, 8)
	for i := range cmds {
		index := i
		cmds[i] = func() tea.Msg {
			now := active.Add(1)
			for {
				seen := peak.Load()
				if now <= seen || peak.CompareAndSwap(seen, now) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			return diffFileLoadedMsg{index: index}
		}
	}

	msgs, ok := diffFilesLoadCmd(cmds)().(diffFilesLoadedMsg)
	if !ok || len(msgs) != len(cmds) {
		t.Fatalf("serial load returned %T with %d results", msgs, len(msgs))
	}
	if got := peak.Load(); got != 1 {
		t.Fatalf("refresh loads peaked at %d concurrent jobs, want 1", got)
	}
}

// Marking a file reviewed advances to the next unreviewed file; the advanced
// file must still get its syntax highlighting.
func TestSpaceAdvanceKeepsHighlight(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "hl", gitRepoWithTwoChangedFiles(t))
	if len(m.diff.set.Files) != 2 {
		t.Fatalf("want 2 files, got %d (err=%q)", len(m.diff.set.Files), m.diff.errText)
	}
	if m.currentHL() == nil {
		t.Fatal("first file should be highlighted after open")
	}
	m.pressDiffKey(t, ' ')
	if m.diff.fileIdx != 1 {
		t.Fatalf("space should advance to the next file, idx = %d", m.diff.fileIdx)
	}
	if m.currentHL() == nil {
		t.Error("advanced file lost its highlight: switch command was dropped")
	}
}

// Scroll positions are per session and per scope; a second session touching
// the same path must open the file at the top.
func TestScrollDoesNotLeakAcrossSessions(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "one", dir)
	firstFile := m.diff.set.Files[0].File.Path
	m.diff.scroll = 2
	m.drainCmds(t, m.switchDiffFile(1)) // persists scroll for file one

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	openReviewOn(t, m, "two", dir)
	m.drainCmds(t, m.switchDiffFile(1))
	m.drainCmds(t, m.switchDiffFile(1)) // wraps back to the first file
	if fd := m.currentFileDiff(); fd == nil || fd.File.Path != firstFile {
		t.Fatalf("expected to land back on %q", firstFile)
	}
	if m.diff.scroll != 0 {
		t.Errorf("session two inherited session one's scroll: %d", m.diff.scroll)
	}
}

// While a comment is being written or confirmed, background reloads pause,
// and an in-flight reload result is dropped instead of shifting lines under
// the open editor.
func TestNoReloadWhileAnnotating(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "ann", gitRepoWithTwoChangedFiles(t))
	linesBefore := len(m.currentFileDiff().Lines)

	m.openAnnotate()
	if !m.diff.annotating {
		t.Fatal("openAnnotate should enter annotating mode")
	}
	for i := 0; i < 4; i++ {
		if cmd := m.diffRefreshCmd(); cmd != nil {
			t.Fatal("probe must pause while annotating")
		}
	}
	// An in-flight reload from before the comment box opened is dropped.
	stale := diffLoadedMsg{sessID: m.diff.sessID, scope: m.diff.scope, gen: m.diff.gen}
	if cmd := m.handleDiffLoaded(stale); cmd != nil {
		t.Fatal("stale reload should be dropped without follow-up")
	}
	if got := len(m.currentFileDiff().Lines); got != linesBefore {
		t.Errorf("reload replaced the diff under the comment box: %d -> %d lines", linesBefore, got)
	}

	m.diff.annotating = false
	m.diff.sendConfirm = true
	if cmd := m.diffRefreshCmd(); cmd != nil {
		t.Fatal("probe must pause while confirming a send")
	}
}

// refreshDiff drives the silent same-scope reload path (the probe piggyback),
// the only reload that re-anchors comments.
func (m *Model) refreshDiff(t *testing.T) {
	t.Helper()
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, true))
}

// A silent same-scope reload that shifts line numbers re-points saved comments
// at the line carrying their excerpt, so the agent gets the location meant.
func TestAnnotationsReanchorAfterRefresh(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "anchor", dir)
	m.pressDiffKey(t, 'n') // jump to the changed line (return 10)
	m.openAnnotate()
	m.diff.annInput.SetValue("note")
	m.saveAnnotation()
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
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "rounds", dir)
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("verify this return value")
	m.saveAnnotation()
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
	m.discardOrToggleAnnotation()
	if !m.diff.annotations[m.reviewKey()][0].handled {
		t.Fatal("d should mark a sent comment handled")
	}
	state, err := m.store.ReviewState(m.diff.sessID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Comments) != 1 || !state.Comments[0].Resolved || !state.Comments[0].Outdated {
		t.Fatalf("persisted handled comment = %+v", state.Comments)
	}
}

func TestAgentHandledUpdateReloadsWithoutDroppingTheComment(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "handled", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("fix this")
	m.saveAnnotation()
	_, cmd := m.sendAnnotations()
	m.applyCmd(t, cmd)
	note := m.diff.annotations[m.reviewKey()][0]
	if found, err := m.store.SetReviewCommentHandled(m.diff.sessID, note.id, true); err != nil || !found {
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

func TestSavedReviewRoundsGainStableIDsAndPointNumbers(t *testing.T) {
	m := buildModel(t)
	m.diff.sessID = "abc123"
	m.diff.repoSel = "/repo"
	if err := m.store.SetReviewState(m.diff.sessID, m.diff.repoSel, store.ReviewState{
		Comments: []store.ReviewComment{
			{File: "a.go", Line: 2, Text: "first", Round: 3},
			{File: "b.go", Line: 4, Text: "second", Round: 3},
		},
		Round: store.ReviewRound{Number: 3},
	}); err != nil {
		t.Fatal(err)
	}
	state, err := readReviewState(m.store, m.diff.sessID, m.diff.repoSel)
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
	state, err = m.store.ReviewState(m.diff.sessID, m.diff.repoSel)
	if err != nil || state.Comments[0].ID == "" || state.Comments[1].Point != 2 {
		t.Fatalf("persisted migration = %+v, %v", state.Comments, err)
	}
}

// A scope cycle loads a different file set; it must not re-anchor a comment's
// stored line against content it was never made against.
func TestScopeCycleDoesNotReanchor(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "scoped", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("note")
	m.saveAnnotation()
	before := m.diff.annotations[m.reviewKey()][0].line

	m.drainCmds(t, m.cycleDiffScope())
	if got := m.diff.annotations[m.reviewKey()][0].line; got != before {
		t.Fatalf("scope cycle rewrote the comment line: %d -> %d", before, got)
	}
}

// An ambiguous excerpt (blank line, or several identical lines) never moves the
// comment, and re-anchoring never stacks two comments onto one line.
func TestReanchorKeepsAmbiguousAndAvoidsCollapse(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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

// Ctrl+C quits from the comment editor and the send-confirm prompt, not just
// the base review keymap.
func TestReviewCtrlCQuitsFromSubmodes(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "subquit", gitRepoWithTwoChangedFiles(t))
	m.openAnnotate()
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while annotating")
	}
	m.diff.annotating = false
	m.diff.sendConfirm = true
	if _, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit from the send-confirm prompt")
	}
	m.diff.sendConfirm = false
	m.diff.repoRoots = []string{"/tmp/one", "/tmp/two"}
	m.openRepoPick()
	if _, cmd := m.handleRepoPickKey(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit while the repo picker is open")
	}
}

// A load in flight when the comment box opens (e.g. a scope cycle) must not
// swap the set under the editor, even though m.diff.loading is still true.
func TestInFlightLoadDroppedWhileAnnotating(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "inflight", gitRepoWithTwoChangedFiles(t))
	linesBefore := len(m.currentFileDiff().Lines)
	m.openAnnotate()
	m.diff.loading = true // simulate a user-initiated load still running
	stale := diffLoadedMsg{sessID: m.diff.sessID, scope: m.diff.scope, gen: m.diff.gen}
	if cmd := m.handleDiffLoaded(stale); cmd != nil {
		t.Fatal("load must be dropped while annotating")
	}
	if m.diff.loading {
		t.Fatal("in-flight flag must clear so probes resume")
	}
	if got := len(m.currentFileDiff().Lines); got != linesBefore {
		t.Errorf("set swapped under the comment box: %d -> %d", linesBefore, got)
	}
}

// Ctrl+C quits from review mode like it does from the list.
func TestReviewCtrlCQuits(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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

func TestBinaryFileShowsBinaryNotZeroCounts(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "binary", dir)
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "logo.png" {
			m.diff.fileIdx = i
			m.drainCmds(t, m.loadCurrentDiffFile())
			break
		}
	}

	rendered := m.viewDiffFileList(60, 20)
	row := ""
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "logo.png") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("logo.png missing from the file list:\n%s", rendered)
	}
	if !strings.Contains(row, "binary") {
		t.Errorf("logo.png row should be labelled binary, got: %q", row)
	}
	if strings.Contains(row, "+0") || strings.Contains(row, "−0") {
		t.Errorf("logo.png row still shows zero counts: %q", row)
	}
}

// Rows past the eager-load cap are rendered before their content is read, so
// the binary label has to come from numstat rather than the loaded file.
func TestTrackedBinaryPastEagerCapShowsBinary(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "-b", "main")
	const filler = 250
	for i := 0; i < filler; i++ {
		write(fmt.Sprintf("f%03d.txt", i), "one\n")
	}
	write("zz.bin", "\x00\x01\x02initial")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	for i := 0; i < filler; i++ {
		write(fmt.Sprintf("f%03d.txt", i), "two\n")
	}
	write("zz.bin", "\x00\x01\x02changed")
	openReviewOn(t, m, "bigbin", dir)

	files := m.diff.set.Files
	index := -1
	for i := range files {
		if files[i].File.Path == "zz.bin" {
			index = i
		}
	}
	if index < 0 {
		t.Fatal("zz.bin missing from the diff set")
	}
	if files[index].Lines != nil || files[index].Binary {
		t.Fatalf("zz.bin at index %d was loaded; the test needs an unloaded row", index)
	}

	rendered := m.viewDiffFileList(60, len(files)+2)
	row := ""
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "zz.bin") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("zz.bin missing from the file list:\n%s", rendered)
	}
	if !strings.Contains(row, "binary") {
		t.Errorf("zz.bin row should be labelled binary, got: %q", row)
	}
	if strings.Contains(row, "+0") || strings.Contains(row, "−0") {
		t.Errorf("zz.bin row still shows zero counts: %q", row)
	}
}

// writeGitRepo commits a first version of every file, then lays down the
// second one, leaving a working tree whose changes a review can open.
func writeGitRepo(t *testing.T, committed, working map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	run("git", "init", "-b", "main")
	for name, content := range committed {
		writeRepoFile(t, dir, name, content)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	for name, content := range working {
		writeRepoFile(t, dir, name, content)
	}
	return dir
}

func writeRepoFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitRepoWithBinaryBetweenTextFiles changes a tracked binary file sitting
// between two text files. Its name says nothing, so git's numstat verdict is
// the only thing that can call it binary.
func gitRepoWithBinaryBetweenTextFiles(t *testing.T) string {
	t.Helper()
	return writeGitRepo(t,
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 1 }\n",
			"b.dat": "\x00\x01\x02one",
			"c.go":  "package a\n\nfunc C() int { return 3 }\n",
		},
		map[string]string{
			"a.go":  "package a\n\nfunc A() int { return 10 }\n",
			"b.dat": "\x00\x01\x02two",
			"c.go":  "package a\n\nfunc C() int { return 30 }\n",
		})
}

// gitRepoWithLockFileBetweenTextFiles carries a lock file git counts in full,
// so a header reading that follows the list can be told apart from one that
// followed every changed file.
func gitRepoWithLockFileBetweenTextFiles(t *testing.T) string {
	t.Helper()
	lock := func(version string) string {
		return "one " + version + "\ntwo " + version + "\nthree " + version + "\n"
	}
	return writeGitRepo(t,
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 1 }\n",
			"b.dat":  "\x00\x01\x02one",
			"c.go":   "package a\n\nfunc C() int { return 3 }\n",
			"go.sum": lock("v1"),
		},
		map[string]string{
			"a.go":   "package a\n\nfunc A() int { return 10 }\n",
			"b.dat":  "\x00\x01\x02two",
			"c.go":   "package a\n\nfunc C() int { return 30 }\n",
			"go.sum": lock("v2"),
		})
}

// A compiled artifact is dropped on its name alone, which is what a file git
// has not classified yet needs: an untracked one carries no numstat verdict,
// and nothing sniffs its bytes until the cursor reaches it.
func TestNonCodePathNamesCompiledArtifacts(t *testing.T) {
	hidden := []string{
		"build/Main.class", "app/__pycache__/mod.pyc", "assets/logo.PNG",
		"go.sum", "Cargo.lock", "web/package-lock.json", "vendor/lib.so",
	}
	for _, path := range hidden {
		if !nonCodePath(path) {
			t.Errorf("%q should be filtered out of a code-only review", path)
		}
	}
	shown := []string{"main.go", "Main.java", "mod.py", "readme.md", "classy.go"}
	for _, path := range shown {
		if nonCodePath(path) {
			t.Errorf("%q is source and should stay in the review", path)
		}
	}
}

// f drops the files git calls binary out of the review, and neither the
// selection nor a file switch is left on one of them.
func TestReviewCodeOnlyHidesBinaryFiles(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "filter", gitRepoWithBinaryBetweenTextFiles(t))

	binary := -1
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "b.dat" {
			binary = i
		}
	}
	if binary < 0 {
		t.Fatalf("b.dat missing from the diff set: %+v", m.diff.set.Files)
	}
	if !m.diff.set.Files[binary].Stat.Binary {
		t.Fatal("numstat should mark b.dat binary before its content is read")
	}
	m.diff.fileIdx = binary

	m.pressFilterKey(t)
	if !m.diff.codeOnly {
		t.Fatal("f should turn the code-only filter on")
	}
	if m.diff.fileIdx == binary {
		t.Fatal("the selection should leave a file the filter hides")
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "b.dat") {
		t.Fatalf("b.dat should be filtered out of the file list:\n%s", list)
	}
	if !strings.Contains(list, "a.go") || !strings.Contains(list, "c.go") {
		t.Fatalf("the code files should stay listed:\n%s", list)
	}

	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "a.go" {
			m.diff.fileIdx = i
		}
	}
	m.drainCmds(t, m.switchDiffFile(1))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "c.go" {
		t.Fatalf("the file after a.go = %q, want c.go", got)
	}
	m.drainCmds(t, m.switchDiffFile(-1))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.go" {
		t.Fatalf("the file before c.go = %q, want a.go", got)
	}

	m.pressFilterKey(t)
	if m.diff.codeOnly {
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
	m := buildModel(t)
	if m.gitDrv == nil {
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
		t.Fatalf("z.png missing from the diff set: %+v", m.diff.set.Files)
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
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	m.diff.gen++
	updated, cmd := m.Update(m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, true)())
	*m = *updated.(*Model)
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "src.go" {
		t.Fatalf("the reload should settle the selection itself, landed on %q", got)
	}
	m.drainCmds(t, cmd)
	if list := ansi.Strip(m.viewDiffFileList(60, 20)); strings.Contains(list, "z.png") {
		t.Fatalf("the filter should survive a silent reload:\n%s", list)
	}
}

// A blob whose name gives nothing away is only outed when its load sniffs a
// NUL, so the cursor has to be carried off it the way every other file switch
// moves: with the file it lands on scrolled back where the user left it.
func TestReviewCodeOnlyCarriesCursorOffASniffedBlob(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.txt" {
		t.Fatalf("review should open on a.txt, got %q", got)
	}

	m.pressFilterKey(t)
	m.diff.scroll = 150
	m.diff.cursorLine = 150
	m.drainCmds(t, m.switchDiffFile(1))
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "c.go" {
		t.Fatalf("the file after a.txt = %q, want c.go", got)
	}

	m.pressDiffKey(t, 'J')
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.txt" {
		t.Fatalf("the load should carry the cursor off blob.dat, landed on %q", got)
	}
	if m.diff.scroll != 150 {
		t.Fatalf("a.txt scroll = %d, want the 150 it was left at", m.diff.scroll)
	}
}

// space walks to the next file still to review, and a file the filter hides is
// not one the user can review.
func TestReviewCodeOnlySpaceSkipsHiddenFile(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	paths := make([]string, len(m.diff.set.Files))
	for i := range m.diff.set.Files {
		paths[i] = m.diff.set.Files[i].File.Path
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
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "a.go" {
		t.Fatalf("the cursor should be back on a.go, got %q", got)
	}

	m.pressDiffKey(t, ' ')
	if got := m.diff.set.Files[m.diff.fileIdx].File.Path; got != "d.go" {
		t.Fatalf("space should walk past the hidden b.png and the reviewed c.go to d.go, landed on %q", got)
	}
}

// Filtering a review whose every change is a blob leaves both panes with
// nothing to show, which has to read as a state rather than as a broken pane,
// and leaves tab with nowhere to go.
func TestReviewCodeOnlyWithNoCodeFiles(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
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
	if m.diff.fileIdx != 0 {
		t.Fatalf("tab should not walk the selection through hidden files, fileIdx = %d", m.diff.fileIdx)
	}

	path := m.diff.set.Files[m.diff.fileIdx].File.Path
	m.pressDiffKey(t, ' ')
	if m.fileReviewed(path) {
		t.Fatal("space should not mark a file the filter is hiding")
	}
	m.pressDiffKey(t, 'c')
	if m.diff.annotating {
		t.Fatal("c should not comment a file the filter is hiding")
	}
}

// A file whose line count is unknown must not be silently summed as zero in
// the header: the totals carry a marker instead of asserting an exact count.
func TestHeaderMarksUncountedFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads any file regardless of mode")
	}
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "counted", dir)
	if strings.Contains(m.viewDiffHeader("counted"), "?") {
		t.Fatal("header should not flag unknown counts when every file is counted")
	}

	locked := filepath.Join(dir, "locked.go")
	if err := os.WriteFile(locked, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	set, err := diff.BuildSet(m.gitDrv, dir, git.ScopeUncommitted, "")
	if err != nil {
		t.Fatal(err)
	}
	m.diff.set = set
	if !strings.Contains(m.viewDiffHeader("counted"), "+?") {
		t.Fatalf("header should mark the uncounted file, got %q", m.viewDiffHeader("counted"))
	}
}

func TestReviewOpensOnDeclaredRepo(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, dirtyName := umbrellaWithTwoRepos(t)
	createSession(t, m, "declared", umbrella, "")
	m.selectSessionRow(t, "declared")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q (ranking prefers %q)", got, dirtyName)
	}
}

// A repo picked by hand outranks the agent's declaration, and keeps doing so
// after review is closed and reopened.
func TestHandPickedRepoOutlivesReopen(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	createSession(t, m, "picked", umbrella, "")
	m.selectSessionRow(t, "picked")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("review should open on the declared repo, got %q", got)
	}

	m.pickRepo(t, "bravo")
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeList {
		t.Fatalf("esc should leave review, mode = %v", m.mode)
	}
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("the hand-picked repo should win over the declared one on reopen, got %q", got)
	}
}

// A hand-picked repo that disappears must be reported and forgotten, so the
// agent's declaration takes over instead of a dead path shadowing it forever.
func TestVanishedHandPickedRepoIsReportedAndForgotten(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	addDirtyRepo(t, umbrella, "charlie")
	createSession(t, m, "vanish", umbrella, "")
	m.selectSessionRow(t, "vanish")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(umbrella, "alpha")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	m.pickRepo(t, "bravo")
	if got := filepath.Base(m.diff.repoSel); got != "bravo" {
		t.Fatalf("picking bravo should load it, got %q", got)
	}

	if err := os.RemoveAll(filepath.Join(umbrella, "bravo")); err != nil {
		t.Fatal(err)
	}
	m.errBar.text = ""
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))

	if !strings.Contains(m.errBar.text, "bravo") {
		t.Fatalf("a vanished hand-picked repo must be surfaced, got err %q", m.errBar.text)
	}
	if !strings.Contains(m.viewDiffStatus(), m.errBar.text) {
		t.Fatalf("review status should show %q", m.errBar.text)
	}
	if _, still := m.pickedRepos[sess.ID]; still {
		t.Fatal("the dead pick must be forgotten so the declaration can take over")
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	m.drainCmds(t, m.openDiff())
	if got := filepath.Base(m.diff.repoSel); got != "alpha" {
		t.Fatalf("reopening should land on the declared repo, got %q", got)
	}
}

func TestDeclaredWorktreeOutsideCwdIsAccepted(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	outside := filepath.Join(t.TempDir(), "wt-out")
	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit(filepath.Join(umbrella, "alpha"), "worktree", "add", "-b", "feature/wt", outside)

	createSession(t, m, "wtdecl", umbrella, "")
	m.selectSessionRow(t, "wtdecl")
	sess, _ := m.selected()
	if err := m.store.SetReviewRepo(sess.ID, outside); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())
	if m.errBar.text != "" {
		t.Fatalf("declared worktree must not be reported missing, err = %q", m.errBar.text)
	}
	resolved, _ := filepath.EvalSymlinks(outside)
	sel, _ := filepath.EvalSymlinks(m.diff.repoSel)
	if sel != resolved {
		t.Fatalf("review should open on the declared worktree, got %q", m.diff.repoSel)
	}
	found := false
	for _, root := range m.diff.repoRoots {
		if r, _ := filepath.EvalSymlinks(root); r == resolved {
			found = true
		}
	}
	if !found {
		t.Fatal("the declared worktree should appear in the picker roots")
	}
}

// addDirtyRepo adds a committed repo with an uncommitted edit, so it ranks
// ahead of the clean ones.
func addDirtyRepo(t *testing.T, umbrella, name string) {
	t.Helper()
	dir := filepath.Join(umbrella, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "init", "-b", "main")
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 77 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A declared repo the session cwd does not contain must be reported, not
// silently swapped for whatever the ranking put on top.
func TestDeclaredRepoOutsideCwdIsReported(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	createSession(t, m, "elsewhere", umbrella, "")
	m.selectSessionRow(t, "elsewhere")
	sess, ok := m.selected()
	if !ok {
		t.Fatal("no selected session")
	}
	if err := m.store.SetReviewRepo(sess.ID, filepath.Join(t.TempDir(), "somewhere-else")); err != nil {
		t.Fatal(err)
	}
	m.drainCmds(t, m.openDiff())

	if m.errBar.text == "" {
		t.Fatal("a declared repo outside the session cwd must be surfaced")
	}
	if !strings.Contains(m.viewDiffStatus(), m.errBar.text) {
		t.Fatalf("review status should show %q", m.errBar.text)
	}
	if len(m.diff.repoRoots) < 2 {
		t.Fatal("the picker must stay usable so the user can recover")
	}
}

// Picking a repo after the session has left m.sessions must say so instead of
// dropping the user back into review with the old repo and no explanation.
func TestRepoPickerReportsMissingSession(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "gone", umbrella)
	before := m.diff.repoSel

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for _, r := range "alph" {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	m.sessions = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	if cmd != nil {
		t.Fatal("a missing session must not kick off a diff load")
	}
	if m.errBar.text == "" {
		t.Fatal("picking a repo for a missing session must surface an error")
	}
	if m.diff.repoSel != before {
		t.Fatalf("repo should not change when the session is gone, got %q", m.diff.repoSel)
	}
	if !strings.Contains(m.viewDiffStatus(), m.errBar.text) {
		t.Fatalf("review status should show the error %q", m.errBar.text)
	}
}

// The poller reloads repoRoots while the picker is open, so the live list can
// shrink and, because rankRepos is dirty-first, reorder under a parked cursor.
// The picker works off a snapshot, so Enter must load the repo whose row was on
// screen and must never index past the list.
func TestRepoPickerSurvivesShrinkingRootList(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithTwoRepos(t)
	openReviewOn(t, m, "shrink", umbrella)

	realRoots := append([]string(nil), m.diff.repoRoots...)
	if len(realRoots) != 2 {
		t.Fatalf("want 2 real repos, got %v", realRoots)
	}
	for i := len(realRoots); i < 20; i++ {
		m.diff.repoRoots = append(m.diff.repoRoots, filepath.Join(umbrella, fmt.Sprintf("repo-%02d", i)))
	}

	m.pressDiffKey(t, 'r')
	if m.mode != modeRepoPick {
		t.Fatalf("r should open the repo picker, mode = %v", m.mode)
	}
	for m.repoPick.cursor != 1 {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
		*m = *updated.(*Model)
	}
	onScreen := m.filteredRows()[m.repoPick.cursor].root

	// A reload lands carrying only the repos that still exist, re-ranked.
	m.diff.repoRoots = []string{realRoots[1], realRoots[0]}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)

	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if m.repoPick.cursor >= len(m.repoPick.rows) {
		t.Fatalf("cursor should stay inside the snapshot, got %d", m.repoPick.cursor)
	}
	if m.diff.repoSel != onScreen {
		t.Fatalf("enter should load the repo on the cursor row %q, got %q", onScreen, m.diff.repoSel)
	}
}

func TestRepoPickerFitsTerminalHeight(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 80, 24
	for i := 0; i < 20; i++ {
		m.diff.repoRoots = append(m.diff.repoRoots,
			fmt.Sprintf("/home/someone/very/long/parent/path/for/wrapping/umbrella/repo-%02d", i))
	}
	m.diff.repoSel = m.diff.repoRoots[0]
	m.openRepoPick()

	view := m.viewRepoPick()
	if lines := len(strings.Split(view, "\n")); lines > m.height {
		t.Fatalf("picker rendered %d lines, terminal is %d", lines, m.height)
	}
	if !strings.Contains(view, "repo-00") {
		t.Fatal("the cursor row should be visible at the top of the list")
	}
	shown := strings.Count(view, "repo-")
	if shown == 0 || shown >= len(m.diff.repoRoots) {
		t.Fatalf("expected a windowed subset of the repos, %d of %d rendered", shown, len(m.diff.repoRoots))
	}
	if want := fmt.Sprintf("+%d more", len(m.diff.repoRoots)-shown); !strings.Contains(view, want) {
		t.Fatalf("hidden count should match the %d rows actually rendered, want %q in view", shown, want)
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	*m = *updated.(*Model)
	if m.repoPick.cursor != len(m.diff.repoRoots)-1 {
		t.Fatalf("up from the top should wrap to the last repo, cursor = %d", m.repoPick.cursor)
	}
	view = m.viewRepoPick()
	if lines := len(strings.Split(view, "\n")); lines > m.height {
		t.Fatalf("picker rendered %d lines at the list end, terminal is %d", lines, m.height)
	}
	if !strings.Contains(view, "repo-19") {
		t.Fatal("the cursor must stay visible after moving to the end of the list")
	}
}

func TestCtrlRFromListOpensReview(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	createSession(t, m, "ctrlr", gitRepoWithTwoChangedFiles(t), "")
	m.selectSessionRow(t, "ctrlr")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
	if m.mode != modeDiff {
		t.Fatalf("ctrl+r from the list should open review, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	if m.diff.reattachID != "" {
		t.Fatal("review opened from the list should return to the list, not re-attach")
	}
}

func gitRepoWithSecondBranch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "-b", "main")
	write("a.go", "package a\n\nfunc A() int { return 1 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c1")
	run("git", "branch", "feature")
	write("a.go", "package a\n\nfunc A() int { return 2 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c2")
	return dir
}

func (m *Model) typeAndEnter(t *testing.T, text string) {
	t.Helper()
	for _, r := range text {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		*m = *updated.(*Model)
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	*m = *updated.(*Model)
	m.drainCmds(t, cmd)
}

// The B picker lists auto plus the repo's refs; picking a ref persists it per
// repo and forces the branch scope, and auto clears the stored base.
func TestBasePickerPersistsSwitchesScopeAndClears(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "base", dir)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}
	if m.diff.scope == git.ScopeBranch {
		t.Fatal("precondition: scope should start off vs target so the switch is observable")
	}

	m.pressDiffKey(t, 'B')
	if m.mode != modeRepoPick {
		t.Fatalf("B should open the base picker, mode = %v", m.mode)
	}
	labels := map[string]bool{}
	for _, row := range m.repoPick.rows {
		labels[row.label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and feature, got %v", labels)
	}

	m.typeAndEnter(t, "feature")
	if m.mode != modeDiff {
		t.Fatalf("enter should return to review, mode = %v", m.mode)
	}
	if m.diff.scope != git.ScopeBranch {
		t.Errorf("picking a base should switch scope to vs target, got %v", m.diff.scope)
	}
	got, err := m.store.ReviewBase(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if got != "feature" {
		t.Errorf("stored base = %q, want feature", got)
	}
	// Genuine per-repo round trip: a base stored for a second repo must read
	// back independently, and repo A's base must stay put.
	repoB := gitRepoWithSecondBranch(t)
	if err := m.store.SetReviewBase(sess.ID, repoB, "main"); err != nil {
		t.Fatal(err)
	}
	baseA, err := m.store.ReviewBase(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if baseA != "feature" {
		t.Errorf("repo A base = %q, want feature", baseA)
	}
	baseB, err := m.store.ReviewBase(sess.ID, repoB)
	if err != nil {
		t.Fatal(err)
	}
	if baseB != "main" {
		t.Errorf("repo B base = %q, want main", baseB)
	}

	m.pressDiffKey(t, 'B')
	if m.mode != modeRepoPick {
		t.Fatalf("B should reopen the base picker, mode = %v", m.mode)
	}
	m.typeAndEnter(t, "auto")
	cleared, err := m.store.ReviewBase(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if cleared != "" {
		t.Errorf("auto should clear the stored base, got %q", cleared)
	}
}

// umbrellaWithBranchedRepo makes a dir that is not itself a repo but holds one
// nested repo whose feature branch diverges from main, so an overridden base
// yields a different fingerprint than auto-detection. On macOS the discovered
// root string is unresolved (/var/...) while git's toplevel resolves
// (/private/var/...), which is exactly the split the base keying must survive.
func umbrellaWithBranchedRepo(t *testing.T) (umbrella, repoRoot string) {
	t.Helper()
	umbrella = t.TempDir()
	repoRoot = filepath.Join(umbrella, "solo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, out)
		}
	}
	write := func(content string) {
		if err := os.WriteFile(filepath.Join(repoRoot, "a.go"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("git", "init", "-b", "main")
	write("package a\n\nfunc A() int { return 1 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c1")
	run("git", "branch", "feature")
	write("package a\n\nfunc A() int { return 2 }\n")
	run("git", "add", ".")
	run("git", "commit", "-m", "c2")
	return umbrella, repoRoot
}

// A stored base that no longer resolves errors the branch-scope load and clears
// the diff set, leaving the resolved toplevel empty. The B picker must still
// open - keyed off the raw selection - so auto can clear the bad base, which is
// the only recovery path.
func TestInvalidStoredBaseStillOpensPickerAndRecovers(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithSecondBranch(t)
	openReviewOn(t, m, "invalidbase", dir)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}

	if err := m.store.SetReviewBase(sess.ID, m.diff.repoSel, "gone-ref"); err != nil {
		t.Fatal(err)
	}
	m.diff.scope = git.ScopeBranch
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))
	if m.diff.errText == "" {
		t.Fatal("an unresolvable stored base must error the load")
	}
	if m.diff.set.Repo.Root != "" {
		t.Fatalf("the errored load should clear the diff set, root = %q", m.diff.set.Repo.Root)
	}

	m.drainCmds(t, m.openBasePick())
	if m.mode != modeRepoPick {
		t.Fatalf("B must open the base picker after the load errored, mode = %v (err=%q)", m.mode, m.errBar.text)
	}
	labels := map[string]bool{}
	for _, row := range m.repoPick.rows {
		labels[row.label] = true
	}
	if !labels["auto"] || !labels["feature"] {
		t.Fatalf("picker should list auto and the refs, got %v", labels)
	}

	m.typeAndEnter(t, "auto")
	base, err := m.store.ReviewBase(sess.ID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if base != "" {
		t.Fatalf("auto should clear the bad base, got %q", base)
	}
	if m.diff.errText != "" {
		t.Fatalf("clearing the base should let the reload succeed, err = %q", m.diff.errText)
	}
	if m.diff.set.Repo.Root == "" {
		t.Fatal("the recovery reload should rebuild the diff set")
	}
}

// Probe and load must derive the base and fingerprint identically. With an
// unresolved umbrella root and a stored override, the probe has to read the
// base under the raw selection - not the resolved toplevel - or its fingerprint
// diverges from the load's and review reloads every tick forever.
func TestProbeAndLoadAgreeOnFingerprint(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	umbrella, _ := umbrellaWithBranchedRepo(t)
	openReviewOn(t, m, "probe", umbrella)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}

	if err := m.store.SetReviewBase(sess.ID, m.diff.repoSel, "feature"); err != nil {
		t.Fatal(err)
	}
	m.diff.scope = git.ScopeBranch
	m.diff.gen++
	m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))
	if m.diff.errText != "" {
		t.Fatalf("branch-scope load with a valid override should not error, err = %q", m.diff.errText)
	}
	if m.diff.fingerprint == 0 {
		t.Fatal("load should record a non-zero fingerprint")
	}

	msg, ok := m.diffProbeCmd(sess, m.diff.scope)().(diffProbeMsg)
	if !ok {
		t.Fatal("probe closure should yield a diffProbeMsg")
	}
	if msg.repoRoot != m.diff.repoSel {
		t.Fatalf("probe should report the selected repo %q, got %q", m.diff.repoSel, msg.repoRoot)
	}
	if msg.fp != m.diff.fingerprint {
		t.Fatalf("probe fingerprint %d must match the load's %d or review reloads forever (repoSel=%q toplevel=%q)",
			msg.fp, m.diff.fingerprint, m.diff.repoSel, m.diff.set.Repo.Root)
	}
}

// The CLI writes the base under git's symlink-resolved toplevel, while the UI
// discovers umbrella repos under the raw cwd. This exercises the whole path an
// agent's `review-base` takes: mailbox written the way the CLI writes it (the
// resolved root), the poller applying it, then the UI load. The load must key
// its read the same resolved way or the override silently never reaches review.
func TestCLIReviewBaseReachesLoadAcrossSymlinkBoundary(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	_, repoRoot := umbrellaWithBranchedRepo(t)
	umbrella := filepath.Dir(repoRoot)
	openReviewOn(t, m, "cliboundary", umbrella)
	sess, ok := m.diffSession()
	if !ok {
		t.Fatal("no diff session")
	}

	resolvedRoot := resolveSymlinksOrSelf(m.diff.repoSel)
	if resolvedRoot == m.diff.repoSel {
		t.Skip("temp dir is not symlinked, so there is no raw/resolved boundary to cross")
	}

	branchReload := func() {
		m.diff.scope = git.ScopeBranch
		m.diff.gen++
		m.drainCmds(t, m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false))
		if m.diff.errText != "" {
			t.Fatalf("branch-scope load should not error, err = %q", m.diff.errText)
		}
	}

	branchReload()
	autoFingerprint := m.diff.fingerprint

	// Mirror the CLI exactly: OpenRepo yields the same symlink-resolved toplevel
	// the review-base subcommand stores, so the mailbox holds the resolved root.
	repo, err := m.gitDrv.OpenRepo(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Root != resolvedRoot {
		t.Fatalf("test premise broken: git toplevel %q should match the resolved selection %q", repo.Root, resolvedRoot)
	}
	path := m.hooks.ReviewBaseFile(sess.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(repo.Root+"\nfeature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.poller.applyPendingReviewBase(&sess); err != nil {
		t.Fatal(err)
	}

	branchReload()
	if m.diff.fingerprint == autoFingerprint {
		t.Fatalf("the CLI-declared feature base never reached the load: fingerprint stayed at the auto value %d (base keyed under %q was read under %q)",
			autoFingerprint, resolvedRoot, m.diff.repoSel)
	}
	if len(m.diff.set.Files) == 0 {
		t.Fatal("the feature base should surface the diverging file in review")
	}
}

func TestReviewedMarkClearsOnContentChange(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitRepoWithTwoChangedFiles(t)
	openReviewOn(t, m, "reset", dir)
	if len(m.diff.set.Files) == 0 {
		t.Fatal("want at least one changed file")
	}
	path := m.diff.set.Files[0].File.Path

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

func TestReviewProgressAndDraftsRestoreFromStore(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	openReviewOn(t, m, "restore", gitRepoWithTwoChangedFiles(t))
	m.pressDiffKey(t, 'n')
	m.openAnnotate()
	m.diff.annInput.SetValue("keep this feedback")
	m.saveAnnotation()
	path := m.currentFileDiff().File.Path
	m.drainCmds(t, m.toggleReviewed())
	wantHash := m.diff.reviewed[m.reviewKey()][path]
	if wantHash == 0 {
		t.Fatal("reviewed hash was not recorded")
	}

	key := m.reviewKey()
	delete(m.diff.reviewed, key)
	delete(m.diff.annotations, key)
	delete(m.diff.rounds, key)
	delete(m.diff.stateLoaded, key)
	state, err := readReviewState(m.store, m.diff.sessID, m.diff.repoSel)
	if err != nil {
		t.Fatal(err)
	}
	if !m.restoreReviewState(state) {
		t.Fatal("review state was not restored")
	}
	if got := m.diff.reviewed[key][path]; got != wantHash {
		t.Fatalf("restored reviewed hash = %d, want %d", got, wantHash)
	}
	notes := m.diff.annotations[key]
	if len(notes) != 1 || notes[0].text != "keep this feedback" || notes[0].round != 0 {
		t.Fatalf("restored draft = %+v", notes)
	}
}

// Sending review comments writes into the pane the same way the quick
// prompt does, so it refuses a shell for the same reason: the prompt is an
// English sentence, and a shell would run it.
func TestSendAnnotationsRefusesAShell(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	if err := m.store.CreateGroup("work", dir); err != nil {
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

func TestDiffPageKeysMoveAViewport(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithLongFile(t, 80)
	openReviewOn(t, m, "pager", dir)
	before := m.diff.cursorLine
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyPgDown})
	if m.diff.cursorLine <= before {
		t.Fatalf("pgdown left cursor at %d", m.diff.cursorLine)
	}
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.diff.cursorLine != before {
		t.Fatalf("pgup should return to %d, got %d", before, m.diff.cursorLine)
	}
}

func TestDiffHelpReturnsToReview(t *testing.T) {
	m := buildModel(t)
	dir := gitTestRepo(t)
	openReviewOn(t, m, "helper", dir)
	m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	if m.mode != modeHelp {
		t.Fatalf("? should open the key map, mode = %v", m.mode)
	}
	if !m.diff.active {
		t.Fatal("opening help must not close the review")
	}
	m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.mode != modeDiff || !m.diff.active {
		t.Fatalf("esc should return to review, mode = %v active = %v", m.mode, m.diff.active)
	}
}

func TestDiffHelpRestartsLoaderOnReturn(t *testing.T) {
	m := &Model{mode: modeDiff, diff: diffState{active: true, loading: true}}
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader")
	}
	m.openHelp()
	m.Update(startupTickMsg{})
	if m.startupAnimating {
		t.Fatal("loader should stop while help covers the review")
	}
	_, cmd := m.handleHelpKey(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil || !m.startupAnimating {
		t.Fatal("returning to a loading review should restart the loader")
	}
}

func TestDiffHeaderKeepsCountsWhenNarrow(t *testing.T) {
	m := buildModel(t)
	m.width, m.height = 52, 24
	openReviewOn(t, m, "very-long-session-name", gitTestRepo(t))
	got := ansi.Strip(m.viewDiffHeader("very-long-session-name"))
	if !strings.Contains(got, "+") || !strings.Contains(got, "−") {
		t.Fatalf("narrow header dropped counts:\n%s", got)
	}
}

func TestDiffFileListTruncatesAtSlash(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "paths", gitTestRepo(t))
	m.diff.set.Files[0].File.Path = "internal/api/handlers/sessions.go"
	list := ansi.Strip(m.viewDiffFileList(28, 8))
	if strings.Contains(list, "ternal") {
		t.Fatalf("file list cut mid-segment:\n%s", list)
	}
	if !strings.Contains(list, "sessions.go") {
		t.Fatalf("file list lost the filename:\n%s", list)
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

func TestDiffProbeSetsLoadingSoItDoesNotStack(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "probe-load", gitTestRepo(t))
	gen := m.diff.gen
	fp := m.diff.fingerprint
	if fp == 0 {
		t.Fatal("loaded review should have a fingerprint")
	}
	cmd := m.handleDiffProbe(diffProbeMsg{
		sessID: m.diff.sessID, scope: m.diff.scope, repoRoot: m.diff.repoSel, fp: fp + 1,
	})
	if cmd == nil {
		t.Fatal("a changed fingerprint should start a reload")
	}
	if !m.diff.loading {
		t.Fatal("reload must set loading so the next probe cannot cancel it")
	}
	if m.diff.gen != gen+1 {
		t.Fatalf("gen = %d, want %d", m.diff.gen, gen+1)
	}
	if stacked := m.handleDiffProbe(diffProbeMsg{
		sessID: m.diff.sessID, scope: m.diff.scope, repoRoot: m.diff.repoSel, fp: fp + 2,
	}); stacked != nil {
		t.Fatal("a probe while loading must not start another reload")
	}
	if m.diff.gen != gen+1 {
		t.Fatalf("stacked probe bumped gen to %d", m.diff.gen)
	}
	if m.diffRefreshCmd() != nil {
		t.Fatal("refresh must wait until the in-flight load lands")
	}
}

func TestCycleDiffScopeKeepsSetWhenBaseLookupFails(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "keepset", gitTestRepo(t))
	if len(m.diff.set.Files) == 0 {
		t.Fatal("expected files")
	}
	files := len(m.diff.set.Files)
	scope := m.diff.scope
	if err := m.store.Close(); err != nil {
		t.Fatal(err)
	}
	if cmd := m.cycleDiffScope(); cmd != nil {
		t.Fatal("a failed lookup should not start a load")
	}
	if m.diff.scope != scope {
		t.Fatalf("scope advanced to %s on a failed lookup", m.diff.scope)
	}
	if len(m.diff.set.Files) != files {
		t.Fatalf("set was wiped, %d files left", len(m.diff.set.Files))
	}
	if m.errBar.text == "" {
		t.Fatal("the lookup error should be shown")
	}
}

func TestReviewUntrackedFileShowsCountWithoutOpening(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "counts", gitTestRepo(t))
	var extra *diff.FileDiff
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "extra.txt" {
			extra = &m.diff.set.Files[i]
		}
	}
	if extra == nil {
		t.Fatal("extra.txt missing")
	}
	if extra.Loaded() {
		t.Fatal("unselected untracked file should stay unloaded")
	}
	if !extra.StatKnown() || extra.Stat.Adds < 1 {
		t.Fatalf("untracked extra.txt should already have a +N, known=%v stat=%+v", extra.StatKnown(), extra.Stat)
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if strings.Contains(list, "?") {
		t.Fatalf("file list should not use ? for a counted untracked file:\n%s", list)
	}
	if !strings.Contains(list, "+") {
		t.Fatalf("file list should show adds for extra.txt:\n%s", list)
	}
}

func TestReviewUntrackedImageShowsBinaryWithoutOpening(t *testing.T) {
	m := buildModel(t)
	if m.gitDrv == nil {
		t.Skip("git not installed")
	}
	dir := gitTestRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), []byte("\x89PNG\r\n\x1a\n\x00\x00binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	openReviewOn(t, m, "shots", dir)
	var shot *diff.FileDiff
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == "shot.png" {
			shot = &m.diff.set.Files[i]
		}
	}
	if shot == nil {
		t.Fatal("shot.png missing")
	}
	if shot.Loaded() {
		t.Fatal("unselected image should stay unloaded")
	}
	if !shot.StatKnown() || !shot.Stat.Binary {
		t.Fatalf("untracked image should count as binary, known=%v stat=%+v", shot.StatKnown(), shot.Stat)
	}
	list := ansi.Strip(m.viewDiffFileList(60, 20))
	if !strings.Contains(list, "binary") {
		t.Fatalf("file list should say binary, not ?:\n%s", list)
	}
}

func TestReviewShowsLoaderWhileDiffLoads(t *testing.T) {
	m := &Model{width: 100, height: 30, mode: modeDiff, diff: diffState{active: true, loading: true, sessID: "s"}}
	code := ansi.Strip(m.viewDiffCode(80, 20))
	if !strings.Contains(code, "loading diff") {
		t.Fatalf("code pane should carry the diff loader, got %q", code)
	}
	if strings.Count(code, "●") != 1 || strings.Count(code, "•") != 1 {
		t.Fatalf("diff loader should show the ring, got %q", code)
	}
	list := ansi.Strip(m.viewDiffFileList(28, 10))
	if !strings.Contains(list, "loading diff") {
		t.Fatalf("file list should carry the compact loader, got %q", list)
	}
	if cmd := m.startStartupTick(); cmd == nil {
		t.Fatal("loading review should start the loader tick")
	}
	first := code
	m.Update(startupTickMsg{})
	if next := ansi.Strip(m.viewDiffCode(80, 20)); next == first {
		t.Fatal("diff loader did not move on the tick")
	}
}

func TestReviewShowsLoaderWhileFileLoads(t *testing.T) {
	m := &Model{
		width: 100, height: 30, mode: modeDiff,
		diff: diffState{
			active: true,
			sessID: "s",
			set:    diff.Set{Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}},
		},
	}
	code := ansi.Strip(m.viewDiffCode(80, 20))
	if !strings.Contains(code, "loading file") {
		t.Fatalf("code pane should carry the file loader, got %q", code)
	}
	if strings.Count(code, "●") != 1 {
		t.Fatalf("file loader should show the ring, got %q", code)
	}
	list := ansi.Strip(m.viewDiffFileList(40, 8))
	if strings.Contains(list, "loading") {
		t.Fatalf("file list should keep the files while one loads, got %q", list)
	}
}

func TestFailedDiffLoadKeepsRepoPicker(t *testing.T) {
	m := buildModel(t)
	openReviewOn(t, m, "keeprepo", gitTestRepo(t))
	roots := append([]string{}, m.diff.repoRoots...)
	sel := m.diff.repoSel
	if len(roots) == 0 {
		t.Fatal("expected repo roots")
	}
	if cmd := m.handleDiffLoaded(diffLoadedMsg{
		sessID:    m.diff.sessID,
		scope:     m.diff.scope,
		gen:       m.diff.gen,
		err:       errors.New("git died"),
		repoRoots: roots,
		repoRoot:  sel,
	}); cmd != nil {
		t.Fatal("errored load should not follow up")
	}
	if m.diff.errText == "" {
		t.Fatal("error text missing")
	}
	if len(m.diff.repoRoots) == 0 {
		t.Fatal("repo list should survive a failed load so r still works")
	}
	m.openRepoPick()
	if m.mode != modeRepoPick {
		t.Fatalf("r should still open, mode = %v", m.mode)
	}
}
