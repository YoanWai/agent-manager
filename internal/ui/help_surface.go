package ui

import (
	"strings"

	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpSurface is the key map. It opens over whatever screen is underneath,
// the list or Review, and closing pops back to it, so nothing remembers
// where it came from.
type helpSurface struct {
	state uihelp.State
	root  helpHost
}

// helpHost is what the key map reads live beyond surfaceHost, whose cap
// leaves no room for it: the key tables and glyphs it paints from, and
// whether the status bar has a message. The surface is built with it.
type helpHost interface {
	helpContext(width int) uihelp.Context
	statusShown() bool
	// takeForeground marks a key as the user moving on, as every key the
	// root routes does, which fences a completion started before it.
	takeForeground()
}

type helpLayout struct {
	content uihelp.Content
	width   int
	rows    int
}

func newHelpSurface(root helpHost, scope uihelp.Scope) *helpSurface {
	return &helpSurface{state: uihelp.New(scope), root: root}
}

// openHelp scopes the key map to Review when Review is underneath. The base
// is read here, to open, and never again.
func (m *Model) openHelp() {
	scope := uihelp.Global
	if m.base == baseReview {
		scope = uihelp.Review
	}
	m.push(newHelpSurface(m, scope))
}

func (s *helpSurface) layout(h surfaceHost) helpLayout {
	cols, rows := h.size()
	width := uihelp.CardWidth(cols)
	inner := cardInnerWidth(width)
	content := s.state.Content(s.root.helpContext(inner))
	room := rows - 5 - lipgloss.Height(legendInline(content.Hints, inner)) - len(content.Head)
	if s.root.statusShown() {
		room -= 2
	}
	return helpLayout{content: content, width: width, rows: max(room, 1)}
}

func (s *helpSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	s.root.takeForeground()
	return true, s.step(h, msg)
}

func (s *helpSurface) step(h surfaceHost, msg tea.KeyMsg) tea.Cmd {
	layout := s.layout(h)
	switch s.state.Update(msg, uihelp.Viewport{Rows: layout.rows, Lines: len(layout.content.Lines)}) {
	case uihelp.OpenDocs:
		return openLink(docsURL)
	case uihelp.Close:
		h.pop()
	}
	return nil
}

func (s *helpSurface) Click(_ surfaceHost, line int) tea.Cmd {
	s.state.Click(line)
	return nil
}

func (s *helpSurface) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	return s.step(h, arrow)
}

func (s *helpSurface) View(h surfaceHost, _ string) string {
	layout := s.layout(h)
	lines := append([]string(nil), layout.content.Head...)
	lines = append(lines, presentation.Window(
		layout.content.Lines,
		layout.rows,
		layout.content.Offset,
		subtleStyle(),
	)...)
	return h.cardSized(layout.width, layout.content.Title, strings.Join(lines, "\n"), layout.content.Hints)
}

// Footer is empty: the card names its own keys on its bottom edge.
func (s *helpSurface) Footer() [][2]string { return nil }

func (s *helpSurface) Owner() inputOwner { return ownerManager }

func (s *helpSurface) Dismissible() bool { return true }

// Leave has nothing to undo. Review stays the mode underneath, so its loader
// never stopped while the card covered it.
func (s *helpSurface) Leave(surfaceHost) {}
