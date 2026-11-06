package sessioncmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
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
	if session.Environment != "" {
		line += " under environment " + session.Environment
	}
	if session.AgentSessionID != "" {
		line += " on conversation " + session.AgentSessionID
	}
	if session.Warning != "" {
		line += "\nwarning: " + session.Warning
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

// FormatAfterTurn tells the agent what happens to it, in the order it acts
// on: what is pending, and whether anything is running to carry it out.
func FormatAfterTurn(result AfterTurn) string {
	switch {
	case result.Canceled != "":
		return "canceled the pending " + result.Canceled + "; this session stays as it is when the turn ends"
	case result.Pending == "":
		return "no archive or kill was pending"
	}
	verb := "archived"
	if result.Pending == store.AfterTurnKill {
		verb = "killed"
	}
	text := "this session is " + verb + " once the current turn ends, so finish your reply now"
	if result.Replaced != "" {
		text += "; this replaces the pending " + result.Replaced
	}
	if !result.ManagerAwake {
		text += "; Agent Manager is not running, so it happens once the user opens it"
	}
	return text
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

func FormatMessageList(entries []MessageEntry) string {
	return formatMessageList(entries, time.Now())
}

func formatMessageList(entries []MessageEntry, now time.Time) string {
	if len(entries) == 0 {
		return "no messages in the log"
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		line := fmt.Sprintf("- %d %s -> %s %s %s", entry.MessageID, messageSender(entry), messageParty(entry.ToName, entry.ToSessionID), messageClock(entry.SentAt, now), messageStanding(entry))
		if entry.Subject != "" {
			line += ": " + entry.Subject
		}
		if entry.Reason != "" {
			line += "\n    " + entry.Reason
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func FormatMessage(entry MessageEntry) string {
	return formatMessage(entry, time.Now())
}

func formatMessage(entry MessageEntry, now time.Time) string {
	lines := []string{
		fmt.Sprintf("message %d from %s to %s", entry.MessageID, messageSender(entry), messageParty(entry.ToName, entry.ToSessionID)),
		"sent " + messageClock(entry.SentAt, now),
		"state: " + messageStanding(entry),
	}
	if entry.DeliveredAt != "" {
		lines[1] += ", delivered " + messageClock(entry.DeliveredAt, now)
	}
	if entry.Reason != "" {
		lines[2] += ": " + entry.Reason
	}
	if entry.Subject != "" {
		lines = append(lines, "subject: "+entry.Subject)
	}
	return strings.Join(lines, "\n") + "\n\n" + entry.Body
}

func messageSender(entry MessageEntry) string {
	if entry.FromSessionID == "" {
		return fmt.Sprintf("%q (another machine)", entry.FromName)
	}
	return messageParty(entry.FromName, entry.FromSessionID)
}

func messageParty(name, id string) string { return fmt.Sprintf("%q (%s)", name, id) }

func messageStanding(entry MessageEntry) string {
	if entry.QueuePosition > 0 {
		return fmt.Sprintf("%s at position %d", entry.State, entry.QueuePosition)
	}
	return entry.State
}

// messageClock is a stamp in local time, dated when it is not today.
func messageClock(stamp string, now time.Time) string {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return stamp
	}
	at, now = at.Local(), now.Local()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("2006-01-02 15:04")
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

func FormatGroupRemoval(removal GroupRemoval) string {
	line := fmt.Sprintf("deleted %s", strings.Join(removal.Removed, ", "))
	if len(removal.Moved) > 0 {
		line += fmt.Sprintf("; %d session(s) moved to the root group: %s",
			len(removal.Moved), strings.Join(removal.Moved, ", "))
	}
	return line
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
