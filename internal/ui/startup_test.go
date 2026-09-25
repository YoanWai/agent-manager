package ui

import (
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

func TestStartupTickRunsWhileBooting(t *testing.T) {
	m := &Model{booting: true}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("boot should start the loader tick")
	}
	m.booting = false
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("loader tick kept running after boot settled")
	}
}

func TestFirstRefreshClearsBootLoader(t *testing.T) {
	m := &Model{booting: true, collapsed: map[string]bool{}}
	updated, _ := m.Update(refreshMsg{listedAt: time.Now()})
	got := updated.(*Model)
	if got.booting {
		t.Fatal("the first poller pass should end boot")
	}
}

func TestStartupTickRunsOnlyWhileAStartingRowIsVisible(t *testing.T) {
	m := &Model{}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("startup tick began without a starting row")
	}
	m.rows = []treeRow{{sess: store.Session{Status: status.Starting}}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("starting row did not begin the startup tick")
	}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("an active startup tick was scheduled twice")
	}
	m.rows[0].sess.Status = status.Idle
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("startup tick kept running after the starting row settled")
	}
}

func TestStartupTickRunsWhileReviewLoads(t *testing.T) {
	m := &Model{mode: modeDiff, diff: diffState{active: true, loading: true}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("a loading review should start the loader tick")
	}
	m.diff.loading = false
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("loader tick kept running after the review load settled")
	}
	m.diff.set.Files = []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startupAnimating {
		t.Fatal("an unloaded selected file should start the loader tick")
	}
	m.diff.set.Files[0] = diff.BuildFile(nil, nil, git.ChangedFile{Path: "main.go"}, git.FileStat{})
	_, cmd = m.Update(startupTickMsg{})
	if cmd != nil || m.startupAnimating {
		t.Fatal("loader tick kept running after the selected file loaded")
	}
}
