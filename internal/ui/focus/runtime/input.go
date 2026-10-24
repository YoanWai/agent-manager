package runtime

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/focus/watch"
	"github.com/charmbracelet/x/ansi"
)

// Kind is what one queued write to the focused pane carries.
type Kind uint8

const (
	// Keys is a tmux send-keys command the view already encoded.
	Keys Kind = iota
	// Mouse is a guarded mouse report the pane's application asked for.
	Mouse
	// Paste is pasted text, which travels over its own transport.
	Paste
	// Prompt records a composer draft as the session's last prompt.
	Prompt
)

// Request is one write to a focused session's pane. It freezes the session row
// it was built from, so a target relaunched, re-used or moved to another tmux
// server between the key press and the lane reaching it refuses input.
type Request struct {
	Kind    Kind
	Session store.Session
	Command string
	Args    []string
	Text    string
	Submit  bool
	// Echo asks the watcher for a prompt capture once the input lands, so the
	// pane's echo of it shows without waiting out the frame budget.
	Echo bool
	// Counted marks a write the polled watcher counted, so it hears the write
	// land whatever the outcome.
	Counted bool
}

// Copied is the copy the effect lane keeps for itself.
func (r Request) Copied() Request {
	r.Args = slices.Clone(r.Args)
	r.Session.PendingInputs = slices.Clone(r.Session.PendingInputs)
	r.Session.RelaunchSnapshot = maps.Clone(r.Session.RelaunchSnapshot)
	return r
}

// Result reports what a write left for the Update path: the session it reached,
// the composer draft an enter captured, and the prompt a receipt recorded.
type Result struct {
	InputID   string
	Candidate *Candidate
	PromptID  string
	Prompt    string
}

// Candidate is a composer draft snapshotted as enter went into a focused pane,
// held until the session shows the send went through.
type Candidate struct {
	ID      string
	Session store.Session
	Text    string
	At      time.Time
}

// Writer is what one queued write runs against.
type Writer struct {
	Store  *store.Store
	Driver *tmux.Driver
	Engine *status.Engine
	Watch  *watch.Watcher
	// Paste carries pasted text into the pane.
	Paste func(*tmux.Driver, string, string) error
}

// Run performs one queued write.
func (w Writer) Run(request Request) (Result, error) {
	if request.Counted {
		defer w.Watch.WriteLanded(request.Session.ID)
	}
	current, err := w.refuseStale(request)
	if err != nil {
		return Result{}, err
	}
	if request.Kind == Prompt {
		return Result{PromptID: current.ID, Prompt: request.Text}, w.Store.SetLastPromptForLaunch(current, request.Text)
	}
	result := Result{InputID: current.ID}
	if request.Submit && w.Engine != nil {
		if pane, captureErr := w.Driver.CapturePane(current.ID); captureErr == nil {
			if draft, ok := w.Engine.InputDraft(current.Tool, ansi.Strip(pane)); ok {
				result.Candidate = &Candidate{ID: current.ID, Session: current, Text: draft, At: time.Now()}
			}
		}
	}
	return result, w.send(current, request)
}

// ErrUnreplayed marks a write the control client may have delivered before it
// failed, which the person has to know before typing the same thing again.
var ErrUnreplayed = errors.New("focused input may have reached the pane and was not replayed")

// refuseStale reads the row again and refuses a target whose creation, launch
// or socket moved since the write was queued.
func (w Writer) refuseStale(request Request) (store.Session, error) {
	current, err := w.Store.Get(request.Session.ID)
	if err != nil {
		return store.Session{}, err
	}
	if !current.CreatedAt.Equal(request.Session.CreatedAt) || current.TmuxSocket != request.Session.TmuxSocket || !current.LaunchTime().Equal(request.Session.LaunchTime()) {
		return store.Session{}, errors.New("input target changed its creation, launch, or socket before the accepted input ran")
	}
	return current, nil
}

// send writes the request through the watcher's control client when it serves
// the pane, and over its own transport otherwise.
func (w Writer) send(current store.Session, request Request) error {
	if request.Kind != Keys && request.Kind != Mouse {
		err := w.Paste(w.Driver, current.ID, request.Text)
		w.echoed(current.ID, request, err)
		return err
	}
	blocks := 1
	if request.Kind == Mouse {
		// The guarded report answers with one block for if-shell and one for
		// whichever branch it ran.
		blocks = tmux.GuardedMouseReplies
	}
	available, err := w.forward(request, blocks)
	if available && err != nil {
		err = fmt.Errorf("%w: %w", ErrUnreplayed, err)
	}
	if !available {
		if request.Kind == Keys {
			err = w.Driver.SendRaw(request.Command)
		} else {
			err = w.Driver.SendCommand(request.Args...)
		}
	}
	w.echoed(current.ID, request, err)
	return err
}

func (w Writer) forward(request Request, blocks int) (bool, error) {
	if w.Watch == nil || request.Command == "" {
		return false, nil
	}
	return w.Watch.ForwardBlocks(request.Command, blocks)
}

func (w Writer) echoed(id string, request Request, err error) {
	if request.Echo && err == nil && w.Watch != nil {
		w.Watch.InputSent(id)
	}
}
