// Package notify delivers a desktop or terminal notification when a
// managed session's status flips into a state that needs the user.
//
// Delivery is best-path. Inside Ghostty (cmux included) an OSC 777 escape
// goes to the drawing terminal, which turns it into a native notification
// attributed to that window and workspace; since the escape rides the
// terminal connection, it reaches the user even when the manager runs on a
// remote host over SSH. On macOS the manager posts through a helper app
// bundle of its own, so the banner carries the manager's name and icon and
// a click brings the terminal that launched the manager forward. Linux
// posts through notify-send, and a click there raises the terminal window
// where the display allows it. Both record the session the banner named,
// so the manager selects it on the next poll. WSL posts a Windows toast
// through PowerShell, which Windows attributes to PowerShell itself, so
// that banner tells the user and nothing more. With nothing better
// available the terminal bell is the floor, so headless setups still get
// an audible cue.
package notify

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/termseq"
	"github.com/YoanWai/agent-manager/internal/wsl"
)

// Overridable seams so tests can drive the platform branches without a
// real notification backend.
var (
	goos      = runtime.GOOS
	getenv    = os.Getenv
	lookPath  = exec.LookPath
	runCmd    = runBounded
	runOutput = runBoundedOutput
	runEnv    = runBoundedEnv
	emitSeq   = termseq.Emit
	configDir = config.Dir
	isWSL     = wsl.Detect
	macPost   = postThroughHelper
)

// cmdTimeout bounds external notifiers: a wedged osascript or notify-send
// must cost one delivery, not stall it forever.
const cmdTimeout = 2 * time.Second

// notifySendSettle is how long a banner has to prove it reached the
// desktop before delivery counts as done. Tests shorten it.
var notifySendSettle = time.Second

// clickTimeout is how long a notifier that reports the click may stay up
// waiting for it. A banner the user never touches expires on its own well
// inside this; the bound only reaps a daemon that never answers.
const clickTimeout = 10 * time.Minute

// helperStartTimeout bounds a notifier that has a runtime to spin up
// first, such as PowerShell reached through WSL interop.
const helperStartTimeout = 15 * time.Second

// clickWaiters caps how many banners may hold a notify-send open for
// their click at once.
var clickWaiters = make(chan struct{}, 8)

// errDenied reports that the user has refused the manager's notifications
// at the OS level, so no other banner should be tried on their behalf.
var errDenied = errors.New("notifications not allowed")

func runBounded(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), cmdTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}

// runBoundedEnv passes the content through the environment, which keeps
// it clear of the command line and of the notifier's own quoting.
func runBoundedEnv(env map[string]string, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), helperStartTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	return cmd.Run()
}

func runBoundedOutput(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return string(out), err
}

type Kind uint8

const (
	Waiting Kind = iota + 1
	Finished
	Errored
)

// Event is one session transition worth telling the user about. ID is the
// session's store id, which a click hands back so the manager can select
// that row.
type Event struct {
	ID      string
	Session string
	Tool    string
	Kind    Kind
}

type presentation struct {
	body          string
	macSound      string
	windowsSound  string
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
			windowsSound:  "ms-winsoundevent:Notification.Reminder",
			linuxSound:    "dialog-question",
			linuxUrgency:  "normal",
			linuxIcon:     "dialog-question",
			linuxCategory: "x-agent-manager.session.waiting",
		}, true
	case Finished:
		return presentation{
			body:          "● Finished",
			macSound:      "Hero",
			windowsSound:  "ms-winsoundevent:Notification.Default",
			linuxSound:    "complete-download",
			linuxUrgency:  "low",
			linuxIcon:     "emblem-default",
			linuxCategory: "x-agent-manager.session.finished",
		}, true
	case Errored:
		return presentation{
			body:          "✕ Errored",
			macSound:      "Basso",
			windowsSound:  "ms-winsoundevent:Notification.IM",
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
		err := macPost(event.ID, subtitle, body, detail.macSound)
		if err == nil {
			return
		}
		if errors.Is(err, errDenied) {
			break
		}
		// The argv form keeps content out of the script source, so
		// no AppleScript quoting is needed.
		if runCmd("osascript", "-e", "on run argv",
			"-e", `display notification (item 3 of argv) with title (item 1 of argv) subtitle (item 2 of argv) sound name (item 4 of argv)`,
			"-e", "end run", "--", "agent-manager", subtitle, body, detail.macSound) == nil {
			return
		}
	case "linux":
		if isWSL() {
			if windowsToast(subtitle, body, detail.windowsSound) == nil {
				return
			}
			break
		}
		if _, err := lookPath("notify-send"); err == nil && notifySend(event.ID, terminalBody, detail) == nil {
			return
		}
	}
	_ = emitSeq("\a")
}

// notifySend keeps the call open for the banner's lifetime when the
// installed notify-send can report actions, since that reply is the only
// way the click reaches the manager.
func notifySend(sessionID, body string, detail presentation) error {
	args := []string{
		"--app-name=agent-manager",
		"--urgency=" + detail.linuxUrgency,
		"--category=" + detail.linuxCategory,
		"--icon=" + detail.linuxIcon,
		"--hint=string:sound-name:" + detail.linuxSound,
	}
	if !notifySendReportsActions() {
		return runCmd("notify-send", append(args, "--", "agent-manager", body)...)
	}
	// A banner nobody dismisses holds its notify-send until the click
	// window runs out, so the ones that wait for a click are capped. Past
	// that, the banner still goes up; only its click is given up.
	select {
	case clickWaiters <- struct{}{}:
	default:
		return runCmd("notify-send", append(args, "--", "agent-manager", body)...)
	}
	args = append(args, "--action=default=Open", "--", "agent-manager", body)
	type reply struct {
		action string
		err    error
	}
	answered := make(chan reply, 1)
	go func() {
		defer func() { <-clickWaiters }()
		out, err := runOutput(clickTimeout, "notify-send", args...)
		answered <- reply{strings.TrimSpace(out), err}
	}()
	// A daemon that cannot take the banner refuses within milliseconds,
	// and that has to reach the caller as a failure so the bell still
	// rings. A banner on screen holds the call until the user acts.
	select {
	case answer := <-answered:
		if answer.err != nil {
			return answer.err
		}
		handleNotifySendReply(answer.action, sessionID)
	case <-time.After(notifySendSettle):
		go func() {
			answer := <-answered
			if answer.err == nil {
				handleNotifySendReply(answer.action, sessionID)
			}
		}()
	}
	return nil
}

func handleNotifySendReply(action, sessionID string) {
	if action != "default" {
		return
	}
	raiseTerminalWindow()
	dir, err := configDir()
	if err == nil {
		err = RequestFocus(dir, os.Getpid(), sessionID)
	}
	if err != nil {
		// Notify returned long ago, so the bell is the only thing left
		// that reaches the user: their click raised the terminal and then
		// went nowhere.
		_ = emitSeq("\a")
	}
}

// notifySendReportsActions reports whether the installed notify-send
// accepts --action, which libnotify added in 0.7.9.
func notifySendReportsActions() bool {
	out, err := runOutput(cmdTimeout, "notify-send", "--help")
	return err == nil && strings.Contains(out, "--action")
}

// raiseTerminalWindow brings the X11 window that launched the manager to
// the front. Wayland compositors refuse focus requests from other
// processes, so without WINDOWID or a tool to act on it the banner alone
// carries the click.
func raiseTerminalWindow() {
	window := getenv("WINDOWID")
	if window == "" {
		return
	}
	if _, err := lookPath("xdotool"); err == nil {
		if runCmd("xdotool", "windowactivate", "--sync", window) == nil {
			return
		}
	}
	if _, err := lookPath("wmctrl"); err == nil {
		_ = runCmd("wmctrl", "-ia", window)
	}
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
