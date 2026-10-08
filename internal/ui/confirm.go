package ui

import (
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

// A destructive answer is worth a dialog rather than a line of status text:
// the question sits in the middle of the frame, its target is spelled out,
// and the safe answer is the one already under the cursor.

// confirmDialog asks before a captured lifecycle request runs. It owns the
// pending target, the y/n keys and the card's wording. The lifecycle
// adapters fill its target and open it; the root switches modes and runs
// the confirmed request on the effect lane.
type confirmDialog struct{ confirmTarget }

// confirmHost is what the confirm dialog reaches on the root: the shared
// confirm card, quitting, mode changes and the lifecycle effect lane.
type confirmHost interface {
	confirmCard(title, question, consequence string, destructive bool, answer string) string
	quit() tea.Cmd
	setMode(next mode)
	queueLifecycle(target confirmTarget, allowLive bool, emptyNotice string)
	removeConnection(name string)
	nextEffectCmd() tea.Cmd
}

var _ confirmHost = (*Model)(nil)

// title names the dialog after the act it is about to commit.
func (d *confirmDialog) title() string {
	subject := "session"
	if d.isGroup {
		subject = "group"
	}
	switch d.action {
	case actionKill:
		return "✕ Kill " + subject
	case actionArchive:
		return "◇ Archive " + subject
	case actionRestore:
		return "◆ Restore " + subject
	case actionRestart:
		return "↻ Restart " + subject
	case actionRevive:
		return "◆ Revive " + subject
	case actionRemoveConnection:
		return "⚠ Remove connection"
	default:
		return "⚠ Delete " + subject
	}
}

// destructive reports whether the pending answer takes something
// away, which decides whether the dialog reads as an alarm or as a move.
func (d *confirmDialog) destructive() bool {
	return d.action == actionKill || d.action == actionDelete ||
		d.action == actionRestart || d.action == actionArchive || d.action == actionRemoveConnection
}

func (d *confirmDialog) view(h confirmHost) string {
	question, consequence := splitConfirmLabel(d.label)
	answer := "delete"
	switch d.action {
	case actionKill:
		answer = "kill"
	case actionArchive:
		answer = "archive"
	case actionRestore:
		answer = "restore"
	case actionRestart:
		answer = "restart"
	case actionRevive:
		answer = "revive"
	case actionRemoveConnection:
		answer = "remove"
	}
	return h.confirmCard(d.title(), question, consequence, d.destructive(), answer)
}

func (m *Model) confirmCard(title, question, consequence string, destructive bool, answer string) string {
	width := m.cardWidth()
	inner := cardInnerWidth(width)
	tone := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	if destructive {
		tone = errStyle
	}

	var body strings.Builder
	for _, line := range strings.Split(ansi.Wordwrap(question, inner, "-"), "\n") {
		body.WriteString(tone.Render(line) + "\n")
	}
	if consequence != "" {
		body.WriteString("\n")
		for _, line := range strings.Split(ansi.Wordwrap(consequence, inner, "-"), "\n") {
			body.WriteString(mutedStyle.Render(line) + "\n")
		}
	}
	hint := [][2]string{{"y/↵", answer}, {"n/esc", "cancel"}}
	return m.cardSized(width, title, strings.TrimRight(body.String(), "\n"), hint)
}

// splitConfirmLabel cuts a confirm sentence into the question the dialog
// asks and the consequence it sets beneath, so the two read at different
// weights instead of as one run of prose.
func splitConfirmLabel(label string) (string, string) {
	if mark := strings.Index(label, "? "); mark >= 0 {
		return label[:mark+1], strings.TrimSpace(label[mark+2:])
	}
	return label, ""
}

// confirmTarget.action values; the zero value means delete.
const (
	actionDelete  = ""
	actionArchive = "archive"
	actionRestore = "restore"
	actionKill    = "kill"
	actionRestart = "restart"
	actionRevive  = "revive"
	// actionRemoveConnection takes an SSH connection off the list; the
	// target names it and holds no sessions.
	actionRemoveConnection = "remove connection"
)

type confirmTarget struct {
	isGroup bool
	// archivedOnly marks a group delete issued from the archived view,
	// which clears the group's archive instead of the group itself.
	archivedOnly bool
	path         string
	label        string
	sessions     []store.Session
	action       string
	batch        bool
	selection    lifecycleSelection
	connection   string
}

type lifecycleSelectionKind uint8

const (
	lifecycleSelectionNone lifecycleSelectionKind = iota
	lifecycleSelectionSession
	lifecycleSelectionGroup
)

type lifecycleSelection struct {
	kind         lifecycleSelectionKind
	rootID       string
	archivedOnly bool
}
