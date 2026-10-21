package tmux

import (
	"fmt"
	"strings"
)

// SendKeysHex is a send-keys command that writes raw bytes to target as hex
// byte codes (-H), which sidesteps tmux command-line quoting entirely.
func SendKeysHex(target string, raw []byte) string {
	return "send-keys -t " + target + " -H " + hexBytes(string(raw))
}

// SendKeyName is a send-keys command that presses one tmux key name, such
// as Enter or M-Up, on target.
func SendKeyName(target, name string) string {
	return "send-keys -t " + target + " " + name
}

func hexBytes(s string) string {
	codes := make([]string, 0, len(s))
	for i := 0; i < len(s); i++ {
		codes = append(codes, fmt.Sprintf("%02x", s[i]))
	}
	return strings.Join(codes, " ")
}

// GuardedMouseReport sends a report only while the pane still tracks the
// mouse. The branch for a pane that has stopped tracking runs a command with
// no effect, so tmux answers the guard with GuardedMouseReplies blocks
// whichever way it goes. command rides the control pipe; args are the same
// command for a forked tmux, whose nested send has to stay one argument.
func GuardedMouseReport(target, report string) (command string, args []string) {
	const condition = "#{mouse_any_flag}"
	const drop = "display-message -p"
	send := SendKeysHex(target, []byte(report))
	command = "if-shell -F -t " + target + " '" + condition + "' '" + send + "' '" + drop + "'"
	return command, []string{"if-shell", "-F", "-t", target, condition, send, drop}
}

// GuardedMouseReplies is the reply blocks a guarded report costs on the
// control pipe: one for if-shell, one for the branch it ran.
const GuardedMouseReplies = 2
