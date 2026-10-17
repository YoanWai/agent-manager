package dialogs

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A destructive answer is worth a dialog rather than a line of status text:
// the question sits in the middle of the frame, its target is spelled out,
// and the safe answer is the one already under the cursor.

// ConfirmAction is the act a confirm asks about; the zero value is delete.
type ConfirmAction string

const (
	ConfirmDelete  ConfirmAction = ""
	ConfirmArchive ConfirmAction = "archive"
	ConfirmRestore ConfirmAction = "restore"
	ConfirmKill    ConfirmAction = "kill"
	ConfirmRestart ConfirmAction = "restart"
	ConfirmRevive  ConfirmAction = "revive"
)

// Confirm is the question a captured lifecycle request asks before it runs:
// the act, whether it covers a group, and the sentence naming its target.
// The root holds the request itself and runs it once the answer is yes.
type Confirm struct {
	Action ConfirmAction
	Group  bool
	Label  string
}

// ConfirmOutcome is what a key asks the root to do with the pending request.
type ConfirmOutcome uint8

const (
	ConfirmStay ConfirmOutcome = iota
	ConfirmCancel
	ConfirmAccept
	ConfirmQuit
)

// Key answers the question: y or enter runs the request, n or esc drops it.
func (c Confirm) Key(msg tea.KeyMsg) ConfirmOutcome {
	switch msg.String() {
	case "ctrl+c":
		return ConfirmQuit
	case "n", "esc":
		return ConfirmCancel
	case "y", "enter":
		return ConfirmAccept
	}
	return ConfirmStay
}

// Title names the dialog after the act it is about to commit.
func (c Confirm) Title() string {
	subject := "session"
	if c.Group {
		subject = "group"
	}
	switch c.Action {
	case ConfirmKill:
		return "✕ Kill " + subject
	case ConfirmArchive:
		return "◇ Archive " + subject
	case ConfirmRestore:
		return "◆ Restore " + subject
	case ConfirmRestart:
		return "↻ Restart " + subject
	case ConfirmRevive:
		return "◆ Revive " + subject
	default:
		return "⚠ Delete " + subject
	}
}

// Destructive reports whether the pending answer takes something
// away, which decides whether the dialog reads as an alarm or as a move.
func (c Confirm) Destructive() bool {
	return c.Action == ConfirmKill || c.Action == ConfirmDelete ||
		c.Action == ConfirmRestart || c.Action == ConfirmArchive
}

// Answer is the word the accept key carries in the hint.
func (c Confirm) Answer() string {
	switch c.Action {
	case ConfirmKill:
		return "kill"
	case ConfirmArchive:
		return "archive"
	case ConfirmRestore:
		return "restore"
	case ConfirmRestart:
		return "restart"
	case ConfirmRevive:
		return "revive"
	}
	return "delete"
}

// SplitLabel cuts a confirm sentence into the question the dialog
// asks and the consequence it sets beneath, so the two read at different
// weights instead of as one run of prose.
func SplitLabel(label string) (string, string) {
	if mark := strings.Index(label, "? "); mark >= 0 {
		return label[:mark+1], strings.TrimSpace(label[mark+2:])
	}
	return label, ""
}

// ConfirmBody sets a question in tone and its consequence muted beneath,
// both wrapped to the card's inner width.
func ConfirmBody(question, consequence string, inner int, tone, muted lipgloss.Style) string {
	var body strings.Builder
	for _, line := range strings.Split(ansi.Wordwrap(question, inner, "-"), "\n") {
		body.WriteString(tone.Render(line) + "\n")
	}
	if consequence != "" {
		body.WriteString("\n")
		for _, line := range strings.Split(ansi.Wordwrap(consequence, inner, "-"), "\n") {
			body.WriteString(muted.Render(line) + "\n")
		}
	}
	return strings.TrimRight(body.String(), "\n")
}

// ConfirmHints are the card's keys, the accept key named for the act.
func ConfirmHints(answer string) [][2]string {
	return [][2]string{{"y/↵", answer}, {"n/esc", "cancel"}}
}
