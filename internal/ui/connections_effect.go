package ui

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// connectionRequest writes the connection list; uilink.Save does the write.
type connectionRequest struct {
	op   uilink.SaveOp
	name string
	next store.Connection
	gen  uint64
}

func (request connectionRequest) EffectCopy() uieffects.Request { return request }

func (connectionRequest) EffectName() string { return "SSH connection save" }

func (request connectionRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runConnection(request)
}

// connectionEffectResult is the list as the store holds it after the
// write, read back in the same job.
type connectionEffectResult struct {
	connections []store.Connection
}

func (connectionEffectResult) EffectResult() {}

func (result connectionEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyConnectionEffect(job.Request.(connectionRequest), result, err)
}

func (s effectServices) runConnection(request connectionRequest) (uieffects.Result, error) {
	connections, err := uilink.Save(s.store, saveRequest(request))
	return connectionEffectResult{connections: connections}, err
}

func (m *Model) applyConnectionEffect(request connectionRequest, result connectionEffectResult, err error) tea.Cmd {
	return m.ssh.ApplySave(linkRoot{m}, saveRequest(request), result.connections, err)
}

func saveRequest(request connectionRequest) uilink.SaveRequest {
	return uilink.SaveRequest{Op: request.op, Name: request.name, Next: request.next, Gen: request.gen}
}

// remoteRequest is one call to a connection's CLI; uilink.Run makes it.
type remoteRequest struct {
	op       uilink.Op
	host     string
	ref      remote.Ref
	name     string
	text     string
	spawn    sessioncmd.CreateSessionOptions
	terminal sessioncmd.CreateTerminalOptions
	// groupDir is the directory a new group on the host defaults to; name
	// carries its path.
	groupDir string
	// draft and composerGen let a send or spawn from the quick bar, or a
	// spawn from the New Session form, clear what asked for it.
	draft          string
	composerGen    int
	closeAfterSend bool
	fromForm       bool
	// dead is a kill of an agent that has already exited there.
	dead bool
}

func (request remoteRequest) EffectCopy() uieffects.Request { return request }

func (remoteRequest) EffectName() string { return "call to an SSH connection" }

func (request remoteRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runRemote(request)
}

type remoteEffectResult struct {
	session sessioncmd.Session
}

func (remoteEffectResult) EffectResult() {}

func (result remoteEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyRemoteEffect(job.Request.(remoteRequest), result, err)
}

func (s effectServices) runRemote(request remoteRequest) (uieffects.Result, error) {
	result, err := uilink.Run(context.Background(), s.remote, s.store, call(request))
	return remoteEffectResult{session: result.Session}, err
}

func (m *Model) applyRemoteEffect(request remoteRequest, result remoteEffectResult, err error) tea.Cmd {
	return m.ssh.ApplyCall(linkRoot{m}, call(request), uilink.Result{Session: result.session}, err)
}

func call(request remoteRequest) uilink.Call {
	return uilink.Call{
		Op: request.op, Host: request.host, Ref: request.ref, Name: request.name, Text: request.text,
		Spawn: request.spawn, Terminal: request.terminal, GroupDir: request.groupDir,
		Draft: request.draft, ComposerGen: request.composerGen, CloseAfterSend: request.closeAfterSend,
		FromForm: request.fromForm, Dead: request.dead,
	}
}
