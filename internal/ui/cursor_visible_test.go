package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// Whatever the cursor is on has to be on screen. A window that reserves
// room for one overflow indicator but paints two loses a row at the bottom,
// and the row it loses is the one the cursor just moved to.
func TestRailCursorAlwaysPainted(t *testing.T) {
	now := time.Now()
	sessions := make([]store.Session, 40)
	rows := make([]treeRow, len(sessions))
	for i := range sessions {
		name := fmt.Sprintf("session-%02d", i)
		sessions[i] = store.Session{
			ID: name, Name: name, Tool: "claude", Status: status.Idle,
			CreatedAt: now, LastStatusAt: now,
		}
		rows[i] = treeRow{sess: sessions[i]}
	}

	for _, size := range []struct{ w, h int }{{80, 16}, {100, 24}, {120, 30}, {160, 44}} {
		for _, cursor := range []int{0, 1, len(rows) / 2, len(rows) - 2, len(rows) - 1} {
			m := &Model{
				width: size.w, height: size.h, mode: modeList,
				sessions: sessions, rows: rows, cursor: cursor,
				collapsed: map[string]bool{}, split: splitState{ratio: defaultSplitRatio},
			}
			view := ansi.Strip(m.View())
			if !strings.Contains(view, sessions[cursor].Name) {
				t.Errorf("%dx%d cursor=%d: %q is selected but never painted:\n%s",
					size.w, size.h, cursor, sessions[cursor].Name, view)
			}
		}
	}
}

// The same rule for review's file list: tabbing to the last file has to
// bring it on screen, not just select it.
func TestDiffFileListCursorAlwaysPainted(t *testing.T) {
	m := buildModel(t)
	dir := gitRepoWithManyFiles(t, 40)
	createSession(t, m, "coder", dir, "")
	m.selectSessionRow(t, "coder")
	m.drainCmds(t, m.openDiff())
	if m.diff.loading || len(m.diff.set.Files) < 2 {
		t.Fatalf("diff did not load: %q", m.diff.errText)
	}

	last := len(m.diff.set.Files) - 1
	for _, size := range []struct{ w, h int }{{80, 20}, {100, 30}, {140, 44}} {
		m.width, m.height = size.w, size.h
		m.diff.fileIdx = last
		m.drainCmds(t, m.loadCurrentDiffFile())
		view := ansi.Strip(m.viewDiffFull())
		name := m.diff.set.Files[last].File.Path
		if !strings.Contains(view, name) {
			t.Errorf("%dx%d: file %q is selected but never painted:\n%s", size.w, size.h, name, view)
		}
	}
}

// gitRepoWithManyFiles is a repo with n changed files, enough to overflow
// review's file list.
func gitRepoWithManyFiles(t *testing.T, n int) string {
	t.Helper()
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
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("seed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("add", "-A")
	run("commit", "-m", "init")
	for i := 0; i < n; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(path, []byte("seed\nchanged\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
