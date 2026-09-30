package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (m *Model) groupSessionCount(path string) int {
	count := 0
	for _, sess := range m.listedAgents() {
		if inGroupSubtree(sess.Group, path) {
			count++
		}
	}
	return count
}

func (m *Model) renamingGroup(group string) bool {
	return m.mode == modeRename && m.rename.isGroup && m.rename.path == group
}

func (m *Model) renamingRow(entry treeRow) bool {
	if entry.isGroup {
		return m.renamingGroup(entry.group)
	}
	return m.mode == modeRename && !m.rename.isGroup && entry.sess.ID == m.rename.sessID
}

// renameRowInput renders the inline name editor in place of the row's
// label, keeping the row's glyph so the edit reads in context.
func (m *Model) renameRowInput(entry treeRow, width int) string {
	lead := subtleStyle.Render("▾")
	if !entry.isGroup {
		lead = m.sessionGlyph(entry.sess)
	}
	if fieldWidth := width - 4; fieldWidth >= 5 {
		m.rename.input.Width = fieldWidth
	}
	return lead + " " + textInputView(m.rename.input)
}

// groupStatusBreakdown renders "2 working · 1 waiting" for the subtree,
// each count tinted in its status color, skipping zero statuses.
func (m *Model) groupStatusBreakdown(group string) string {
	counts := m.groupStatusCounts(group)
	var parts []string
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Idle, status.Dead} {
		if counts[st] > 0 {
			// The count carries the state's color, the word stays quiet: a
			// rollup line should read as one texture, not as six labels
			// competing with the session names above it.
			parts = append(parts, lipgloss.NewStyle().Foreground(statusColor(st)).
				Render(fmt.Sprintf("%d", counts[st]))+subtleStyle.Render(" "+st))
		}
	}
	return strings.Join(parts, subtleStyle.Render(" · "))
}

func (m *Model) groupStatusCounts(group string) map[string]int {
	counts := map[string]int{}
	for _, sess := range m.listedAgents() {
		if inGroupSubtree(sess.Group, group) {
			counts[sess.Status]++
		}
	}
	return counts
}

// groupStatusGlyphs is the subtree's rollup written in dots: each state
// present as its own glyph and count, tinted its own color. A one-line
// group row has no width to spell the states out, and the glyphs are the
// same ones the sessions under it wear.
func (m *Model) groupStatusGlyphs(group string) string {
	counts := m.groupStatusCounts(group)
	var parts []string
	for _, st := range []string{status.Working, status.Waiting, status.Finished, status.Errored, status.Idle, status.Dead} {
		if counts[st] == 0 {
			continue
		}
		parts = append(parts, lipgloss.NewStyle().Foreground(statusColor(st)).
			Render(fmt.Sprintf("%s %d", statusGlyph(st), counts[st])))
	}
	return strings.Join(parts, subtleStyle.Render("  "))
}
