package ui

import (
	"errors"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"
)

func TestStartupTickRunsWhileBooting(t *testing.T) {
	m := &Model{startup: startupState{booting: true}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("boot should start the loader tick")
	}
	m.startup.booting = false
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startup.startupAnimating {
		t.Fatal("loader tick kept running after boot settled")
	}
}

func TestFirstRefreshClearsBootLoader(t *testing.T) {
	m := &Model{rail: railState{collapsed: map[string]bool{}}, startup: startupState{booting: true}}
	updated, _ := m.Update(refreshMsg{listedAt: time.Now()})
	got := updated.(*Model)
	if got.startup.booting {
		t.Fatal("the first poller pass should end boot")
	}
}

func TestStartupTickRunsOnlyWhileAStartingRowIsVisible(t *testing.T) {
	m := &Model{}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("startup tick began without a starting row")
	}
	m.rail.rows = []treeRow{{sess: store.Session{Status: status.Starting}}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("starting row did not begin the startup tick")
	}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("an active startup tick was scheduled twice")
	}
	m.rail.rows[0].sess.Status = status.Idle
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startup.startupAnimating {
		t.Fatal("startup tick kept running after the starting row settled")
	}
}

func TestStartupTickRunsWhileReviewLoads(t *testing.T) {
	m := &Model{mode: modeDiff, diff: diffState{active: true, loading: true}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("a loading review should start the loader tick")
	}
	m.diff.loading = false
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startup.startupAnimating {
		t.Fatal("loader tick kept running after the review load settled")
	}
	m.diff.set.Files = []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("an unloaded selected file should start the loader tick")
	}
	m.diff.set.Files[0] = diff.BuildFile(nil, nil, git.ChangedFile{Path: "main.go"}, git.FileStat{})
	_, cmd = m.Update(startupTickMsg{})
	if cmd != nil || m.startup.startupAnimating {
		t.Fatal("loader tick kept running after the selected file loaded")
	}
}

func TestStartupErrorStaysVisibleUntilFirstRefresh(t *testing.T) {
	m := buildModel(t)
	m.startup.booting = true
	m.Update(errMsg{errors.New("startup poll failed")})
	if !m.startup.booting {
		t.Fatal("an error before the first refresh must not finish boot")
	}
	if !strings.Contains(ansi.Strip(m.View()), "startup poll failed") {
		t.Fatal("startup error is hidden behind the boot loader")
	}
	m.Update(refreshMsg{listedAt: time.Now()})
	if m.startup.booting {
		t.Fatal("the first successful refresh must finish boot")
	}
}
