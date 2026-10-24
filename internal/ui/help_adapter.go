package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) helpContext(width int) uihelp.Context {
	return uihelp.Context{
		SessionKeys: m.services.keys,
		ListKeys:    m.services.listKeys,
		ArrowStep:   m.prefs.arrowStep,
		Width:       width,
		Glyphs: uihelp.Glyphs{
			Mode:             m.prefs.glyphMode,
			RowMenu:          uirail.MenuGlyph,
			Reorder:          m.glyphs().ReorderGrip,
			AfterTurnArchive: m.afterTurnGlyph(store.AfterTurnArchive),
			AfterTurnKill:    m.afterTurnGlyph(store.AfterTurnKill),
		},
		QuickKeys:    uihelp.QuickKeys{Model: uiquick.ModelKey, Effort: uiquick.EffortKey, Profile: uiquick.ProfileKey},
		CursorMarker: cursorAnchorMarker,
		ReviewRows:   uireview.HelpRows(),
		Styles: uihelp.Styles{
			Section: sectionStyle(),
			Rule:    lipgloss.NewStyle().Foreground(colorBorder()),
			Key:     keyStyle(),
			Muted:   mutedStyle(),
			Subtle:  subtleStyle(),
			Value:   valueStyle(),
			Match:   lipgloss.NewStyle().Foreground(colorBright()).Bold(true),
			Cursor:  lipgloss.NewStyle().Foreground(colorAccent()),
		},
	}
}

// helpRoot is the root as the key map reads it.
type helpRoot struct{ *Model }

func (r helpRoot) Context(width int) uihelp.Context { return r.helpContext(width) }

func (r helpRoot) InnerWidth(width int) int { return cardInnerWidth(width) }

func (r helpRoot) HintRows(hints [][2]string, width int) int {
	return lipgloss.Height(legendInline(hints, width))
}

func (r helpRoot) StatusShown() bool { return r.errBar.text != "" }

func (r helpRoot) TakeForeground() { r.takeForeground() }

func (r helpRoot) OpenDocs() tea.Cmd { return openLink(docsURL) }

// openHelp scopes the key map to Review when Review is underneath. The base
// is read here, to open, and never again.
func (m *Model) openHelp() {
	scope := uihelp.Global
	if m.base == baseReview {
		scope = uihelp.Review
	}
	m.push(uihelp.NewSurface(helpRoot{m}, scope))
}
