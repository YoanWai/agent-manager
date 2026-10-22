package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SendText delivers text into the session's pane and presses Enter, so the
// agent inside receives it as a user message.
func (d *Driver) SendText(id, text string) error {
	_, err := d.SendTextResult(id, text)
	return err
}

// SendTextResult has SendText's default deadline while preserving which
// transport phase began. Callers that own retry policy use the phase to avoid
// resending text that may already have reached the pane.
func (d *Driver) SendTextResult(id, text string) (SendResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
	defer cancel()
	return d.SendTextContext(ctx, id, text)
}

type SendPhase uint8

const (
	SendPhaseNotStarted SendPhase = iota
	SendPhaseLoadStarted
	SendPhaseLoaded
	SendPhasePasteStarted
	SendPhasePasted
	SendPhaseSubmitStarted
	SendPhaseSubmitted
)

type SendResult struct {
	Phase SendPhase
}

func (result SendResult) PasteMayHaveStarted() bool {
	return result.Phase >= SendPhasePasteStarted
}

func (d *Driver) SendTextContext(ctx context.Context, id, text string) (SendResult, error) {
	return d.SubmitTextContext(ctx, id, text, nil)
}

// ErrUnsubmitted reports a paste still sitting in the composer after every
// Enter SubmitTextContext pressed for it.
var ErrUnsubmitted = errors.New("the pasted text is still in the composer after Enter")

// SubmitTextContext is SendTextContext for a caller that can read the
// session's composer: drafted reports whether it holds text, and must report
// false before the paste. A composer it cannot read proves no submit, so its
// error is returned rather than taken for an empty composer. A tool may draw
// the paste as a collapsed placeholder rather than its text, and may swallow
// the Enter after it, so the composer filling is taken as the echo and the
// composer emptying as the submit.
func (d *Driver) SubmitTextContext(ctx context.Context, id, text string, drafted func() (bool, error)) (SendResult, error) {
	target := PaneTarget(id)
	release, err := enterPaneSendGate(ctx, d.socket, target)
	if err != nil {
		return SendResult{}, err
	}
	defer release()
	return d.pasteAndEnterContext(ctx, id, text, drafted)
}

// A human send from the effect lane and the poller's automatic delivery can
// reach one pane at once. Interleaved, the second paste lands before the
// first Enter and both texts submit as one prompt, so each pane takes one
// whole send at a time while other panes do not wait. The gates are keyed by
// socket and pane, so every Driver in the process shares them, and each
// holds a lock file too for a send from another process, such as
// `agent-manager type` beside a host's manager.
var paneSendGates = struct {
	sync.Mutex
	gates map[string]*paneSendGate
}{gates: map[string]*paneSendGate{}}

type paneSendGate struct {
	slot  chan struct{}
	users int
}

func enterPaneSendGate(ctx context.Context, socket, target string) (release func(), err error) {
	key := socket + "\x00" + target
	paneSendGates.Lock()
	gate := paneSendGates.gates[key]
	if gate == nil {
		gate = &paneSendGate{slot: make(chan struct{}, 1)}
		paneSendGates.gates[key] = gate
	}
	gate.users++
	paneSendGates.Unlock()
	leave := func() {
		paneSendGates.Lock()
		if gate.users--; gate.users == 0 {
			delete(paneSendGates.gates, key)
		}
		paneSendGates.Unlock()
	}
	select {
	case gate.slot <- struct{}{}:
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
	unlock, err := lockPaneSend(ctx, socket, target)
	if err != nil {
		<-gate.slot
		leave()
		return nil, err
	}
	return func() { unlock(); <-gate.slot; leave() }, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
	defer cancel()
	return d.PasteContext(ctx, id, text)
}

func (d *Driver) PasteContext(ctx context.Context, id, text string) error {
	_, err := d.pasteContext(ctx, id, text)
	return err
}

var pasteSeq atomic.Uint64

// Only a pane that never echoes what it reads waits out echoWait; an agent
// redraws a paste in tens of milliseconds, even mid-launch.
const (
	echoWait = time.Second
	echoPoll = 25 * time.Millisecond
)

// An agent clears its composer within a frame of reading the Enter; one that
// still holds the paste a second later dropped it, and gets it again.
const (
	submitWait  = time.Second
	submitTries = 3
)

// pasteAndEnter holds the Enter until the pane has drawn the paste. Both
// writes reach one pty, and a pane too busy to read between them takes the
// carriage return as part of the bracketed paste rather than as a submit,
// stranding the message in the composer.
func (d *Driver) pasteAndEnterContext(ctx context.Context, id, text string, drafted func() (bool, error)) (SendResult, error) {
	target := PaneTarget(id)
	before, baseline := d.capturePlainContext(ctx, target)
	if err := ctx.Err(); err != nil {
		return SendResult{}, err
	}
	result, err := d.pasteContext(ctx, id, text)
	if err != nil {
		return result, err
	}
	if baseline != nil {
		// Without a baseline, text already on screen reads as the new paste,
		// so the pane gets the whole window to draw it rather than a match.
		timer := time.NewTimer(echoWait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return result, ctx.Err()
		case <-timer.C:
		}
	} else {
		if err := d.awaitPasteEchoContext(ctx, target, before, text, drafted); err != nil {
			return result, err
		}
	}
	_, err = d.runContextStarted(ctx, func() { result.Phase = SendPhaseSubmitStarted }, "send-keys", "-t", target, "Enter")
	if err != nil {
		return result, err
	}
	result.Phase = SendPhaseSubmitted
	if drafted == nil {
		return result, nil
	}
	return result, d.confirmSubmit(ctx, target, drafted)
}

// confirmSubmit presses Enter again while the composer still holds the
// paste, a bounded number of times, and reports a paste no Enter submitted.
func (d *Driver) confirmSubmit(ctx context.Context, target string, drafted func() (bool, error)) error {
	for try := 1; ; try++ {
		cleared, err := composerCleared(ctx, drafted)
		if err != nil || cleared {
			return err
		}
		if try == submitTries {
			return ErrUnsubmitted
		}
		if _, err := d.runContext(ctx, "send-keys", "-t", target, "Enter"); err != nil {
			return err
		}
	}
}

func composerCleared(ctx context.Context, drafted func() (bool, error)) (bool, error) {
	deadline := time.Now().Add(submitWait)
	for {
		typed, err := drafted()
		if err != nil {
			return false, err
		}
		if !typed {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		timer := time.NewTimer(echoPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

// A pane that draws the paste some other way, as a collapsed placeholder or
// not at all, is released at the cap and submits the way it did before,
// unless the caller can see its composer fill. A composer read that fails
// here only costs the early release; confirmSubmit is what reports it.
func (d *Driver) awaitPasteEchoContext(ctx context.Context, target, before, text string, drafted func() (bool, error)) error {
	opening := MessageOpening(text)
	if opening == "" {
		return nil
	}
	was := strings.Count(before, opening)
	deadline := time.Now().Add(echoWait)
	for {
		if pane, err := d.capturePlainContext(ctx, target); err == nil && strings.Count(pane, opening) > was {
			return nil
		} else if ctx.Err() != nil {
			return ctx.Err()
		}
		if drafted != nil {
			if typed, err := drafted(); err == nil && typed {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return nil
		}
		timer := time.NewTimer(echoPoll)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
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

// paste loads text into a tmux buffer and pastes it into the session's pane.
// tmux send-keys silently stops around 1024 bytes; load-buffer does not.
func (d *Driver) pasteContext(ctx context.Context, id, text string) (SendResult, error) {
	target := PaneTarget(id)
	result := SendResult{}
	file, err := os.CreateTemp("", "am-paste-*")
	if err != nil {
		return result, fmt.Errorf("paste temp file: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(text); err != nil {
		file.Close()
		return result, fmt.Errorf("paste temp write: %w", err)
	}
	if err := file.Close(); err != nil {
		return result, fmt.Errorf("paste temp close: %w", err)
	}
	// tmux buffers are server-wide, and every agent's MCP process pastes too.
	// psmux keeps them per session server, so each step goes to this one's.
	buf := fmt.Sprintf("am_paste_%d_%d", os.Getpid(), pasteSeq.Add(1))
	if _, err := d.runInContext(ctx, func() { result.Phase = SendPhaseLoadStarted }, id, "load-buffer", "-b", buf, path); err != nil {
		return result, err
	}
	result.Phase = SendPhaseLoaded
	// Preserve bracketed-paste boundaries when the pane application requests
	// them. Codex uses paste-burst detection without these markers and can
	// consume the immediately following Enter as part of the paste, leaving
	// the prompt in its composer instead of submitting it.
	if _, err := d.runInContext(ctx, func() { result.Phase = SendPhasePasteStarted }, id, "paste-buffer", "-p", "-d", "-b", buf, "-t", target); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		_, _ = d.runInContext(cleanupCtx, nil, id, "delete-buffer", "-b", buf)
		cancel()
		return result, err
	}
	result.Phase = SendPhasePasted
	return result, nil
}

// SendRaw runs one pre-assembled tmux command line. The focus path builds
// send-keys commands from fixed tokens and hex codes, so whitespace
// splitting is exact; nothing quoted ever rides through here.
func (d *Driver) SendRaw(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), CommandTimeout)
	defer cancel()
	return d.SendRawContext(ctx, command)
}

func (d *Driver) SendRawContext(ctx context.Context, command string) error {
	_, err := d.runContext(ctx, strings.Fields(command)...)
	return err
}
