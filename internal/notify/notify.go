// Package notify delivers a desktop or terminal notification when a
// managed session's status flips into a state that needs the user.
//
// Delivery is best-path. Inside Ghostty (cmux included) an OSC 777 escape
// goes to the drawing terminal, which turns it into a native notification
// attributed to that window and workspace — and since the escape rides the
// terminal connection, it reaches the user even when the manager runs on
// a remote host over SSH. Without such a terminal, macOS posts via
// osascript and Linux via notify-send. With nothing better available the
// terminal bell is the floor, so headless and WSL setups still get an
// audible cue.
package notify

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/termseq"
)

// Overridable seams so tests can drive the platform branches without a
// real notification backend.
var (
	goos     = runtime.GOOS
	getenv   = os.Getenv
	lookPath = exec.LookPath
	runCmd   = runBounded
	emitSeq  = termseq.Emit
)

// cmdTimeout bounds external notifiers: a wedged osascript or notify-send
// must cost one delivery, not stall it forever.
const cmdTimeout = 2 * time.Second

func runBounded(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

type Kind uint8

const (
	Waiting Kind = iota + 1
	Finished
	Errored
)

type Event struct {
	Session string
	Tool    string
	Kind    Kind
}

type presentation struct {
	body          string
	macSound      string
	linuxSound    string
	linuxUrgency  string
	linuxIcon     string
	linuxCategory string
}

func describe(kind Kind) (presentation, bool) {
	switch kind {
	case Waiting:
		return presentation{
			body:          "◆ Waiting for your input",
			macSound:      "Funk",
			linuxSound:    "dialog-question",
			linuxUrgency:  "normal",
			linuxIcon:     "dialog-question",
			linuxCategory: "x-agent-manager.session.waiting",
		}, true
	case Finished:
		return presentation{
			body:          "● Finished",
			macSound:      "Hero",
			linuxSound:    "complete-download",
			linuxUrgency:  "low",
			linuxIcon:     "emblem-default",
			linuxCategory: "x-agent-manager.session.finished",
		}, true
	case Errored:
		return presentation{
			body:          "✕ Errored",
			macSound:      "Basso",
			linuxSound:    "dialog-error",
			linuxUrgency:  "critical",
			linuxIcon:     "dialog-error",
			linuxCategory: "x-agent-manager.session.errored",
		}, true
	default:
		return presentation{}, false
	}
}

// Notify fires one notification for an agent state transition. Delivery
// failures fall through to the next path and finally to the bell; nothing
// is reported, since a missed ping must never surface as an app error.
func Notify(event Event) {
	detail, ok := describe(event.Kind)
	if !ok {
		return
	}
	session := sanitize(event.Session)
	tool := sanitize(event.Tool)
	subtitle := session
	if tool != "" {
		subtitle += " · " + tool
	}
	body := detail.body
	terminalBody := body + " — " + subtitle
	// A terminal that understands OSC 777 turns it into a native
	// notification wherever the terminal actually is — including at the
	// far end of an SSH session, which the remote host's desktop daemon
	// could never reach.
	if ghostty() && emitSeq(osc777("agent-manager", terminalBody)) == nil {
		return
	}
	switch goos {
	case "darwin":
		// The argv form keeps content out of the script source, so
		// no AppleScript quoting is needed.
		if runCmd("osascript", "-e", "on run argv",
			"-e", `display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv) sound name (item 4 of argv)`,
			"-e", "end run", "--", "agent-manager", subtitle, body, detail.macSound) == nil {
			return
		}
	case "linux":
		if _, err := lookPath("notify-send"); err == nil &&
			runCmd("notify-send",
				"--app-name=agent-manager",
				"--urgency="+detail.linuxUrgency,
				"--category="+detail.linuxCategory,
				"--icon="+detail.linuxIcon,
				"--hint=string:sound-name:"+detail.linuxSound,
				"--", "agent-manager", terminalBody) == nil {
			return
		}
	}
	_ = emitSeq("\a")
}

// ghostty reports whether the drawing terminal understands OSC 777
// notifications: Ghostty itself or a cmux workspace. TERM is the one
// marker a plain SSH session carries to the remote host; cmux's own
// remote sessions also pass TERM_PROGRAM and CMUX_WORKSPACE_ID through.
func ghostty() bool {
	return getenv("TERM_PROGRAM") == "ghostty" ||
		getenv("CMUX_WORKSPACE_ID") != "" ||
		getenv("TERM") == "xterm-ghostty"
}

// osc777 builds the Ghostty notification sequence. Semicolons would read
// as field separators in the payload, so they cannot survive.
func osc777(title, body string) string {
	return "\x1b]777;notify;" + strings.ReplaceAll(title, ";", ",") + ";" +
		strings.ReplaceAll(body, ";", ",") + "\a"
}

// sanitize squashes a title or body to one line with no control
// characters, so neither escapes nor external commands can be fed
// anything that breaks out of its payload.
func sanitize(s string) string {
	mapped := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(mapped), " ")
}
