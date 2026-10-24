// Package install is the runtime behind the setup-step card: the CLI
// installer it types into a shell tab, the status that shell records, and the
// launch retried once the binary is on PATH. The card in package dialogs
// paints the fix this State holds; the root routes what the runtime answers.
package install

import (
	"os"
	"slices"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilaunchhint "github.com/YoanWai/agent-manager/internal/ui/launchhint"
	tea "github.com/charmbracelet/bubbletea"
)

// State is the install this run started and the fix the card shows. Both
// outlive the card that started them: the install keeps running in its shell
// tab, and the fix holds the launch to retry and the images its prompt named.
type State struct {
	// Pending is the install still running in a shell tab, nil when none is.
	Pending *Pending
	Launch  Fix
}

// Fix is what the setup-step card holds: the text it shows, the command it
// prints and copies, the script the install key runs, the binary that script
// puts on PATH, the launch to run again once it is there, and the images that
// launch's prompt names. Command is empty when the manager knows no recipe,
// which leaves the card read-only. Update marks a CLI that is installed but
// too old, where the same installer updates it.
type Fix struct {
	Text    string
	Command string
	Script  string
	Binary  string
	Update  bool
	Retry   uieffects.Request
	Images  []composer.Attachment
}

// Pending is the install still running in a shell tab: the session running
// it, the files its script writes and runs from, the binary it puts on PATH,
// and the launch to finish once its exit status lands.
type Pending struct {
	SessionID  string
	Name       string
	Binary     string
	StatusFile string
	Script     string
	Retry      uieffects.Request
	Images     []composer.Attachment
}

// Retry is the launch a refused spawn left for the install to run again. The
// root owns what it points at; this package only carries it.
type Retry = uieffects.Request

type Root interface {
	ReportErr(text string)
	ReportDone(text string)
	// SessionLaunched records a row the install opened, so a poll that
	// listed the store before it cannot drop it.
	SessionLaunched(sess store.Session)
	// HintOpen reports whether the card that asked for this generation is
	// still on screen.
	HintOpen(gen uint64) bool
	// ShowInstall puts the card away and gives the screen to the installer
	// shell, dropping drafts the install now owns.
	ShowInstall(sessionID string) tea.Cmd
	Refresh() tea.Cmd
	QueueRetry(retry Retry)
}

// CopiedMsg reports the card's copy of the install command.
type CopiedMsg struct {
	Err error
}

func (s *State) Copy() tea.Cmd {
	command := s.Launch.Command
	return func() tea.Msg { return CopiedMsg{Err: CopyCommand(command)} }
}

// CopyCommand is the seam tests swap so a copy never reaches the desktop
// clipboard.
var CopyCommand = clipboard.WriteText

func (s *State) Release() {
	DropImages(s.Launch.Images)
	s.Launch = Fix{}
}

// Route applies what the runtime answers itself, reporting whether msg was
// one of its messages.
func (s *State) Route(root Root, msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case CopiedMsg:
		if msg.Err != nil {
			root.ReportErr("copy failed: " + msg.Err.Error())
			return nil, true
		}
		root.ReportDone("copied to clipboard")
		return nil, true
	}
	return nil, false
}

// StartRequest freezes the setup dialog, shell selection, placement, retry
// and image ownership before any filesystem, store or tmux work begins.
type StartRequest struct {
	Gen      uint64
	ID       string
	Command  string
	Binary   string
	ToolName string
	Group    string
	Pane     sessioncmd.PaneSize
	Retry    uieffects.Request
	Images   []composer.Attachment
}

// Copied owns its images, so a later edit of the fix cannot retarget the job.
func (r StartRequest) Copied() StartRequest {
	r.Images = slices.Clone(r.Images)
	return r
}

func (StartRequest) EffectName() string { return "CLI install" }

func (r StartRequest) EffectCopy() uieffects.Request { return r.Copied() }

func (r StartRequest) Run(s Services) (uieffects.Result, error) { return RunStart(s, r) }

// StartResult carries a committed shell row even when a later label or send
// step fails. Pending is set only after the command was typed.
type StartResult struct {
	Session store.Session
	Pending *Pending
}

type SettleRequest struct {
	Pending Pending
}

// Copied owns its images, so a settle check cannot outlive the fix's slice.
func (r SettleRequest) Copied() SettleRequest {
	r.Pending.Images = slices.Clone(r.Pending.Images)
	return r
}

func (SettleRequest) EffectName() string { return "CLI install" }

func (r SettleRequest) EffectCopy() uieffects.Request { return r.Copied() }

func (r SettleRequest) Run(s Services) (uieffects.Result, error) { return RunSettle(s, r) }

type SettleResult struct {
	SessionID string
	State     uilaunchhint.SettleState
	Code      string
}

// Started applies the admit result: the shell row joins the workspace, the
// card hands the screen over, and images the install did not take go back to
// the fix or are dropped with it.
func (s *State) Started(root Root, request StartRequest, result StartResult, err error) tea.Cmd {
	if result.Session.ID != "" {
		root.SessionLaunched(result.Session)
	}
	stillOpen := root.HintOpen(request.Gen)
	var cleanup tea.Cmd
	if err != nil {
		if result.Pending != nil {
			s.Pending = result.Pending
			if stillOpen {
				cleanup = root.ShowInstall(result.Session.ID)
			}
			root.ReportErr(err.Error())
			return tea.Batch(cleanup, root.Refresh())
		}
		if stillOpen {
			s.Launch.Images = append(request.Images, s.Launch.Images...)
		} else {
			cleanup = CleanupImages(request.Images)
		}
		root.ReportErr(err.Error())
		return cleanup
	}
	s.Pending = result.Pending
	if stillOpen {
		cleanup = root.ShowInstall(result.Session.ID)
	}
	root.ReportDone("installing " + request.Binary)
	return tea.Batch(cleanup, root.Refresh())
}

// Settled applies one status check, retrying the launch the fix held once
// the binary is on PATH.
func (s *State) Settled(root Root, request SettleRequest, result SettleResult, err error) tea.Cmd {
	if s.Pending == nil || s.Pending.SessionID != result.SessionID {
		return nil
	}
	if result.State == uilaunchhint.Running {
		if err != nil {
			root.ReportErr(err.Error())
		}
		return nil
	}
	if err != nil {
		s.Pending = nil
		root.ReportErr(err.Error())
		return nil
	}
	text, failed := uilaunchhint.SettleMessage(result.State, request.Pending.Binary, request.Pending.Name, result.Code)
	if failed {
		s.Pending = nil
		root.ReportErr(text)
		return nil
	}
	if text == "" {
		s.Pending = nil
		return nil
	}
	if request.Pending.Retry != nil {
		root.QueueRetry(request.Pending.Retry)
		s.Pending = nil
		root.ReportDone(text + "; retry queued")
		return nil
	}
	s.Pending = nil
	root.ReportDone(text)
	return nil
}

// CleanupImages owns a frozen copy of paths that no dialog or retry still
// references. The command performs the file deletes without retaining or
// reading the model.
func CleanupImages(images []composer.Attachment) tea.Cmd {
	owned := slices.Clone(images)
	return func() tea.Msg {
		DropImages(owned)
		return nil
	}
}

func DropImages(images []composer.Attachment) {
	for _, att := range images {
		if att.Path != "" {
			_ = os.Remove(att.Path)
		}
	}
}

func removeFiles(statusFile, script string) {
	_ = os.Remove(statusFile)
	_ = os.Remove(script)
}
