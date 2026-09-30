package ui

import (
	"strings"

	uihelp "github.com/YoanWai/agent-manager/internal/ui/help"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type helpLayout struct {
	dialog  dialogRenderContext
	content uihelp.Content
	width   int
	rows    int
}

func (m *Model) openHelp() {
	m.helpReturnMode = m.mode
	scope := uihelp.Global
	if m.mode == modeDiff {
		scope = uihelp.Review
	}
	m.help = uihelp.New(scope)
	m.mode = modeHelp
}

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	layout := m.helpLayout()
	viewport := uihelp.Viewport{Rows: layout.rows, Lines: len(layout.content.Lines)}
	switch m.help.Update(msg, viewport) {
	case uihelp.Quit:
		return m, tea.Quit
	case uihelp.Close:
		m.mode = m.helpReturnMode
		m.helpReturnMode = modeList
		return m, m.startStartupTick()
	default:
		return m, nil
	}
}

func (m *Model) helpContext(width int) uihelp.Context {
	return uihelp.Context{
		SessionKeys: m.services.keys,
		ListKeys:    m.services.listKeys,
		ArrowStep:   m.prefs.arrowStep,
		Width:       width,
		Glyphs: uihelp.Glyphs{
			RowMenu: uirail.MenuGlyph,
			Reorder: uirail.ReorderGlyph,
		},
		CursorMarker: cursorAnchorMarker,
		Styles: uihelp.Styles{
			Section: sectionStyle,
			Rule:    lipgloss.NewStyle().Foreground(colorBorder),
			Key:     keyStyle,
			Muted:   mutedStyle,
			Subtle:  subtleStyle,
			Value:   valueStyle,
			Match:   lipgloss.NewStyle().Foreground(colorBright).Bold(true),
			Cursor:  lipgloss.NewStyle().Foreground(colorAccent),
		},
	}
}

func (m *Model) helpLayout() helpLayout {
	dialog := m.dialogRenderContext()
	width := uihelp.CardWidth(dialog.width)
	inner := cardInnerWidth(width)
	content := m.help.Content(m.helpContext(inner))
	rows := dialog.height - 5 - lipgloss.Height(legendInline(content.Hints, inner)) - len(content.Head)
	if dialog.status != "" {
		rows -= 2
	}
	return helpLayout{
		dialog:  dialog,
		content: content,
		width:   width,
		rows:    max(rows, 1),
	}
}

func (m *Model) viewHelp() string {
	layout := m.helpLayout()
	lines := append([]string(nil), layout.content.Head...)
	lines = append(lines, presentation.Window(
		layout.content.Lines,
		layout.rows,
		layout.content.Offset,
		subtleStyle,
	)...)
	return renderDialog(
		layout.dialog,
		layout.width,
		layout.content.Title,
		strings.Join(lines, "\n"),
		layout.content.Hints,
	)
}
