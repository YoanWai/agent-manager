package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
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
		CursorMarker: uiframe.CursorMarker,
		ReviewRows:   uireview.HelpRows(),
		Styles: uihelp.Styles{
			Section: uitheme.SectionStyle(),
			Rule:    lipgloss.NewStyle().Foreground(uitheme.ColorBorder()),
			Key:     uitheme.KeyStyle(),
			Muted:   uitheme.MutedStyle(),
			Subtle:  uitheme.SubtleStyle(),
			Value:   uitheme.ValueStyle(),
			Match:   lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true),
			Cursor:  lipgloss.NewStyle().Foreground(uitheme.ColorAccent()),
		},
	}
}

// helpRoot is the root as the key map reads it.
type helpRoot struct{ *Model }

func (r helpRoot) Context(width int) uihelp.Context { return r.helpContext(width) }

func (r helpRoot) InnerWidth(width int) int { return uiframe.CardInnerWidth(width) }

func (r helpRoot) HintRows(hints [][2]string, width int) int {
	// One untitled run, for the foot of a modal card where the tier's
	// subject is the card's own title.
	return lipgloss.Height(uiframe.LegendInline(hints, width, uiframe.LiveCap()))
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
