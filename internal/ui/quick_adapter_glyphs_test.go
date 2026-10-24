package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestRetainedQuickPromptReadsTheCurrentGlyphMode(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetWidth(70)
	m.quick.input.SetValue("❯↳⚙ literal input")
	for _, tc := range []struct {
		mode presentation.GlyphMode
		mark string
	}{{presentation.GlyphModeBasic, ">"}, {presentation.GlyphModeAdvanced, "❯"}, {presentation.GlyphModeBasic, ">"}} {
		m.prefs.glyphMode = tc.mode
		got := ansi.Strip(m.quick.input.View())
		if !strings.Contains(got, tc.mark+" ❯↳⚙ literal input") {
			t.Fatalf("retained prompt in %s = %q", tc.mode, got)
		}
	}
}

func TestQuickWorktreeMarkKeepsItsKeyboardAndMouseTarget(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("work", gitTestRepo(t)); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	for _, tc := range []struct {
		mode presentation.GlyphMode
		mark string
	}{{presentation.GlyphModeBasic, "Y"}, {presentation.GlyphModeAdvanced, "⎇"}} {
		m.prefs.glyphMode = tc.mode
		for _, width := range []int{80, 120} {
			m.layout.width, m.layout.height = width, 30
			m.quick.SetWorktree(false)
			applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
			if !m.quickWorktreeOn() {
				t.Fatal("keyboard did not enable worktree")
			}
			frame := ansi.Strip(preparedView(m))
			if !strings.Contains(frame, tc.mark+" on") {
				t.Fatalf("%s/%d worktree mark missing:\n%s", tc.mode, width, frame)
			}
			found := false
			for _, hit := range m.quick.Hits() {
				if hit.Action == uiquick.ClickWorktree {
					found = true
					m = leftClick(m, m.quick.originX+hit.X0, m.quick.originY+hit.Line)
					break
				}
			}
			if !found || m.quickWorktreeOn() {
				t.Fatalf("%s/%d worktree click missed", tc.mode, width)
			}
		}
	}
}
