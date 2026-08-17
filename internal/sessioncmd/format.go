package sessioncmd

import (
	"fmt"
	"strings"
)

// The CLI subcommands and the MCP tools describe the same workspace to the
// same agents, so the sentences live beside the types both fronts return.

func groupLabel(group string) string {
	if group == "" {
		return "root"
	}
	return group
}

func FormatTerminal(terminal Terminal) string {
	return fmt.Sprintf("%s (id %s) in %s at %s", terminal.Name, terminal.ID, groupLabel(terminal.Group), terminal.Directory)
}

func FormatTerminalList(terminals []Terminal) string {
	if len(terminals) == 0 {
		return "no managed terminals"
	}
	lines := make([]string, 0, len(terminals))
	for _, terminal := range terminals {
		lines = append(lines, fmt.Sprintf("- %s; status=%s; running=%t", FormatTerminal(terminal), terminal.Status, terminal.Running))
	}
	return strings.Join(lines, "\n")
}

func FormatTerminalInput(input TerminalInput) string {
	return fmt.Sprintf("sent %s to terminal %s", input.Sent, input.TerminalID)
}

func FormatTerminalScreen(screen TerminalScreen) string {
	if screen.Output == "" {
		return "terminal screen is empty"
	}
	return screen.Output
}

func FormatSession(session Session) string {
	line := fmt.Sprintf("%s (id %s) running %s in %s at %s", session.Name, session.ID, session.Tool, groupLabel(session.Group), session.Directory)
	if session.Branch != "" {
		line += " on branch " + session.Branch
	}
	return line
}

func FormatSessionList(sessions []Session) string {
	if len(sessions) == 0 {
		return "no agent sessions"
	}
	lines := make([]string, 0, len(sessions))
	for _, session := range sessions {
		line := fmt.Sprintf("- %s; status=%s; running=%t", FormatSession(session), session.Status, session.Running)
		if session.Archived {
			line += "; archived"
		}
		if session.Self {
			line += "; this session"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// FormatArchiveState reads the verb off the row the call returned, so
// neither front says "archived" about a row that came back active.
func FormatArchiveState(session Session) string {
	if session.Archived {
		return "archived " + FormatSession(session)
	}
	return "restored " + FormatSession(session)
}

func FormatSessionScreen(screen SessionScreen) string {
	if screen.Output == "" {
		return "session screen is empty"
	}
	return screen.Output
}

func FormatSendResult(result SendResult, targetID string) string {
	text := fmt.Sprintf("queued message %d for session %s at position %d", result.MessageID, targetID, result.QueuePosition)
	if !result.ManagerAwake {
		text += "; Agent Manager is not running, so it waits until the user opens it"
	}
	return text
}

func FormatMessageState(state MessageState) string {
	text := fmt.Sprintf("message %d to session %s is %s", state.MessageID, state.SessionID, state.State)
	if state.DeliveredAt != "" {
		text += " (delivered " + state.DeliveredAt + ")"
	}
	if state.Reason != "" {
		text += ": " + state.Reason
	}
	return text
}

func FormatWaitResult(result WaitResult) string {
	text := fmt.Sprintf("%s is %s after %s", FormatSession(result.Session), result.Session.Status, result.Waited)
	switch result.Outcome {
	case WaitTimedOut:
		text = "timed out: " + text
	case WaitDied:
		text = "the session died before reaching any awaited state: " + text
	}
	if !result.ManagerAwake {
		text += "; Agent Manager is not running, so this status is the last one it recorded"
	}
	return text
}

func FormatTask(task Task) string {
	line := fmt.Sprintf("%s (%s) [%s]", task.Title, task.ID, task.State)
	if task.OwnerName != "" {
		line += " held by " + task.OwnerName
	}
	if task.Blocked {
		line += " blocked on " + strings.Join(task.BlockedBy, ", ")
	}
	return line
}

func FormatTaskList(tasks []Task) string {
	if len(tasks) == 0 {
		return "no tasks on the shared list"
	}
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		line := "- " + FormatTask(task)
		if task.Mine {
			line += "; yours"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func FormatReservation(reservation Reservation) string {
	line := fmt.Sprintf("%s (%s) held by %s for %s", reservation.Pattern, reservation.Mode, reservation.Holder, reservation.ExpiresIn)
	if reservation.Note != "" {
		line += ": " + reservation.Note
	}
	return line
}

func FormatReservations(reservations []Reservation) string {
	if len(reservations) == 0 {
		return "no files are reserved"
	}
	lines := make([]string, 0, len(reservations))
	for _, reservation := range reservations {
		line := "- " + FormatReservation(reservation)
		if reservation.Mine {
			line += "; yours"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func FormatReserveResult(result ReserveResult) string {
	lines := make([]string, 0, len(result.Reserved)+len(result.Conflicts)+1)
	for _, reservation := range result.Reserved {
		lines = append(lines, "reserved "+reservation.Pattern)
	}
	if len(result.Conflicts) > 0 {
		lines = append(lines, "conflicts with leases already held; message the holder before editing:")
		for _, conflict := range result.Conflicts {
			lines = append(lines, "- "+FormatReservation(conflict))
		}
	}
	return strings.Join(lines, "\n")
}

func FormatReleased(count int) string {
	return fmt.Sprintf("released %d reservation(s)", count)
}

func FormatGroupList(groups []Group) string {
	if len(groups) == 0 {
		return "no groups; sessions live in the root"
	}
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		line := fmt.Sprintf("- %s; sessions=%d", group.Path, group.Sessions)
		if group.Directory != "" {
			line += "; directory=" + group.Directory
		}
		if group.Worktree != "" {
			line += "; worktree=" + group.Worktree
		}
		if group.Archived {
			line += "; archived"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
