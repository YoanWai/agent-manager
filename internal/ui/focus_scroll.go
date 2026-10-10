package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	tea "github.com/charmbracelet/bubbletea"
)

type focusScrollMsg struct {
	sessID  string
	offset  int
	rows    int
	preview string
	ok      bool
}

func hexBytes(s string) string {
	codes := make([]string, 0, len(s))
	for i := 0; i < len(s); i++ {
		codes = append(codes, fmt.Sprintf("%02x", s[i]))
	}
	return strings.Join(codes, " ")
}

// guardedMouseCommand sends a report only while the pane still tracks the
// mouse. The branch for a pane that has stopped tracking runs a command with
// no effect, so tmux answers the guard with guardedMouseReplies blocks
// whichever way it goes.
func guardedMouseCommand(sessID, report string) (string, []string) {
	target := tmux.PaneTarget(sessID)
	const condition = "#{mouse_any_flag}"
	const drop = "display-message -p"
	send := "send-keys -t " + target + " -H " + hexBytes(report)
	command := "if-shell -F -t " + target + " '" + condition + "' '" + send + "' '" + drop + "'"
	return command, []string{"if-shell", "-F", "-t", target, condition, send, drop}
}

// guardedMouseReplies is the reply blocks a guarded report costs on the
// control pipe: one for if-shell, one for the branch it ran.
const guardedMouseReplies = 2

func (m *Model) sendFocusReport(report string, echo bool) {
	sess, ok := m.selected()
	watch := m.focus.runtime.watch
	if !ok || watch == nil {
		return
	}
	command, args := guardedMouseCommand(sess.ID, report)
	m.dispatchInput(inputRequest{kind: inputMouse, session: sess, command: command, args: args, echo: echo})
}

func (m *Model) focusRegionCmd(sessID string, offset int) tea.Cmd {
	return m.focusRegionRequestCmd(uifocus.RegionRequest{
		SessionID: sessID,
		Offset:    offset,
		Rows:      m.focusPaneRows(),
	})
}

func (m *Model) focusRegionRequestCmd(request uifocus.RegionRequest) tea.Cmd {
	command := fmt.Sprintf(`capture-pane -p -e -t %s -S %d -E -`,
		tmux.PaneTarget(request.SessionID), -(request.Offset + request.Rows))
	watch := m.focus.runtime.watch
	return func() tea.Msg {
		if watch == nil {
			return focusScrollMsg{sessID: request.SessionID, offset: request.Offset, rows: request.Rows}
		}
		out, ok := watch.query(command)
		return focusScrollMsg{
			sessID:  request.SessionID,
			offset:  request.Offset,
			rows:    request.Rows,
			preview: bottomWindow(matchExecShape(out), request.Rows, request.Offset),
			ok:      ok,
		}
	}
}

func bottomWindow(capture string, rows, offset int) string {
	lines := strings.Split(strings.TrimSuffix(capture, "\n"), "\n")
	end := max(len(lines)-offset, 0)
	start := max(end-rows, 0)
	return strings.Join(lines[start:end], "\n") + "\n"
}
