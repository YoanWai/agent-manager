package onboarding

import (
	"sync/atomic"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

var nextSurfaceID atomic.Uint64

// Surface owns the setup's choices, probe and animation while mounted.
type Surface struct {
	Model
	root            Root
	id              uint64
	autoPalette     func(string) string
	probed, ticking bool
	// A command wakes the root, but choices remain in input order even when
	// those command completions arrive in a different order.
	pending []Outcome
}

type Root interface{ Paint(*Surface) string }

type OutcomeMsg struct{ Surface *Surface }
type TickMsg struct{ Surface *Surface }

func NewSurface(root Root, agents []string, choices Choices, firstRun bool) *Surface {
	return &Surface{Model: New(agents, choices, firstRun), root: root, id: nextSurfaceID.Add(1)}
}
func (s *Surface) ID() uint64    { return s.id }
func (s *Surface) Probed() bool  { return s.probed }
func (s *Surface) Ticking() bool { return s.ticking }
func (s *Surface) SetProbe(installed map[string]bool, autoPalette func(string) string) {
	s.SetInstalled(installed)
	s.autoPalette, s.probed = autoPalette, true
}
func (s *Surface) Palette(choices Choices, current string) string {
	if !choices.ThemeAuto {
		return choices.Theme
	}
	if !s.probed {
		return current
	}
	return s.autoPalette(choices.Theme)
}
func (s *Surface) outcome(out Outcome) tea.Cmd {
	s.pending = append(s.pending, out)
	return func() tea.Msg { return OutcomeMsg{Surface: s} }
}
func (s *Surface) TakeOutcomes() []Outcome { out := s.pending; s.pending = nil; return out }
func (s *Surface) Key(_ surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.outcome(s.Model.Key(msg))
}
func (s *Surface) ClickAt(_ surface.Host, col, line int) tea.Cmd {
	return s.outcome(s.Model.Click(col, line))
}
func (s *Surface) Click(h surface.Host, line int) tea.Cmd { return s.ClickAt(h, 0, line) }
func (s *Surface) Wheel(_ surface.Host, arrow tea.KeyMsg) tea.Cmd {
	step := 1
	if arrow.Type == tea.KeyUp {
		step = -1
	}
	return s.outcome(s.Model.Wheel(step))
}
func (s *Surface) View(surface.Host, string) string { return s.root.Paint(s) }
func (s *Surface) Footer() [][2]string              { return nil }
func (s *Surface) Owner() surface.Owner             { return surface.Manager }
func (s *Surface) Dismissible() bool                { return false }
func (s *Surface) Leave(surface.Host)               { s.ticking = false }
func (s *Surface) KeepAnimating() tea.Cmd {
	if !s.Animating() || s.ticking {
		return nil
	}
	s.ticking = true
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return TickMsg{s} })
}
func (s *Surface) Advance() tea.Cmd { s.ticking = false; s.Tick(); return s.KeepAnimating() }
