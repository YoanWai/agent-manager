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
// mouse. command rides the control pipe; args are the same command for a
// forked tmux, whose nested send has to stay one argument.
func GuardedMouseReport(target, report string) (command string, args []string) {
	return guarded(target, "#{mouse_any_flag}", SendKeysHex(target, []byte(report)))
}

// GuardedExtendedKey presses a modified key only while the pane has asked for
// extended keys. Any other pane would get the bare key or its name typed out,
// such as Enter for S-Enter, and tmux before 3.5 has no pane_key_mode, so the
// key is dropped there.
func GuardedExtendedKey(target, name string) (command string, args []string) {
	return guarded(target, "#{m:Ext *,#{pane_key_mode}}", SendKeyName(target, name))
}

// guarded runs send only while condition holds on target. The branch for a
// false condition runs a command with no effect, so tmux answers the guard
// with GuardedReplies blocks whichever way it goes.
func guarded(target, condition, send string) (command string, args []string) {
	const drop = "display-message -p"
	command = "if-shell -F -t " + target + " '" + condition + "' '" + send + "' '" + drop + "'"
	return command, []string{"if-shell", "-F", "-t", target, condition, send, drop}
}

// GuardedReplies is the reply blocks a guarded send costs on the control
// pipe: one for if-shell, one for the branch it ran.
const GuardedReplies = 2
