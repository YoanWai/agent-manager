package startup

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// LoaderInterval is the loader animation's frame time while rows start.
const LoaderInterval = 80 * time.Millisecond

// TickMsg is one loader frame.
type TickMsg struct{}

// BannerTickMsg is one wordmark sweep frame.
type BannerTickMsg struct{}

// BannerShimmerMsg is the rest after a finished sweep.
type BannerShimmerMsg struct{}

// State is the startup animation's own state.
type State struct {
	// SessionsSized flips after the first refresh shrinks sessions left
	// over from a previous manager run to the preview panel's width.
	SessionsSized bool
	BannerPhase   int
	LoadPhase     int
	Animating     bool
	Booting       bool
}

// Host is what the startup animation reads on the root.
type Host interface {
	// LoaderNeeded reports whether a starting row still shows a loader.
	LoaderNeeded() bool
	// ReportWarn puts a warning on the status bar.
	ReportWarn(text string)
}

// Tick schedules one loader frame.
func Tick() tea.Cmd {
	return tea.Tick(LoaderInterval, func(time.Time) tea.Msg { return TickMsg{} })
}

// StartTick asks for the first loader frame when one is needed, one at a time.
func (s *State) StartTick(host Host) tea.Cmd {
	if s.Animating || !host.LoaderNeeded() {
		return nil
	}
	s.Animating = true
	return Tick()
}

// BannerTick drives a sweep frame by frame; a settled wordmark waits out the
// shimmer interval before the next one.
func (s *State) BannerTick() tea.Cmd {
	if s.BannerPhase >= Frames {
		return tea.Tick(ShimmerEvery, func(time.Time) tea.Msg { return BannerShimmerMsg{} })
	}
	return tea.Tick(Interval, func(time.Time) tea.Msg { return BannerTickMsg{} })
}

// Update runs one startup message against the state. A message this package
// does not own returns false and leaves the state alone.
func (s *State) Update(host Host, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case PaneShellMsg:
		if msg.Warning != "" {
			host.ReportWarn(msg.Warning)
		}
		return nil, true

	case BannerTickMsg:
		s.BannerPhase++
		return s.BannerTick(), true

	case BannerShimmerMsg:
		s.BannerPhase = 0
		return s.BannerTick(), true

	case TickMsg:
		if !host.LoaderNeeded() {
			s.Animating = false
			return nil, true
		}
		s.LoadPhase++
		return Tick(), true
	}
	return nil, false
}

// PaneShellMsg carries a warning about the pane shell, empty when it has
// none.
type PaneShellMsg struct{ Warning string }

// CheckPaneShell runs the pane shell's version probe off the update path:
// it starts a PowerShell.
func CheckPaneShell() tea.Msg { return PaneShellMsg{Warning: PaneShellWarning()} }
