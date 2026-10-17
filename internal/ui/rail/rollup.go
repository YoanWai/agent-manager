package rail

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// rollupStates is the order a group rollup lists its states in.
var rollupStates = []string{"starting", "working", "waiting", "finished", "errored", "idle", "dead"}

// GroupSessionCount counts the copied sessions in group's subtree.
func GroupSessionCount(sessions []Session, group string) int {
	count := 0
	for _, session := range sessions {
		if InGroupSubtree(session.Group, group) {
			count++
		}
	}
	return count
}

// GroupStatusCounts counts the copied sessions in group's subtree by status.
func GroupStatusCounts(sessions []Session, group string) map[string]int {
	counts := map[string]int{}
	for _, session := range sessions {
		if InGroupSubtree(session.Group, group) {
			counts[session.Status]++
		}
	}
	return counts
}

// GroupStatusBreakdown renders "2 working · 1 waiting" for the subtree,
// each count tinted in its status color, skipping zero statuses.
func GroupStatusBreakdown(theme Theme, sessions []Session, group string) string {
	p := newPalette(theme)
	counts := GroupStatusCounts(sessions, group)
	var parts []string
	for _, state := range rollupStates {
		if counts[state] > 0 {
			// The count carries the state's color, the word stays quiet: a
			// rollup line should read as one texture, not as six labels
			// competing with the session names above it.
			parts = append(parts, lipgloss.NewStyle().Foreground(p.statusColor(state)).
				Render(fmt.Sprintf("%d", counts[state]))+p.subtle.Render(" "+state))
		}
	}
	return strings.Join(parts, p.subtle.Render(" · "))
}
