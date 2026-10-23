package ui

import (
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// settingsRoot is the root as the Settings card reads it.
type settingsRoot struct{ *Model }

func (r settingsRoot) Context() uisettings.Context { return r.settingsContext() }

func (r settingsRoot) Apply(d *uisettings.State, result uisettings.Result) (tea.Cmd, bool) {
	return r.applySettingsResult(d, result)
}

func (r settingsRoot) Leave(d *uisettings.State) { r.leaveSettings(d) }

func (r settingsRoot) Paint(d *uisettings.State) string { return r.paintSettings(d) }

// paintSettings frames the dialog in the root's card chrome.
func (m *Model) paintSettings(d *settingsState) string {
	frame := d.View(uisettings.ViewContext{
		GlyphMode:  m.prefs.glyphMode,
		Session:    m.services.keys,
		List:       m.services.listKeys,
		Version:    m.update.version,
		Latest:     m.update.latest,
		Applying:   m.update.applying,
		Height:     m.layout.height,
		InnerWidth: cardInnerWidth(m.cardWidth()),
		Chrome:     m.flexChromeRows,
		Styles:     settingsStyles(),
		TextInput:  textInputView,
		TextArea:   textAreaView,
	})
	switch frame.Card {
	case uisettings.Confirm:
		return m.confirmCard(frame.Title, frame.Question, frame.Consequence, frame.Destructive, frame.Answer)
	case uisettings.Fixed:
		return m.card(frame.Title, frame.Body, frame.Hints)
	}
	return m.cardFlex(frame.Title, frame.Body, frame.Hints)
}

func settingsStyles() uisettings.Styles {
	return uisettings.Styles{
		Value:      valueStyle(),
		Subtle:     subtleStyle(),
		Muted:      mutedStyle(),
		Key:        keyStyle(),
		Annotation: annotationStyle(),
		Marker:     lipgloss.NewStyle().Foreground(colorAccent()),
		Focused:    lipgloss.NewStyle().Foreground(colorAccent()).Bold(true),
		Action:     lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true),
		ActionText: lipgloss.NewStyle().Foreground(colorAccent2()),
	}
}

func newSettingsSurface(m *Model, state settingsState) *uisettings.Surface {
	return uisettings.NewSurface(settingsRoot{m}, state)
}

// columnClicker is the optional capability of a surface whose card acts on
// where in a line a click lands: the root hands it the column too. It
// bridges until surface.Click carries the column for every card.
type columnClicker interface {
	ClickAt(h surfaceHost, col, line int) tea.Cmd
}

var _ columnClicker = (*uisettings.Surface)(nil)
