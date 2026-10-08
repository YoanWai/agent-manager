package rail

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	menuMinWidth = 24
	menuMaxWidth = 40
	menuChrome   = 4
	menuKeyGap   = 3
)

func (r *renderer) prepareMenu() {
	if !r.model.menu.active {
		return
	}
	menu := r.renderMenu()
	width := 0
	for _, line := range menu {
		width = max(width, ansi.StringWidth(line))
	}
	left := r.model.menu.anchorX + 1
	if left+width > r.ctx.TerminalWidth {
		left = max(r.model.menu.anchorX-width, 0)
	}
	top := r.model.menu.anchorY
	if top+len(menu) > r.ctx.TerminalHeight {
		top = max(r.ctx.TerminalHeight-len(menu), 0)
	}
	r.frame.Menu = menu
	r.frame.MenuRect = Rect{Left: left, Top: top, Width: width, Height: len(menu)}
}

func (r renderer) renderMenu() []string {
	glyphs := make([]string, len(r.model.menu.items))
	need := menuMinWidth
	for i, item := range r.model.menu.items {
		glyphs[i] = r.menuGlyph(item.action)
		need = max(need, ansi.StringWidth(item.label)+ansi.StringWidth(glyphs[i])+menuChrome+menuKeyGap)
	}
	width := min(need, menuMaxWidth, max(r.ctx.TerminalWidth, menuMinWidth))
	inner := width - menuChrome
	border := lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Border))
	edge := border.Render("│")
	background := mix(r.ctx.Theme.Bg, r.ctx.Theme.Surface, 0.35)
	title := ansi.Truncate(r.model.menu.title, max(width-8, 1), "…")
	rows := []string{paint(cardTitleRow(width, title, border), width, background)}
	for i, item := range r.model.menu.items {
		if item.label == "" {
			rows = append(rows, paint(border.Render("├"+strings.Repeat("─", width-2)+"┤"), width, background))
			continue
		}
		glyph := glyphs[i]
		gap := strings.Repeat(" ", max(inner-ansi.StringWidth(item.label)-ansi.StringWidth(glyph), 1))
		var line string
		if i == r.model.menu.index {
			ink := lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Bg)).Background(lipgloss.Color(r.ctx.Theme.Accent)).Bold(true)
			if item.danger {
				ink = ink.Background(lipgloss.Color(r.ctx.Theme.Errored))
			}
			line = ink.Render(" " + item.label + gap + glyph + " ")
		} else {
			ink := lipgloss.NewStyle().Foreground(lipgloss.Color(r.ctx.Theme.Text))
			if item.danger {
				ink = ink.Foreground(lipgloss.Color(r.ctx.Theme.Errored))
			}
			line = " " + ink.Render(item.label) + gap + r.palette.subtle.Render(glyph) + " "
		}
		rows = append(rows, paint(edge+line+edge, width, background))
	}
	return append(rows, paint(border.Render("╰"+strings.Repeat("─", width-2)+"╯"), width, background))
}

func cardTitleRow(width int, title string, border lipgloss.Style) string {
	left := "╭─ "
	right := " " + strings.Repeat("─", max(width-ansi.StringWidth(left)-ansi.StringWidth(title)-2, 0)) + "╮"
	return border.Render(left) + title + border.Render(right)
}

func (r renderer) menuGlyph(action ActionKind) string {
	binding := ""
	switch action {
	case Attach:
		binding = keybind.Attach
	case NewSession:
		binding = keybind.NewSession
	case NewGroup:
		binding = keybind.NewGroup
	case NewConnection:
		binding = keybind.NewConnection
	case Fork:
		binding = keybind.Fork
	case Revive:
		binding = keybind.Revive
	case Restart:
		binding = keybind.Restart
	case Kill:
		binding = keybind.Kill
	case Archive:
		binding = keybind.Archive
	case Restore:
		binding = keybind.Restore
	case Delete:
		binding = keybind.Delete
	case Prompt:
		binding = keybind.Prompt
	case CopyReply:
		binding = keybind.CopyReply
	case NewTerminal:
		binding = keybind.Terminal
	case OpenEditor:
		binding = keybind.Editor
	case RenameAction:
		binding = keybind.Rename
	case MoveToGroup:
		binding = keybind.Move
	case OpenReview:
		binding = keybind.Review
	case CancelEnd:
		binding = keybind.CancelEnd
	}
	if binding == "" {
		return ""
	}
	return r.ctx.ListKeys.Binding(binding).Glyph("/")
}
