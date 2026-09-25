package tmux

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// SendText delivers text into the session's pane and presses Enter, so the
// agent inside receives it as a user message.
func (d *Driver) SendText(id, text string) error {
	return d.pasteAndEnter(PaneTarget(id), text)
}

// SendKeys delivers exact tmux key names to a session. Keeping each key as
// its own argv entry avoids routing agent-supplied input through a shell.
func (d *Driver) SendKeys(id string, keys ...string) error {
	args := []string{"send-keys", "-t", PaneTarget(id), "--"}
	_, err := d.run(append(args, keys...)...)
	return err
}

// Paste delivers text into the session's pane without submitting it. The
// focus path uses this for clipboard pastes: sending the bytes as raw
// keystrokes would turn every newline into an Enter press and submit the
// agent's prompt mid-paste.
func (d *Driver) Paste(id, text string) error {
	return d.paste(PaneTarget(id), text)
}

var pasteSeq atomic.Uint64

// Only a pane that never echoes what it reads waits out echoWait; an agent
// redraws a paste in tens of milliseconds, even mid-launch.
const (
	echoWait = time.Second
	echoPoll = 25 * time.Millisecond
)

// pasteAndEnter holds the Enter until the pane has drawn the paste. Both
// writes reach one pty, and a pane too busy to read between them takes the
// carriage return as part of the bracketed paste rather than as a submit,
// stranding the message in the composer.
func (d *Driver) pasteAndEnter(target, text string) error {
	before, baseline := d.capturePlain(target)
	if err := d.paste(target, text); err != nil {
		return err
	}
	if baseline != nil {
		// Without a baseline, text already on screen reads as the new paste,
		// so the pane gets the whole window to draw it rather than a match.
		time.Sleep(echoWait)
	} else {
		d.awaitPasteEcho(target, before, text)
	}
	_, err := d.run("send-keys", "-t", target, "Enter")
	return err
}

// A pane that draws the paste some other way, as a collapsed placeholder or
// not at all, is released at the cap and submits the way it did before.
func (d *Driver) awaitPasteEcho(target, before, text string) {
	opening := MessageOpening(text)
	if opening == "" {
		return
	}
	was := strings.Count(before, opening)
	deadline := time.Now().Add(echoWait)
	for {
		if pane, err := d.capturePlain(target); err == nil && strings.Count(pane, opening) > was {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(echoPoll)
	}
}

// MessageOpening is the slice of a message to look for in a pane: its first
// line with anything on it, cut short because a composer wraps a long line
// and would split any longer match. A message that opens on a blank line
// still has to be waited for, so the blank lines are skipped rather than
// answered with nothing to match.
func MessageOpening(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if runes := []rune(line); len(runes) > 16 {
			line = strings.TrimSpace(string(runes[:16]))
		}
		return line
	}
	return ""
}

// paste loads text into a tmux buffer and pastes it into the pane.
// tmux send-keys silently stops around 1024 bytes; load-buffer does not.
func (d *Driver) paste(target, text string) error {
	file, err := os.CreateTemp("", "am-paste-*")
	if err != nil {
		return fmt.Errorf("paste temp file: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return fmt.Errorf("paste temp write: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("paste temp close: %w", err)
	}
	// tmux buffers are server-wide, and every agent's MCP process pastes too.
	buf := fmt.Sprintf("am_paste_%d_%d", os.Getpid(), pasteSeq.Add(1))
	if _, err := d.run("load-buffer", "-b", buf, path); err != nil {
		return err
	}
	// Preserve bracketed-paste boundaries when the pane application requests
	// them. Codex uses paste-burst detection without these markers and can
	// consume the immediately following Enter as part of the paste, leaving
	// the prompt in its composer instead of submitting it.
	if _, err := d.run("paste-buffer", "-p", "-d", "-b", buf, "-t", target); err != nil {
		_, _ = d.run("delete-buffer", "-b", buf)
		return err
	}
	return nil
}

// SendRaw runs one pre-assembled tmux command line. The focus path builds
// send-keys commands from fixed tokens and hex codes, so whitespace
// splitting is exact; nothing quoted ever rides through here.
func (d *Driver) SendRaw(command string) error {
	_, err := d.run(strings.Fields(command)...)
	return err
}

// SendCommand runs one tmux command from already-separated arguments,
// for a command such as if-shell whose branch is itself a command and so
// cannot survive the whitespace split SendRaw does.
func (d *Driver) SendCommand(args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("tmux command is empty")
	}
	_, err := d.run(args...)
	return err
}
