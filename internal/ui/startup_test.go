package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/charmbracelet/x/ansi"
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
	m := &Model{startup: startupState{booting: true}}
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
	m.workspace.sessions = []store.Session{{ID: "starting", Status: status.Starting}}
	m.rebuildRows()
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("starting row did not begin the startup tick")
	}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("an active startup tick was scheduled twice")
	}
	setRailSessionStatus(m, "starting", status.Idle)
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startup.startupAnimating {
		t.Fatal("startup tick kept running after the starting row settled")
	}
}

func TestStartupTickRunsWhileReviewLoads(t *testing.T) {
	m := &Model{mode: modeDiff}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("a loading review should start the loader tick")
	}
	state := m.review.Snapshot()
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: diff.Set{}, SavedLoaded: true,
	})
	_, cmd := m.Update(startupTickMsg{})
	if cmd != nil || m.startup.startupAnimating {
		t.Fatal("loader tick kept running after the review load settled")
	}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}}, true)
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.startupAnimating {
		t.Fatal("an unloaded selected file should start the loader tick")
	}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{Files: []diff.FileDiff{diff.BuildFile(nil, nil, git.ChangedFile{Path: "main.go"}, git.FileStat{})}}, true)
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
	if !strings.Contains(ansi.Strip(preparedView(m)), "startup poll failed") {
		t.Fatal("startup error is hidden behind the boot loader")
	}
	m.Update(refreshMsg{listedAt: time.Now()})
	if m.startup.booting {
		t.Fatal("the first successful refresh must finish boot")
	}
}
