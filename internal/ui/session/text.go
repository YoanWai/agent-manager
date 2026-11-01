package session

import (
	"errors"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// DeadHint names the ways back from a dead row by the keys the user bound:
// revive resumes the conversation, restart starts the tool over.
func DeadHint(keys keybind.Table) string {
	revive := keys.Binding(keybind.Revive).Label()
	restart := keys.Binding(keybind.Restart).Label()
	switch {
	case revive == "" && restart == "":
		return "session is dead - choose Revive from its row menu"
	case revive == "":
		return fmt.Sprintf("session is dead - press %s to restart or choose Revive from its row menu", restart)
	case restart == "":
		return fmt.Sprintf("session is dead - press %s to revive", revive)
	default:
		return fmt.Sprintf("session is dead - press %s to revive or %s to restart", revive, restart)
	}
}

// ArchivedHint names the way back from an archived row.
func ArchivedHint(keys keybind.Table) string {
	binding := keys.Binding(keybind.Restore).Label()
	if binding == "" {
		return "session is archived - choose Restore from its row menu"
	}
	return fmt.Sprintf("session is archived - press %s to restore", binding)
}

// DeadError is the refusal a write to a pane that is gone reports.
func DeadError(archived bool, keys keybind.Table) error {
	if archived {
		return errors.New(ArchivedHint(keys))
	}
	return errors.New(DeadHint(keys))
}

// ShellPromptHint refuses to write into a shell. SendText pastes and then
// presses Enter, so a sentence meant for an agent would run as a command on
// the user's machine. Entering the session is how text reaches a shell, where
// what is typed is plainly a command.
func ShellPromptHint(name string) string {
	return name + " is a shell, not an agent - enter it to type there"
}

// NonAgentPromptHint refuses a prompt for a shell or a browser row.
func NonAgentPromptHint(name string, browser bool) string {
	if browser {
		return name + " is a browser, not an agent - attach it to use the page"
	}
	return ShellPromptHint(name)
}

// DegradedResumeText says a revive fell back to the working directory's most
// recent conversation because its own conversation id was never captured,
// which resumes the wrong conversation whenever sessions share a directory.
func DegradedResumeText(name string) string {
	return fmt.Sprintf("revived %s with --continue: no conversation id captured, may resume the wrong conversation", name)
}

// FollowConfirmLabel asks to archive, restore, kill or delete one session and
// its live children, naming how many terminals go with it.
func FollowConfirmLabel(verb, name string, extra int, one, many string) string {
	if extra <= 0 {
		return fmt.Sprintf("%s %s? %s", verb, name, one)
	}
	unit := "terminal"
	if extra != 1 {
		unit = "terminals"
	}
	return fmt.Sprintf("%s %s and %d %s? %s", verb, name, extra, unit, many)
}

type Severity int

const (
	Done Severity = iota
	Warn
	Fail
)

// ReplyReport is the line a finished reply copy reports, and how the status
// bar reads it. No characters means the turn held nothing; unreadable means the
// tool draws no region a reply can be read from, which no amount of retrying
// changes; unbounded means nothing in the pane said where the turn began, so
// the copy is the whole screen rather than one answer.
func ReplyReport(name, tool string, chars int, unreadable, unbounded bool) (string, Severity) {
	switch {
	case unreadable:
		return fmt.Sprintf("no reply to read in %s: a %s pane is not read that way", name, tool), Warn
	case chars == 0:
		return "nothing to copy from " + name, Fail
	case unbounded:
		return fmt.Sprintf("copied %d chars from %s: %s marks no turn start here, so this is the whole pane",
			chars, name, tool), Warn
	}
	return fmt.Sprintf("copied %d chars from %s", chars, name), Done
}
