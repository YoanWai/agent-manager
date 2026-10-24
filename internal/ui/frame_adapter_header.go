package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// viewHeaderRows is the full-width band over both columns: the wordmark
// on the left, and the richest reading of the fleet that fits set against
// the right edge (scope and rollup, then a compact rollup, then the scope
// alone).
func (m *Model) viewHeaderRows() []string {
	if m.headerRows() == 0 {
		return nil
	}
	left := m.viewBanner()[0]
	if tag := m.updateTag(); tag != "" {
		left += uitheme.SubtleStyle().Render("  ") + tag
	}
	sep := uitheme.SubtleStyle().Render("   ")
	scope := m.headerScope()
	agents := m.headerAgents()
	readings := []string{
		uiframe.JoinHeaderPieces(sep, scope, m.viewStatusCounts(false), agents),
		uiframe.JoinHeaderPieces(sep, scope, m.viewStatusCounts(true), agents),
		uiframe.JoinHeaderPieces(sep, scope, m.viewStatusCounts(true), ""),
		scope,
		"",
	}
	return []string{uiframe.HeaderRow(left, readings, m.layout.width, uiframe.RailGutter)}
}

func (m *Model) updateTag() string {
	if m.release.Latest == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(uitheme.ColorAccent()).Render(m.glyphs().UpdateGlyph + " " + m.release.Latest + " available")
}

// titleTopRowWithUpdate keeps the update tag on the top edge when the header
// that would carry it is hidden.
func (m *Model) titleTopRowWithUpdate(frame string) string {
	tag := m.updateTag()
	if !m.prefs.hideHeader || tag == "" {
		return frame
	}
	title := uitheme.Paint(" "+tag+" ", ansi.StringWidth(tag)+2, uitheme.BackdropHex())
	left := m.layout.width - ansi.StringWidth(title) - uiframe.RailGutter
	if left < uiframe.RailGutter {
		return frame
	}
	rows := strings.SplitN(frame, "\n", 2)
	rows[0] = spliceAtColumn(rows[0], title, left)
	return strings.Join(rows, "\n")
}

// headerScope names what the list is showing. The count is of the same
// agents the rollup beside it breaks down, so the two lines always add up;
// counting painted rows instead would drop everything folded inside a
// collapsed group. Shells are left to their own block.
func (m *Model) headerScope() string {
	count := len(m.listedAgents())
	label := " agents"
	if count == 1 {
		label = " agent"
	}
	scope := uitheme.SubtleStyle().Render(" · active")
	if m.rail.ShowArchived() {
		scope = uitheme.SubtleStyle().Render(" · archived")
	}
	return uitheme.ValueStyle().Render(fmt.Sprintf("%d", count)) + uitheme.SubtleStyle().Render(label) + scope
}

// headerAgents is the fleet's process cost as shares of this machine,
// empty when nothing is running. RAM shows both percent and absolute size.
func (m *Model) headerAgents() string {
	if m.workspace.agents.count == 0 {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true).Render("agents total usage:")
	return title + " " +
		uitheme.LabelStyle().Render("cpu ") + uitheme.ValueStyle().Render(fmt.Sprintf("%.0f%%", m.workspace.agents.cpu)) +
		uitheme.SubtleStyle().Render(" · ") + uitheme.LabelStyle().Render("ram ") +
		uitheme.ValueStyle().Render(fmt.Sprintf("%.0f%%", m.workspace.agents.ram)) +
		uitheme.SubtleStyle().Render(" · ") + uitheme.ValueStyle().Render(uiframe.HumanBytes(m.workspace.agents.rss))
}

// elsewhereNote marks a session this manager does not speak for: its pane
// runs on a tmux server this one does not talk to, or no server has claimed
// it and the store belongs to another manager. Its status is the last one
// that manager wrote, and nothing here refreshes or drives it.
func (m *Model) elsewhereNote(sess store.Session) string {
	if m.workspace.tmuxSocket == "" {
		return ""
	}
	if sess.TmuxSocket == "" && !m.workspace.leadingManager {
		return " · elsewhere"
	}
	if sess.TmuxSocket == "" || sess.TmuxSocket == m.workspace.tmuxSocket {
		return ""
	}
	return " · elsewhere"
}

// viewStatusCounts is the fleet-at-a-glance strip: a tinted dot and count
// per state present among the listed agents.
func (m *Model) viewStatusCounts(compact bool) string {
	counts := map[string]int{}
	for _, sess := range m.listedAgents() {
		counts[sess.Status]++
	}
	var parts []string
	for _, st := range []string{status.Waiting, status.Working, status.Finished, status.Idle, status.Errored, status.Dead} {
		if counts[st] == 0 {
			continue
		}
		dot := lipgloss.NewStyle().Foreground(statusColor(st)).Render(m.glyphs().StatusGlyph(st))
		label := fmt.Sprintf(" %d %s", counts[st], st)
		if compact {
			label = fmt.Sprintf(" %d", counts[st])
		}
		parts = append(parts, dot+uitheme.SubtleStyle().Render(label))
	}
	return strings.Join(parts, uitheme.SubtleStyle().Render("  "))
}
