package ui

import (
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// reviewPicker is the repo, branch and base picker the r, b and B keys open
// over Review. It is a surface: the manager reads the keys while it is up,
// esc, ctrl+c and a click off the card close it, and a pick comes off the
// stack before the root acts on it, so the choice is checked against the
// review that is showing then, not the one the picker remembers.
type reviewPicker struct {
	picker uireview.Picker
	render uireview.RenderContext
	glyph  string
}

// newReviewPicker is the one way to build the surface, so a picker on the
// stack always paints with the styles it was opened under.
func newReviewPicker(rows []uireview.PickerRow, title string, kind uireview.PickerKind, current string, source uireview.PickerSource, storeRoot string, render uireview.RenderContext, glyph string) *reviewPicker {
	s := &reviewPicker{render: render, glyph: glyph}
	s.picker.Open(rows, title, kind, current, source, storeRoot)
	return s
}

// reviewPickedMsg carries a pick off the surface to the adapter, which
// checks the review it names is still the one open before it loads.
type reviewPickedMsg struct {
	choice    uireview.PickerChoice
	source    uireview.PickerSource
	storeRoot string
}

func (s *reviewPicker) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.apply(h, s.picker.Key(msg))
}

func (s *reviewPicker) Click(h surfaceHost, line int) tea.Cmd {
	return s.apply(h, s.picker.Click(line))
}

func (s *reviewPicker) Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd {
	return s.apply(h, s.picker.Key(arrow))
}

func (s *reviewPicker) View(h surfaceHost, _ string) string {
	cols, rows := h.Size()
	width := cardWidthFor(cols)
	body := s.picker.Body(s.render, cardInnerWidth(width), rows)
	return h.CardSized(width, s.glyph+" "+s.picker.Title(), body, s.picker.Hints())
}

func (s *reviewPicker) Footer() [][2]string { return s.picker.Hints() }

func (s *reviewPicker) Owner() inputOwner { return ownerManager }

// Dismissible: the filter is a search, not a draft, so a stray click closes
// the picker the way esc does.
func (s *reviewPicker) Dismissible() bool { return true }

func (s *reviewPicker) Leave(surfaceHost) {}

// apply takes the picker off the stack once it has closed itself, and sends
// a pick up to the adapter.
func (s *reviewPicker) apply(h surfaceHost, result uireview.PickerResult) tea.Cmd {
	if !result.Close {
		return nil
	}
	h.Pop()
	if result.Choice == nil {
		return nil
	}
	msg := reviewPickedMsg{choice: *result.Choice, source: s.picker.Source(), storeRoot: s.picker.StoreRoot()}
	return func() tea.Msg { return msg }
}
