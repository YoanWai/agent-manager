package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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
	if _, ok := m.layout.keyHits.at(x, y); !ok {
		t.Fatal("the send question's own keys should be click targets")
	}
	if m = clickLabel(t, m, "esc cancel"); m.review.Snapshot().SendConfirm || m.mode != modeDiff {
		t.Fatalf("esc in the question should cancel it, confirm %v mode %v", m.review.Snapshot().SendConfirm, m.mode)
	}
}
