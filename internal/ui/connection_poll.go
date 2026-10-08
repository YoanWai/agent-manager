package ui

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/remote"
	tea "github.com/charmbracelet/bubbletea"
)

// connectionRefreshMsg is one host's refresh, for the list generation that
// started it.
type connectionRefreshMsg struct {
	host  string
	gen   uint64
	state remote.HostState
}

// pollConnections refreshes every connection that has no refresh in
// flight. It rides the workspace poll, off the update path.
func (m *Model) pollConnections() tea.Cmd {
	var commands []tea.Cmd
	for _, conn := range m.ssh.list {
		commands = append(commands, m.pollConnection(conn.Name))
	}
	return tea.Batch(commands...)
}

func (m *Model) pollConnection(host string) tea.Cmd {
	c := &m.ssh
	if _, listed := c.connection(host); !listed || c.polling[host] {
		return nil
	}
	c.polling[host] = true
	client, gen := c.client, c.gen
	return func() tea.Msg {
		return connectionRefreshMsg{host: host, gen: gen, state: client.Refresh(context.Background(), host)}
	}
}

func (m *Model) applyConnectionRefresh(msg connectionRefreshMsg) tea.Cmd {
	c := &m.ssh
	if msg.gen != c.gen {
		return nil
	}
	delete(c.polling, msg.host)
	c.hosts[msg.host] = msg.state
	m.rebuildRows()
	return m.syncRemotePreview()
}

func (m *Model) routeConnectionMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case connectionRefreshMsg:
		return routed(m, m.applyConnectionRefresh(msg))
	case remotePreviewMsg:
		m.applyRemotePreview(msg)
		return routed(m, nil)
	case remoteAttachReadyMsg:
		return routed(m, m.runRemoteAttach(msg))
	case remoteAttachDoneMsg:
		if msg.err != nil {
			m.reportErr(msg.host + ": attach ended: " + msg.err.Error())
		}
		return routed(m, tea.Batch(m.pollConnection(msg.host), m.readRemotePreview()))
	}
	return nil, nil, false
}
