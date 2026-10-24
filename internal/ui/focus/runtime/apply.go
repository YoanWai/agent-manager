package runtime

import (
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// ProbeHost is what entering focus once the dead-pane probe has run asks of
// the root.
type ProbeHost interface {
	ReportErr(text string)
	// Grounded reports whether a probe asked with generation still owns the
	// foreground: not a quit, and no newer press since.
	Grounded(generation uint64) bool
	// ListClear reports whether the list is up with no card over it.
	ListClear() bool
	Enter(sessionID string) tea.Cmd
}

// ApplyProbe enters focus once the dead-pane probe has run, while the
// selection and the foreground it was asked for still hold.
func ApplyProbe(h ProbeHost, request ProbeRequest, err error) tea.Cmd {
	if err != nil {
		h.ReportErr(err.Error())
		return nil
	}
	if !h.Grounded(request.Generation) || !h.ListClear() {
		return nil
	}
	return h.Enter(request.SessionID)
}

type DetachHost interface {
	ReportErr(text string)
	// Warn puts a caveat on the status bar, which a refused request is.
	Warn(text string)
	Refresh()
	Quitting() bool
	// TakenSinceDetach reports whether the user moved the screen since the
	// session detached.
	TakenSinceDetach() bool
	// Name is the display name of a session, or its id when the row is gone.
	Name(sessionID string) string
	Focused(sessionID string) bool
	Review(sessionID string) tea.Cmd
	Editor(sessionID string) tea.Cmd
}

// ApplyDetach opens the view a session asked for before it detached. The
// marker is already cleared, so a request the user moved past stays on the
// status bar instead of vanishing; moving the cursor is not moving past it.
func ApplyDetach(h DetachHost, request DetachRequest, result Detach, err error) tea.Cmd {
	if err != nil {
		h.ReportErr(err.Error())
		return nil
	}
	if result.ClearErr != nil {
		h.ReportErr(result.ClearErr.Error())
		h.Refresh()
		return nil
	}
	if h.Quitting() {
		return nil
	}
	if result.Request == "" {
		h.Refresh()
		return nil
	}
	if h.TakenSinceDetach() {
		h.Warn("the " + result.Request + " asked for in " + h.Name(request.SessionID) + " did not open: something else took the screen first; ask again")
		h.Refresh()
		return nil
	}
	if !h.Focused(request.SessionID) {
		h.ReportErr("the session that asked for it has left the list")
		h.Refresh()
		return nil
	}
	switch result.Request {
	case tmux.RequestReview:
		return h.Review(request.SessionID)
	case tmux.RequestEditor:
		return h.Editor(request.SessionID)
	}
	h.Refresh()
	return nil
}

type InputHost interface {
	ReportErr(text string)
	Landed(sessionID string)
	Hold(candidate Candidate)
	Recorded(sessionID, prompt string)
}

// ApplyInput lands one queued write: the echo it asks for, the composer draft
// an enter captured, and the prompt a receipt recorded.
func ApplyInput(h InputHost, result Result, err error) {
	if err != nil {
		h.ReportErr(err.Error())
		return
	}
	if result.InputID != "" {
		h.Landed(result.InputID)
	}
	if result.Candidate != nil {
		h.Hold(*result.Candidate)
	}
	if result.PromptID != "" {
		h.Recorded(result.PromptID, result.Prompt)
	}
}
