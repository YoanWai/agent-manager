package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
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
func (m *Model) paintSettings(d *uisettings.State) string {
	frame := d.View(uisettings.ViewContext{
		GlyphMode:  m.prefs.glyphMode,
		Session:    m.services.keys,
		List:       m.services.listKeys,
		Version:    m.release.Version,
		Latest:     m.release.Latest,
		Applying:   m.release.Applying,
		Height:     m.layout.height,
		InnerWidth: uiframe.CardInnerWidth(m.cardWidth()),
		Chrome:     m.flexChromeRows,
		Styles:     settingsStyles(),
		TextInput:  uiframe.TextInputView,
		TextArea:   uiframe.TextAreaView,
	})
	switch frame.Card {
	case uisettings.Confirm:
		return m.confirmCard(frame.Title, frame.Question, frame.Consequence, frame.Destructive, frame.Answer)
	case uisettings.Fixed:
		return m.CardSized(m.cardWidth(), frame.Title, frame.Body, frame.Hints)
	}
	return m.CardSized(m.flexCardWidth(frame.Title, frame.Body, frame.Hints), frame.Title, frame.Body, frame.Hints)
}

func settingsStyles() uisettings.Styles {
	return uisettings.Styles{
		Value:      uitheme.ValueStyle(),
		Subtle:     uitheme.SubtleStyle(),
		Muted:      uitheme.MutedStyle(),
		Key:        uitheme.KeyStyle(),
		Annotation: uitheme.AnnotationStyle(),
		Marker:     lipgloss.NewStyle().Foreground(uitheme.ColorAccent()),
		Focused:    lipgloss.NewStyle().Foreground(uitheme.ColorAccent()).Bold(true),
		Action:     lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Bold(true),
		ActionText: lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()),
	}
}

func newSettingsSurface(m *Model, state uisettings.State) *uisettings.Surface {
	return uisettings.NewSurface(settingsRoot{m}, state)
}

// columnClicker is the optional capability of a surface whose card acts on
// where in a line a click lands: the root hands it the column too. It
// bridges until surface.Click carries the column for every card.
type columnClicker interface {
	ClickAt(h uisurface.Host, col, line int) tea.Cmd
}

var _ columnClicker = (*uisettings.Surface)(nil)
