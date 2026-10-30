package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestFormWorktreePresentationSurvivesExpiry(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	m.form().SetDir(filepath.Join(t.TempDir(), "worktree-base"))
	dir := m.formSpawnDir()
	m.ledger.spawner.WorktreeRepos = map[string]uilaunch.RepoAnswer{
		dir: {Capable: true, At: time.Now().Add(-24 * time.Hour)},
	}
	for _, on := range []bool{true, false} {
		m.form().SetWorktree(on)
		view := ansi.Strip(m.viewForm())
		if m.formWorktreeOn() != on || strings.Contains(view, uilaunch.WorktreeUnavailable) {
			t.Fatalf("expired repository lost available state (on=%t): %q", on, view)
		}
		want := "off"
		if on {
			want = "on"
		}
		worktreeRow, baseShown := "", false
		for _, line := range strings.Split(view, "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch fields[1] {
			case "worktree":
				worktreeRow = line
			case "base":
				baseShown = true
			}
		}
		if !strings.Contains(worktreeRow, " "+want+" ") || baseShown != on {
			t.Fatalf("form should show worktree %s and base only when on: %q", want, view)
		}
	}
	m.form().SetWorktree(true)
	m.ledger.spawner.WorktreeRepos[dir] = uilaunch.RepoAnswer{At: time.Now().Add(-24 * time.Hour)}
	if view := ansi.Strip(m.viewForm()); m.formWorktreeOn() || !strings.Contains(view, uilaunch.WorktreeUnavailable) {
		t.Fatalf("expired negative should retain the unavailable form state: %q", view)
	}
}

func TestWorktreeToggleFreshness(t *testing.T) {
	t.Parallel()
	for _, quick := range []bool{false, true} {
		surface := "form"
		if quick {
			surface = "quick"
		}
		for _, tt := range []struct {
			name                  string
			cached, stale, from   bool
			probed, want, wantErr bool
		}{
			{name: "fresh negative", wantErr: true},
			{name: "fresh positive", cached: true, want: true},
			{name: "expired negative discovers repo", stale: true, probed: true, want: true},
			{name: "expired negative remains unavailable", stale: true, wantErr: true},
			{name: "expired positive becomes unavailable", cached: true, stale: true, wantErr: true},
			{name: "expired positive enables once", cached: true, stale: true, probed: true, want: true},
			{name: "expired positive disables once", cached: true, stale: true, from: true, probed: true},
			{name: "expired positive rejects disable", cached: true, stale: true, from: true, want: true, wantErr: true},
		} {
			t.Run(surface+"/"+tt.name, func(t *testing.T) {
				m := buildModel(t)
				var dir string
				var generation int
				target := uilaunch.ProbeForm
				worktree := func() bool { return m.form().Worktree() }
				if quick {
					m.openQuickMode()
					m.quick().SetWorktree(tt.from)
					dir, generation, target = m.quickTargetDir(), m.quick().Gen, uilaunch.ProbeQuick
					worktree = func() bool { return m.quick().Worktree() }
				} else {
					m.openForm()
					m.form().SetWorktree(tt.from)
					dir, generation = m.formSpawnDir(), m.formPromptFor().Gen
				}
				at := time.Now()
				if tt.stale {
					at = at.Add(-24 * time.Hour)
				}
				m.ledger.spawner.WorktreeRepos = map[string]uilaunch.RepoAnswer{dir: {Capable: tt.cached, At: at}}
				sequence := m.gens.worktreeProbe
				var cmd tea.Cmd
				if quick {
					_, cmd = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlT})
				} else {
					m.form().FocusField(m.spawnHost(), spawn.FieldWorktree)
					_, cmd = m.handleFormKey(tea.KeyMsg{Type: tea.KeyRight})
				}
				if (cmd != nil) != tt.stale {
					t.Fatalf("deferred probe = %t, want %t", cmd != nil, tt.stale)
				}
				if tt.stale {
					if m.gens.worktreeProbe != sequence+1 || worktree() != tt.from {
						t.Fatal("toggle changed before its deferred probe completed")
					}
					if got := m.ledger.spawner.WorktreeRepos[dir]; got.Capable != tt.cached || !got.At.Equal(at) {
						t.Fatal("requesting a refresh discarded the display observation")
					}
					m.handleWorktreeProbe(uilaunch.ProbeMsg{Request: uilaunch.ProbeRequest{
						Target: target, Generation: generation, Sequence: m.gens.worktreeProbe,
						Dir: dir, Toggle: true, From: tt.from,
					}, Capable: tt.probed})
				}
				if got := worktree(); got != tt.want || strings.Contains(m.errBar.text, "need a git repository") != tt.wantErr {
					t.Fatalf("toggle = %t error = %q; want %t error %t", got, m.errBar.text, tt.want, tt.wantErr)
				}
			})
		}
	}
}

func TestWorktreeObservationIsDirectoryScoped(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.ledger.spawner.WorktreeRepos = map[string]uilaunch.RepoAnswer{
		"old":  {Capable: true, At: time.Now().Add(-24 * time.Hour)},
		"zero": {Capable: true},
	}
	for _, dir := range []string{"new", "zero"} {
		if _, known := m.cachedWorktreeCapability(dir); known {
			t.Fatalf("unobserved directory %q inherited a repository verdict", dir)
		}
		if _, fresh := m.freshWorktreeCapability(dir); fresh {
			t.Fatalf("unobserved directory %q has a fresh repository verdict", dir)
		}
	}
}
