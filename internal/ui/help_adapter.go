package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
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
		QuickKeys:    uihelp.QuickKeys{Model: quickModelKey, Effort: quickEffortKey, Profile: quickProfileKey},
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

// statusShown is whether the status bar has a message, which the card makes
// room for.
func (m *Model) statusShown() bool { return m.errBar.text != "" }
