package ui

import (
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// connectionsSurface is the add and edit SSH connection card. It owns the
// dialog and the connections its name must not collide with, read when it
// opens; the save runs on the root's effect lane.
type connectionsSurface struct {
	dialog uiconnections.Dialog
	others []remote.Connection
	gen    uint64
	styles spawn.Styles
}

func newConnectionsSurface(editing remote.Connection, others []remote.Connection, gen uint64, styles spawn.Styles) *connectionsSurface {
	s := &connectionsSurface{others: others, gen: gen, styles: styles}
	s.dialog.Open(editing)
	return s
}

// connectionDialogMsg is what a key on the card asked of the root: save a
// connection, or only clear the status line the card is leaving behind.
type connectionDialogMsg struct {
	save *connectionRequest
}

func (m *Model) openConnectionDialog(editing remote.Connection) {
	m.push(newConnectionsSurface(editing, m.ssh.List(), uint64(m.nextComposerGen()), m.spawnStyles()))
	m.clearErr()
}

func (s *connectionsSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	key := s.dialog.Key(typedText(msg), s.others)
	switch key.Action {
	case uiconnections.DialogCancel:
		h.pop()
		return true, func() tea.Msg { return connectionDialogMsg{} }
	case uiconnections.DialogRefused:
		err := refusal(key.Refusal)
		return true, func() tea.Msg { return errMsg{err: err} }
	case uiconnections.DialogSave:
		op := connectionAdd
		if key.Editing != "" {
			op = connectionUpdate
		}
		request := &connectionRequest{op: op, name: key.Editing, next: store.Connection(key.Next), gen: s.gen}
		return true, tea.Batch(key.Cmd, func() tea.Msg { return connectionDialogMsg{save: request} })
	}
	return true, key.Cmd
}

type refusal string

func (r refusal) Error() string { return string(r) }

func (s *connectionsSurface) Click(_ surfaceHost, line int) tea.Cmd {
	s.dialog.Click(line)
	return nil
}

// Wheel: three fields and nothing to scroll.
func (s *connectionsSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

func (s *connectionsSurface) View(h surfaceHost, _ string) string {
	card := s.dialog.View(uiconnections.DialogView{
		Field: func(label, value string, focused bool) string {
			return spawn.FormField(s.styles, label, value, focused)
		},
		Input: textInputView,
		Muted: mutedStyle(),
	})
	cols, _ := h.size()
	return h.cardSized(cardWidthIn(cols), card.Title, card.Body, card.Hints)
}

// Footer is empty: the card paints its own hints.
func (s *connectionsSurface) Footer() [][2]string { return nil }

func (s *connectionsSurface) Owner() inputOwner { return ownerManager }

// Dismissible: the fields hold a draft, so a stray click keeps it.
func (s *connectionsSurface) Dismissible() bool { return false }

func (s *connectionsSurface) Leave(surfaceHost) {}
