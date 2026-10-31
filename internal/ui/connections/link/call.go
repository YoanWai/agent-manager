package link

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	tea "github.com/charmbracelet/bubbletea"
)

type Op uint8

const (
	Type Op = iota + 1
	Spawn
	NewTerminal
	Kill
	CloseTerminal
	Revive
	Archive
	Restore
	CreateGroup
)

type Call struct {
	Op       Op
	Host     string
	Ref      remote.Ref
	Name     string
	Text     string
	Spawn    sessioncmd.CreateSessionOptions
	Terminal sessioncmd.CreateTerminalOptions
	// GroupDir is the directory a new group on the host defaults to; Name
	// carries its path.
	GroupDir string
	// Draft and ComposerGen let a send or spawn from the quick bar, or a
	// spawn from the New Session form, clear what asked for it.
	Draft          string
	ComposerGen    int
	CloseAfterSend bool
	FromForm       bool
	Dead           bool
}

// Result is what a call returned: the session or terminal it made or
// changed.
type Result struct {
	Session sessioncmd.Session
}

func Run(ctx context.Context, client *remote.Client, openers remote.Openers, call Call) (Result, error) {
	var result Result
	var err error
	switch call.Op {
	case Type:
		result.Session, err = client.Type(ctx, call.Ref, call.Text)
	case Spawn:
		result.Session, err = client.Spawn(ctx, call.Host, call.Spawn)
	case NewTerminal:
		var term sessioncmd.Terminal
		term, err = client.CreateTerminal(ctx, call.Host, call.Terminal)
		result.Session = sessioncmd.Session{ID: term.ID, Name: term.Name}
	case Kill:
		result.Session, err = client.Kill(ctx, call.Ref)
	case CloseTerminal:
		err = client.Terminals(openers).CloseForUser(ctx, call.Ref)
	case Revive:
		result.Session, err = client.Revive(ctx, call.Ref)
	case Archive, Restore:
		result.Session, err = client.Archive(ctx, call.Ref, call.Op == Restore)
	case CreateGroup:
		_, err = client.CreateGroup(ctx, call.Host, call.Name, call.GroupDir)
	}
	return result, err
}

func (s *State) ApplyCall(root Root, call Call, result Result, err error) tea.Cmd {
	if err != nil {
		// As on this machine, a prompt that may sit in the pane leaves the
		// bar, so the next enter cannot type it a second time.
		if call.Op == Type && remote.MayHaveReached(err) {
			root.ClearAccepted(call.ComposerGen, call.Draft, call.CloseAfterSend)
		}
		root.ReportErr(err.Error())
		return s.PollHost(call.Host)
	}
	on := " on " + call.Host
	switch call.Op {
	case Type:
		root.ClearAccepted(call.ComposerGen, call.Draft, call.CloseAfterSend)
		root.ReportDone("sent to " + call.Name + on)
	case Spawn:
		if call.FromForm {
			root.CloseAsker(call.ComposerGen, FormAsker)
		} else {
			root.ClearAccepted(call.ComposerGen, call.Draft, call.CloseAfterSend)
		}
		root.ReportDone("started " + result.Session.Name + on)
	case NewTerminal:
		root.ReportDone("opened terminal " + result.Session.Name + on)
	case Kill, CloseTerminal:
		root.ReportDone("killed " + call.Name + on)
	case Revive:
		root.ReportDone("revived " + call.Name + on)
	case Archive:
		root.ReportDone("archived " + call.Name + on)
	case Restore:
		root.ReportDone("restored " + call.Name + on)
	case CreateGroup:
		root.CloseAsker(call.ComposerGen, GroupFormAsker)
		root.ReportDone("created group " + call.Name + on)
	}
	return s.PollHost(call.Host)
}
