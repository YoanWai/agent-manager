package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	cfName = iota
	cfDestination
	cfCount
)

// connectionDialog adds an SSH connection or edits one: its two fields,
// keys, view and validation, and the connectionRequest enter builds. The
// root opens it and runs the request on the effect lane.
type connectionDialog struct {
	name        textinput.Model
	destination textinput.Model
	focus       int
	// editing is the name of the connection being edited; empty adds one.
	editing string
	// gen tells this dialog from the one opened before it, so a save that
	// completes late cannot close a dialog the user reopened.
	gen uint64
}

// connectionDialogHost is what the dialog reaches on the root.
type connectionDialogHost interface {
	reportErr(text string)
	clearErr()
	setMode(next mode)
	connectionList() []remote.Connection
	card(title, body string, hint [][2]string) string
}

var _ connectionDialogHost = (*Model)(nil)

func (m *Model) connectionList() []remote.Connection { return m.ssh.list }

// open starts the dialog empty, or filled from the connection it edits.
func (d *connectionDialog) open(h connectionDialogHost, editing remote.Connection) {
	name := textField("build-box", 40)
	destination := textField("user@host or an ~/.ssh/config alias", 255)
	name.SetValue(editing.Name)
	destination.SetValue(editing.Destination)
	name.CursorEnd()
	destination.CursorEnd()
	name.Focus()
	*d = connectionDialog{name: name, destination: destination, editing: editing.Name, gen: d.gen + 1}
	h.clearErr()
	h.setMode(modeConnection)
}

func (d *connectionDialog) handleKey(h connectionDialogHost, msg tea.KeyMsg) (tea.Cmd, *connectionRequest) {
	msg = typedText(msg)
	switch msg.String() {
	case "esc":
		h.clearErr()
		h.setMode(modeList)
		return nil, nil
	case "tab", "down":
		d.focusStep(1)
		return nil, nil
	case "shift+tab", "up":
		d.focusStep(-1)
		return nil, nil
	case "enter":
		return nil, d.submit(h)
	}
	var cmd tea.Cmd
	if d.focus == cfName {
		d.name, cmd = d.name.Update(msg)
	} else {
		d.destination, cmd = d.destination.Update(msg)
	}
	return cmd, nil
}

func (d *connectionDialog) focusStep(delta int) {
	d.focus = (d.focus + delta + cfCount) % cfCount
	d.name.Blur()
	d.destination.Blur()
	if d.focus == cfName {
		d.name.Focus()
	} else {
		d.destination.Focus()
	}
}

// submit validates the connection against the others it would sit beside.
func (d *connectionDialog) submit(h connectionDialogHost) *connectionRequest {
	next := remote.Connection{Name: strings.TrimSpace(d.name.Value()), Destination: strings.TrimSpace(d.destination.Value())}
	var others []remote.Connection
	for _, conn := range h.connectionList() {
		if conn.Name != d.editing {
			others = append(others, conn)
		}
	}
	if err := remote.ValidateConnection(next, others); err != nil {
		h.reportErr(err.Error())
		return nil
	}
	op := connectionAdd
	if d.editing != "" {
		op = connectionUpdate
	}
	return &connectionRequest{op: op, name: d.editing, next: store.Connection(next), gen: d.gen}
}

func (d *connectionDialog) view(h connectionDialogHost) string {
	title := "⇄ New SSH Connection"
	if d.editing != "" {
		title = "⇄ Edit SSH Connection"
	}
	body := formField("name", textInputView(d.name), d.focus == cfName) +
		formField("ssh to", textInputView(d.destination), d.focus == cfDestination) + "\n" +
		mutedStyle.Render("  Its sessions show under this name. The host needs agent-manager on") + "\n" +
		mutedStyle.Render("  its login PATH and a key this machine's ssh can use without a prompt.")
	return h.card(title, body, [][2]string{{"tab/↑↓", "move"}, {"↵", "save"}, {"esc", "cancel"}})
}

func (m *Model) openConnectionDialog(editing remote.Connection) {
	m.ssh.dialog.open(m, editing)
}

func (m *Model) handleConnectionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.ssh.dialog.handleKey(m, msg)
	if request == nil {
		return m, cmd
	}
	m.clearErr()
	m.enqueueEffect(*request, 0, false)
	return m, tea.Batch(cmd, m.nextEffectCmd())
}
