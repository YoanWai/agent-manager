package ui

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
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

func (connectionRequest) EffectRequest() {}

func (request connectionRequest) copied() effectRequest { return request }

func (connectionRequest) effectName() string { return "SSH connection save" }

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
	s, dialogOpen := surfaceOf[*uiconnections.Surface](m)
	dialogOpen = dialogOpen && s.Gen() == request.gen
	if err != nil {
		if dialogOpen {
			s.Dialog().SaveFailed(s.Dialog().Gen())
		}
		m.reportErr("connection not saved: " + err.Error())
		return nil
	}
	if dialogOpen {
		m.Pop()
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

func (remoteRequest) EffectRequest() {}

func (request remoteRequest) copied() effectRequest { return request }

func (remoteRequest) effectName() string { return "call to an SSH connection" }

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
			m.clearAccepted(request.composerGen, request.draft, request.closeAfterSend)
		}
		m.reportErr(err.Error())
		return m.pollConnection(request.host)
	}
	on := " on " + request.host
	switch request.op {
	case remoteType:
		m.clearAccepted(request.composerGen, request.draft, request.closeAfterSend)
		m.reportDone("sent to " + request.name + on)
	case remoteSpawn:
		if request.fromForm {
			if _, ok := m.formFor(request.composerGen); ok {
				m.closeForm()
			}
		} else {
			m.clearAccepted(request.composerGen, request.draft, request.closeAfterSend)
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
		if s, ok := m.groupFormSurface(); ok && s.Gen() == request.composerGen {
			m.Pop()
		}
		m.reportDone("created group " + request.name + on)
	}
	return m.pollConnection(request.host)
}
