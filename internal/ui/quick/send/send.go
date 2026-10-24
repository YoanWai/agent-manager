package send

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	tea "github.com/charmbracelet/bubbletea"
)

// SendOutcome is how far a send got: refused before anything reached the
// pane, maybe reached it, or confirmed by the pane.
type SendOutcome uint8

const (
	SendRefused SendOutcome = iota
	SendUncertain
	SendConfirmed
)

// SendRequest freezes the selected pane and the prompt that Enter accepted.
// The worker may finish after the cursor or composer has moved.
type SendRequest struct {
	Session        store.Session
	ComposerGen    int
	Draft          string
	Text           string
	CloseAfterSend bool
	Images         []composer.Attachment
}

func (SendRequest) EffectName() string { return "quick send" }

func (r SendRequest) EffectCopy() uieffects.Request {
	r.Session.PendingInputs = slices.Clone(r.Session.PendingInputs)
	r.Session.RelaunchSnapshot = maps.Clone(r.Session.RelaunchSnapshot)
	r.Images = slices.Clone(r.Images)
	return r
}

func (r SendRequest) Run(s Services) (uieffects.Result, error) { return RunSend(s, r) }

// SendResult is what a send left: how far it got, the transport error, and
// the error from recording a confirmed prompt on the row.
type SendResult struct {
	Outcome     SendOutcome
	SendErr     error
	MetadataErr error
}

// Services is the work a send needs done off the update path.
type Services struct {
	Store         *store.Store
	Config        config.Config
	Driver        *tmux.Driver
	SessionExists func(*tmux.Driver, string) (bool, error)
	SendText      func(*tmux.Driver, string, string) (tmux.SendResult, error)
}

// RunSend checks the pane is still the one Enter accepted, then types the
// prompt into it.
func RunSend(services Services, request SendRequest) (SendResult, error) {
	result := SendResult{Outcome: SendRefused}
	current, err := services.Store.Get(request.Session.ID)
	if err != nil {
		return result, err
	}
	if !SameTarget(current, request.Session) {
		return result, errors.New("quick-send target changed its identity, tool, launch, or socket before the accepted prompt ran")
	}
	if services.Config.Tools[current.Tool].Shell {
		return result, errors.New(uisession.ShellPromptHint(current.Name))
	}
	exists, err := services.SessionExists(services.Driver, current.ID)
	if err != nil {
		return result, fmt.Errorf("check quick-send target %s: %w", current.Name, err)
	}
	if !exists {
		return result, errors.New(uisession.DeadHint)
	}
	sent, sendErr := services.SendText(services.Driver, current.ID, request.Text)
	if sendErr != nil {
		result.SendErr = sendErr
		if sent.PasteMayHaveStarted() {
			result.Outcome = SendUncertain
		}
		return result, nil
	}
	result.Outcome = SendConfirmed
	result.MetadataErr = services.Store.RecordQuickSendForLaunch(request.Session, request.Text)
	return result, nil
}

// SameTarget reports whether two reads name the same launch of a session.
func SameTarget(current, captured store.Session) bool {
	return current.ID == captured.ID &&
		current.CreatedAt.Equal(captured.CreatedAt) &&
		current.Tool == captured.Tool &&
		current.LaunchTime().Equal(captured.LaunchTime()) &&
		current.TmuxSocket == captured.TmuxSocket
}

// Root is what a finished send asks of the manager: the bar a confirmed
// send clears and the status bar it reports on.
type Root interface {
	ClearAccepted(composerGen int, draft string, closeAfterSend bool)
	RestoreImages(request SendRequest) tea.Cmd
	ReportErr(text string)
	ClearErr()
	MarkSent(session store.Session, text string)
}

// Apply applies a finished send: a refused one gives its images back, a
// confirmed one clears the bar it came from and marks the row.
func Apply(root Root, request SendRequest, result SendResult, err error) tea.Cmd {
	if err != nil || result.Outcome == SendRefused {
		cleanup := root.RestoreImages(request)
		if err != nil {
			root.ReportErr(err.Error())
		} else {
			root.ReportErr(fmt.Sprintf("prompt was not sent to %s: %v", request.Session.Name, result.SendErr))
		}
		return cleanup
	}

	root.ClearAccepted(request.ComposerGen, request.Draft, request.CloseAfterSend)
	// Once paste may have started, the pane may still need image paths from
	// the accepted text. They remain owned by that delivery, never by a retry.
	if result.Outcome == SendUncertain {
		root.ReportErr(fmt.Sprintf("prompt may have reached %s and was not sent again: %v", request.Session.Name, result.SendErr))
		return nil
	}
	if result.MetadataErr != nil {
		root.ReportErr("prompt sent, but recording it for the row failed: " + result.MetadataErr.Error())
		return nil
	}
	root.MarkSent(request.Session, request.Text)
	root.ClearErr()
	return nil
}
