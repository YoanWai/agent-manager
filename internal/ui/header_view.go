package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
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
	if m.update.latest != "" {
		left += subtleStyle.Render("  ") +
			lipgloss.NewStyle().Foreground(colorAccent).Render("↑ "+m.update.latest+" available")
	}
	sep := subtleStyle.Render("   ")
	scope := m.headerScope()
	agents := m.headerAgents()
	for _, right := range []string{
		joinHeaderPieces(sep, scope, m.viewStatusCounts(false), agents),
		joinHeaderPieces(sep, scope, m.viewStatusCounts(true), agents),
		joinHeaderPieces(sep, scope, m.viewStatusCounts(true), ""),
		scope,
		"",
	} {
		gap := m.width - railGutter - ansi.StringWidth(left) - ansi.StringWidth(right)
		if right == "" || gap < 2 {
			continue
		}
		return []string{left + strings.Repeat(" ", gap) + right + strings.Repeat(" ", railGutter)}
	}
	return []string{left}
}

// joinHeaderPieces joins the header's non-empty readings with a separator.
func joinHeaderPieces(sep string, pieces ...string) string {
	kept := pieces[:0:0]
	for _, piece := range pieces {
		if piece != "" {
			kept = append(kept, piece)
		}
	}
	return strings.Join(kept, sep)
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
	scope := subtleStyle.Render(" · active")
	if m.rail.showArchived {
		scope = subtleStyle.Render(" · archived")
	}
	return valueStyle.Render(fmt.Sprintf("%d", count)) + subtleStyle.Render(label) + scope
}

// headerAgents is the fleet's process cost as shares of this machine,
// empty when nothing is running. RAM shows both percent and absolute size.
func (m *Model) headerAgents() string {
	if m.workspace.agents.count == 0 {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render("agents total usage:")
	return title + " " +
		labelStyle.Render("cpu ") + valueStyle.Render(fmt.Sprintf("%.0f%%", m.workspace.agents.cpu)) +
		subtleStyle.Render(" · ") + labelStyle.Render("ram ") +
		valueStyle.Render(fmt.Sprintf("%.0f%%", m.workspace.agents.ram)) +
		subtleStyle.Render(" · ") + valueStyle.Render(humanBytes(m.workspace.agents.rss))
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
		dot := lipgloss.NewStyle().Foreground(statusColor(st)).Render(statusGlyph(st))
		label := fmt.Sprintf(" %d %s", counts[st], st)
		if compact {
			label = fmt.Sprintf(" %d", counts[st])
		}
		parts = append(parts, dot+subtleStyle.Render(label))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}
