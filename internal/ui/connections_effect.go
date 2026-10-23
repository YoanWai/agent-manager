package ui

import (
	"context"

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
	dialogOpen := m.mode == modeConnection && m.ssh.Dialog.Gen() == request.gen
	if err != nil {
		if dialogOpen {
			m.ssh.Dialog.SaveFailed(request.gen)
		}
		m.reportErr("connection not saved: " + err.Error())
		return nil
	}
	if dialogOpen {
		m.mode = modeList
	}
	m.ssh.setList(result.connections)
	m.rebuildRows()
	switch {
	case request.op == connectionUpdate && request.name != request.next.Name:
		m.applyRailStateDecision(m.rail.RenameHost(request.name, request.next.Name))
	case request.op == connectionRemove:
		m.applyRailStateDecision(m.rail.ForgetHost(request.name))
	}
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
	remoteType remoteOp = iota + 1
	remoteSpawn
	remoteNewTerminal
	remoteKill
	remoteCloseTerminal
	remoteRevive
	remoteArchive
	remoteRestore
	remoteCreateGroup
)

// remoteRequest is one call to a connection's CLI.
type remoteRequest struct {
	op       remoteOp
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

func (remoteRequest) effectRequest() {}

type remoteEffectResult struct {
	session sessioncmd.Session
}

func (remoteEffectResult) effectResult() {}

func (s effectServices) runRemote(request remoteRequest) (effectResult, error) {
	ctx := context.Background()
	var result remoteEffectResult
	var err error
	switch request.op {
	case remoteType:
		result.session, err = s.remote.Type(ctx, request.ref, request.text)
	case remoteSpawn:
		result.session, err = s.remote.Spawn(ctx, request.host, request.spawn)
	case remoteNewTerminal:
		var term sessioncmd.Terminal
		term, err = s.remote.CreateTerminal(ctx, request.host, request.terminal)
		result.session = sessioncmd.Session{ID: term.ID, Name: term.Name}
	case remoteKill:
		result.session, err = s.remote.Kill(ctx, request.ref)
	case remoteCloseTerminal:
		err = s.remote.Terminals(s.store).CloseForUser(ctx, request.ref)
	case remoteRevive:
		result.session, err = s.remote.Revive(ctx, request.ref)
	case remoteArchive, remoteRestore:
		result.session, err = s.remote.Archive(ctx, request.ref, request.op == remoteRestore)
	case remoteCreateGroup:
		_, err = s.remote.CreateGroup(ctx, request.host, request.name, request.groupDir)
	}
	return result, err
}

func (m *Model) applyRemoteEffect(request remoteRequest, result remoteEffectResult, err error) tea.Cmd {
	if err != nil {
		// As on this machine, a prompt that may sit in the pane leaves the
		// bar, so the next enter cannot type it a second time.
		if request.op == remoteType && remote.MayHaveReached(err) {
			m.quick.clearAccepted(quickSendRequest{composerGen: request.composerGen, draft: request.draft, closeAfterSend: request.closeAfterSend})
		}
		m.reportErr(err.Error())
		return m.pollConnection(request.host)
	}
	on := " on " + request.host
	switch request.op {
	case remoteType:
		m.quick.clearAccepted(quickSendRequest{composerGen: request.composerGen, draft: request.draft, closeAfterSend: request.closeAfterSend})
		m.reportDone("sent to " + request.name + on)
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
	case remoteCreateGroup:
		if m.mode == modeGroupForm && uint64(m.groupForm.Gen()) == uint64(request.composerGen) {
			m.mode = modeList
		}
		m.reportDone("created group " + request.name + on)
	}
	return m.pollConnection(request.host)
}
