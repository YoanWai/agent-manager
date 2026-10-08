package ui

import (
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
		m.reportDone("saved connection " + request.next.Name)
	default:
		m.reportDone("removed " + request.name + "; its sessions keep running on that host")
	}
	return nil
}
