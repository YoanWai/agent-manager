package review

import (
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// Surface is the repo, branch and base picker the r, b and B keys open over
// Review. It is a surface: the manager reads the keys while it is up, esc,
// ctrl+c and a click off the card close it, and a pick comes off the stack
// before the root acts on it, so the choice is checked against the review
// that is showing then, not the one the picker remembers.
type Surface struct {
	picker Picker
	render RenderContext
	glyph  string
	root   Root
}

// Root is what the picker reads on the root beyond surface.Host: the card
// geometry, so this card is the width every other card is.
type Root interface {
	CardWidth(cols int) int
	InnerWidth(width int) int
}

// PickedMsg carries a pick off the surface to the root's adapter, which
// checks the review it names is still the one open before it loads.
type PickedMsg struct {
	Choice    PickerChoice
	Source    PickerSource
	StoreRoot string
}

// NewSurface is the one way to build the surface, so a picker on the stack
// always paints with the styles it was opened under.
func NewSurface(root Root, rows []PickerRow, title string, kind PickerKind, current string, source PickerSource, storeRoot string, render RenderContext, glyph string) *Surface {
	s := &Surface{render: render, glyph: glyph, root: root}
	s.picker.Open(rows, title, kind, current, source, storeRoot)
	return s
}

func (s *Surface) Picker() *Picker { return &s.picker }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.apply(h, s.picker.Key(msg))
}

func (s *Surface) Click(h surface.Host, line int) tea.Cmd {
	return s.apply(h, s.picker.Click(line))
}

func (s *Surface) Wheel(h surface.Host, arrow tea.KeyMsg) tea.Cmd {
	return s.apply(h, s.picker.Key(arrow))
}

func (s *Surface) View(h surface.Host, _ string) string {
	cols, rows := h.Size()
	width := s.root.CardWidth(cols)
	body := s.picker.Body(s.render, s.root.InnerWidth(width), rows)
	return h.CardSized(width, s.glyph+" "+s.picker.Title(), body, s.picker.Hints(s.render.GlyphMode))
}

func (s *Surface) Footer() [][2]string { return s.picker.Hints(s.render.GlyphMode) }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

// Dismissible: the filter is a search, not a draft, so a stray click closes
// the picker the way esc does.
func (s *Surface) Dismissible() bool { return true }

func (s *Surface) Leave(surface.Host) {}

// apply takes the picker off the stack once it has closed itself, and sends
// a pick up to the adapter.
func (s *Surface) apply(h surface.Host, result PickerResult) tea.Cmd {
	if !result.Close {
		return nil
	}
	h.Pop()
	if result.Choice == nil {
		return nil
	}
	msg := PickedMsg{Choice: *result.Choice, Source: s.picker.Source(), StoreRoot: s.picker.StoreRoot()}
	return func() tea.Msg { return msg }
}
