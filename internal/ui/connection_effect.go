package ui

import (
	"context"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

type connectionOp uint8

const (
	connectionAdd connectionOp = iota + 1
	connectionUpdate
	connectionRemove
)

// connectionRequest writes the connection list. name is the connection an
// update or removal targets; gen is the dialog that asked, if any.
type connectionRequest struct {
	op   connectionOp
	name string
	next store.Connection
	gen  uint64
}

func (connectionRequest) effectRequest() {}

// connectionEffectResult is the list as the store holds it after the
// write, read back in the same job.
type connectionEffectResult struct {
	connections []store.Connection
}

func (connectionEffectResult) effectResult() {}

func (s effectServices) runConnection(request connectionRequest) (effectResult, error) {
	var err error
	switch request.op {
	case connectionAdd:
		err = s.store.AddConnection(request.next)
	case connectionUpdate:
		err = s.store.UpdateConnection(request.name, request.next)
	case connectionRemove:
		err = s.store.DeleteConnection(request.name)
	}
	if err != nil {
		return connectionEffectResult{}, err
	}
	connections, err := s.store.Connections()
	return connectionEffectResult{connections: connections}, err
}

func (m *Model) applyConnectionEffect(request connectionRequest, result connectionEffectResult, err error) tea.Cmd {
	dialogOpen := m.mode == modeConnection && m.ssh.dialog.gen == request.gen
	if err != nil {
		m.reportErr("connection not saved: " + err.Error())
		return nil
	}
	if dialogOpen {
		m.mode = modeList
	}
	m.ssh.setList(result.connections)
	m.rebuildRows()
	switch request.op {
	case connectionAdd, connectionUpdate:
		m.rail.Focus(uirail.Selection{Kind: uirail.ConnectionRow, Host: request.next.Name})
		m.reportDone("connecting to " + request.next.Name + " over SSH")
		return m.pollConnections()
	default:
		m.reportDone("removed " + request.name + "; its sessions keep running on that host")
	}
	return nil
}

type remoteOp uint8

const (
	remoteSend remoteOp = iota + 1
	remoteSpawn
	remoteNewTerminal
	remoteKill
	remoteCloseTerminal
	remoteRevive
	remoteArchive
	remoteRestore
)

// remoteRequest is one call to a connection's CLI. Nothing it does is
// written to the local store.
type remoteRequest struct {
	op       remoteOp
	host     string
	ref      remote.Ref
	name     string
	text     string
	from     string
	spawn    sessioncmd.CreateSessionOptions
	terminal sessioncmd.CreateTerminalOptions
	// draft and composerGen let a send or spawn from the quick bar, or a
	// spawn from the New Session form, clear what asked for it.
	draft          string
	composerGen    int
	closeAfterSend bool
	fromForm       bool
}

func (remoteRequest) effectRequest() {}

type remoteEffectResult struct {
	sent    sessioncmd.SendResult
	session sessioncmd.Session
}

func (remoteEffectResult) effectResult() {}

func (s effectServices) runRemote(request remoteRequest) (effectResult, error) {
	ctx := context.Background()
	var result remoteEffectResult
	var err error
	switch request.op {
	case remoteSend:
		result.sent, err = s.remote.Send(ctx, request.ref, request.text, request.from)
	case remoteSpawn:
		result.session, err = s.remote.Spawn(ctx, request.host, request.spawn)
	case remoteNewTerminal:
		var term sessioncmd.Terminal
		term, err = s.remote.CreateTerminal(ctx, request.host, request.terminal)
		result.session = sessioncmd.Session{ID: term.ID, Name: term.Name}
	case remoteKill:
		result.session, err = s.remote.Kill(ctx, request.ref)
	case remoteCloseTerminal:
		err = s.remote.TerminalClose(ctx, request.ref)
	case remoteRevive:
		result.session, err = s.remote.Revive(ctx, request.ref)
	case remoteArchive, remoteRestore:
		result.session, err = s.remote.Archive(ctx, request.ref, request.op == remoteRestore)
	}
	return result, err
}

func (m *Model) applyRemoteEffect(request remoteRequest, result remoteEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return m.pollConnection(request.host)
	}
	on := " on " + request.host
	switch request.op {
	case remoteSend:
		m.quick.clearAccepted(quickSendRequest{composerGen: request.composerGen, draft: request.draft, closeAfterSend: request.closeAfterSend})
		if result.sent.ManagerAwake {
			m.reportDone("sent to " + request.name + on)
		} else {
			m.reportWarn(fmt.Sprintf("queued for %s%s; it arrives once a manager runs there", request.name, on))
		}
	case remoteSpawn:
		if request.fromForm {
			if m.mode == modeForm && m.form.prompt.gen == request.composerGen {
				m.mode = modeList
			}
		} else {
			m.quick.clearAccepted(quickSendRequest{composerGen: request.composerGen, draft: request.draft, closeAfterSend: request.closeAfterSend})
		}
		m.reportDone("started " + result.session.Name + on)
	case remoteNewTerminal:
		m.reportDone("opened terminal " + result.session.Name + on)
	case remoteKill, remoteCloseTerminal:
		m.reportDone("killed " + request.name + on)
	case remoteRevive:
		m.reportDone("revived " + request.name + on)
	case remoteArchive:
		m.reportDone("archived " + request.name + on)
	case remoteRestore:
		m.reportDone("restored " + request.name + on)
	}
	return m.pollConnection(request.host)
}
