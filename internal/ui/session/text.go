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
