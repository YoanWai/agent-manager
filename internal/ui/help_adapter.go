package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpHost is the root as the key map reads it.
type helpHost struct{ *Model }

func (h helpHost) KeyTables() uihelp.Tables {
	return uihelp.Tables{Session: h.services.keys, List: h.services.listKeys, ArrowStep: h.prefs.arrowStep}
}

func (h helpHost) Glyphs() uihelp.Glyphs {
	return uihelp.Glyphs{
		Mode:             h.prefs.glyphMode,
		RowMenu:          uirail.MenuGlyph,
		Reorder:          h.glyphs().ReorderGrip,
		AfterTurnArchive: h.afterTurnGlyph(store.AfterTurnArchive),
		AfterTurnKill:    h.afterTurnGlyph(store.AfterTurnKill),
	}
}

func (h helpHost) QuickKeys() uihelp.QuickKeys {
	return uihelp.QuickKeys{Model: uiquick.ModelKey, Effort: uiquick.EffortKey, Profile: uiquick.ProfileKey, Env: uiquick.EnvKey}
}

func (h helpHost) ReviewRows() [][2]string {
	return uireview.HelpRows(h.services.listKeys.Binding(keybind.Help), h.prefs.glyphMode)
}

func (h helpHost) CursorMarker() string { return uiframe.CursorMarker }

func (h helpHost) Context(width int) uihelp.Context { return uihelp.ContextFor(h, width) }

func (h helpHost) Styles() uihelp.Styles { return uihelp.CurrentStyles() }

func (h helpHost) HintRows(hints [][2]string, width int) int {
	// One untitled run, for the foot of a modal card where the tier's
	// subject is the card's own title.
	return lipgloss.Height(uiframe.LegendInline(hints, width, uiframe.LiveCap(h.prefs.glyphMode)))
}

func (h helpHost) StatusShown() bool { return h.errBar.text != "" }

func (h helpHost) TakeForeground() { h.takeForeground() }

func (h helpHost) OpenDocs() tea.Cmd { return openLink(docsURL) }

// openHelp scopes the key map to Review when Review is underneath. The base
// is read here, to open, and never again.
func (m *Model) openHelp() {
	scope := uihelp.Global
	if m.base == baseReview {
		scope = uihelp.Review
	}
	m.push(uihelp.NewSurface(helpHost{m}, scope))
}
