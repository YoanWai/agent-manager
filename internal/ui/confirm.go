package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A destructive answer is worth a dialog rather than a line of status text:
// the question sits in the middle of the frame, its target is spelled out,
// and the safe answer is the one already under the cursor.

// confirmTitle names the dialog after the act it is about to commit.
func (m *Model) confirmTitle() string {
	subject := "session"
	if m.confirm.isGroup {
		subject = "group"
	}
	switch m.confirm.action {
	case actionKill:
		return "✕ Kill " + subject
	case actionArchive:
		return "◇ Archive " + subject
	case actionRestore:
		return "◆ Restore " + subject
	default:
		return "⚠ Delete " + subject
	}
}

// confirmDestructive reports whether the pending answer takes something
// away, which decides whether the dialog reads as an alarm or as a move.
func (m *Model) confirmDestructive() bool {
	return m.confirm.action == actionKill || m.confirm.action == actionDelete
}

func (m *Model) viewConfirm() string {
	width := m.cardWidth()
	inner := cardInnerWidth(width)

	question, consequence := splitConfirmLabel(m.confirm.label)
	tone := lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	if m.confirmDestructive() {
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

	answer := "delete"
	switch m.confirm.action {
	case actionKill:
		answer = "kill"
	case actionArchive:
		answer = "archive"
	case actionRestore:
		answer = "restore"
	}
	hint := [][2]string{{"y/↵", answer}, {"n/esc", "cancel"}}
	return m.cardSized(width, m.confirmTitle(), strings.TrimRight(body.String(), "\n"), hint)
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
