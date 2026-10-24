package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	"github.com/charmbracelet/x/ansi"
)

func TestStartupTickRunsWhileBooting(t *testing.T) {
	m := &Model{startup: startupState{Booting: true}}
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.Animating {
		t.Fatal("boot should start the loader tick")
	}
	m.startup.Booting = false
	_, cmd := m.Update(uistartup.TickMsg{})
	if cmd != nil || m.startup.Animating {
		t.Fatal("loader tick kept running after boot settled")
	}
}

func TestFirstRefreshClearsBootLoader(t *testing.T) {
	m := &Model{startup: startupState{Booting: true}}
	updated, _ := m.Update(refreshMsg{listedAt: time.Now()})
	got := updated.(*Model)
	if got.startup.Booting {
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
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.Animating {
		t.Fatal("starting row did not begin the startup tick")
	}
	if cmd := m.startStartupTick(); cmd != nil {
		t.Fatal("an active startup tick was scheduled twice")
	}
	setRailSessionStatus(m, "starting", status.Idle)
	_, cmd := m.Update(uistartup.TickMsg{})
	if cmd != nil || m.startup.Animating {
		t.Fatal("startup tick kept running after the starting row settled")
	}
}

func TestStartupTickRunsWhileReviewLoads(t *testing.T) {
	m := &Model{screen: screen{base: baseReview}}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{}, false)
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.Animating {
		t.Fatal("a loading review should start the loader tick")
	}
	state := m.review.Snapshot()
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation,
		RepoRoot: "/repo", RepoRoots: []string{"/repo"}, Set: diff.Set{}, SavedLoaded: true,
	})
	_, cmd := m.Update(uistartup.TickMsg{})
	if cmd != nil || m.startup.Animating {
		t.Fatal("loader tick kept running after the review load settled")
	}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{Files: []diff.FileDiff{{File: git.ChangedFile{Path: "main.go"}}}}, true)
	if cmd := m.startStartupTick(); cmd == nil || !m.startup.Animating {
		t.Fatal("an unloaded selected file should start the loader tick")
	}
	seedReviewForTest(m, uireview.Target{ID: "review"}, git.ScopeUncommitted, "/repo", diff.Set{Files: []diff.FileDiff{diff.BuildFile(nil, nil, git.ChangedFile{Path: "main.go"}, git.FileStat{})}}, true)
	_, cmd = m.Update(uistartup.TickMsg{})
	if cmd != nil || m.startup.Animating {
		t.Fatal("loader tick kept running after the selected file loaded")
	}
}

func TestStartupErrorStaysVisibleUntilFirstRefresh(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.startup.Booting = true
	m.Update(errMsg{errors.New("startup poll failed")})
	if !m.startup.Booting {
		t.Fatal("an error before the first refresh must not finish boot")
	}
	if !strings.Contains(ansi.Strip(preparedView(m)), "startup poll failed") {
		t.Fatal("startup error is hidden behind the boot loader")
	}
	m.Update(refreshMsg{listedAt: time.Now()})
	if m.startup.Booting {
		t.Fatal("the first successful refresh must finish boot")
	}
}

// A settled wordmark keeps a timer alive for the next shimmer, and the
// shimmer message starts the sweep over.
func TestBannerShimmerReArms(t *testing.T) {
	m := &Model{
		startup: startupState{
			BannerPhase: uistartup.Frames,
		},
	}
	if m.bannerTick() == nil {
		t.Fatal("a settled banner should wait for the next shimmer, not stop")
	}
	updated, cmd := m.Update(uistartup.BannerShimmerMsg{})
	m = updated.(*Model)
	if m.startup.BannerPhase != 0 {
		t.Fatalf("shimmer should restart the sweep, phase = %d", m.startup.BannerPhase)
	}
	if cmd == nil {
		t.Fatal("a restarted sweep must schedule its frames")
	}
}

func TestStartingSessionGlyphMovesOnTheStartupTick(t *testing.T) {
	for _, tool := range []string{"agent", "shell"} {
		m := previewModel(status.Starting, blankCapture)
		selected := railSelectedSession(m)
		setRailSessionTool(m, selected.ID, tool)
		m.services.cfg.Tools = map[string]config.Tool{"shell": {Shell: true}}
		first := ansi.Strip(m.sessionGlyph(railSelectedSession(m)))
		m.Update(uistartup.TickMsg{})
		second := ansi.Strip(m.sessionGlyph(railSelectedSession(m)))
		if first == second {
			t.Fatalf("starting %s glyph stayed on %q", tool, first)
		}
		if second == m.glyphs().ShellGlyph {
			t.Fatal("starting shell used the resting shell glyph")
		}
	}
}

func TestBootShowsFullScreenRing(t *testing.T) {
	m := shotModel()
	m.startup.Booting = true
	body := ansi.Strip(preparedView(m))
	if strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("boot should hide the list:\n%s", body)
	}
	if !strings.Contains(body, "loading") {
		t.Fatalf("boot should be the preview ring, got:\n%s", body)
	}
	m.startup.Booting = false
	body = ansi.Strip(preparedView(m))
	if !strings.Contains(body, "add-rate-limiting") {
		t.Fatalf("after boot the list should show:\n%s", body)
	}
}
