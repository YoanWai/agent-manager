package session

import "fmt"

// DeadHint names both ways back from a dead row: revive resumes the
// conversation, restart starts the tool over.
const DeadHint = "session is dead - press v to revive or R to restart"

// ShellPromptHint refuses to write into a shell. SendText pastes and then
// presses Enter, so a sentence meant for an agent would run as a command on
// the user's machine. Entering the session is how text reaches a shell, where
// what is typed is plainly a command.
func ShellPromptHint(name string) string {
	return name + " is a shell, not an agent - enter it to type there"
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

// Severity is how the status bar reads a reply copy's line.
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
