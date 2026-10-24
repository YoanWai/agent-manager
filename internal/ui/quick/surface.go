package quick

import (
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Aim is the row the user last pointed the bar at, as the root names it: a
// refresh that drops a connection's row moves the cursor on its own, and
// enter must not follow it to another agent or host. The root converts its
// own row type, so quick keeps only the fields an aim compares.
type Aim struct {
	Kind      uint8
	SessionID string
	Group     string
	Host      string
}

// Surface is the quick bar over the list. It is not a card: the bar paints
// into the list's footer and the rail stays live under it, so the list keeps
// the mouse and the cursor the bar aims at. It holds a draft, so a stray
// click keeps it and only esc, a send that closes it, or a screen that takes
// the list's place drops it.
type Surface struct {
	Bar
	composer.Composer
	// CloseAfterSend is the setting: a confirmed send takes the bar down.
	CloseAfterSend bool
	// Aim is the row the bar was pointed at, where enter must still land.
	Aim              Aim
	OriginX, OriginY int
	root             SurfaceRoot
}

type SurfaceRoot interface {
	// Key runs a bar key on the root: the rail's cursor, the catalog, the
	// send or the spawn.
	Key(msg tea.KeyMsg) (tea.Cmd, bool)
	// Mouse runs the mouse on the list, which the bar is docked in.
	Mouse(msg tea.MouseMsg) tea.Cmd
}

func NewSurface(root SurfaceRoot) *Surface { return &Surface{root: root} }

func (s *Surface) Key(_ surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	cmd, handled := s.root.Key(msg)
	return handled, cmd
}

// Mouse is the list's: the rail moves the bar's target, the divider drags,
// and the list routes a press on the bar back here.
func (s *Surface) Mouse(_ surface.Host, msg tea.MouseMsg) tea.Cmd { return s.root.Mouse(msg) }

func (s *Surface) Click(surface.Host, int) tea.Cmd { return nil }

func (s *Surface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

// View leaves the frame as it is: the list footer paints the bar.
func (s *Surface) View(_ surface.Host, below string) string { return below }

// Footer is empty: the bar is the footer, painted by the list.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the prompt is a draft, so a stray click keeps it.
func (s *Surface) Dismissible() bool { return false }

// Leave lets go of the images the prompt held; a send has already taken the
// ones it carries.
func (s *Surface) Leave(surface.Host) { s.Composer.Release() }
