package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// confirmSurface is the yes/no card in front of a lifecycle call, a remote
// call or a connection removal. It holds the request the answer runs, so
// popping it is dropping the request.
type confirmSurface struct {
	confirmTarget
	glyphMode presentation.GlyphMode
}

// confirmHost is what the card reaches on the root beyond the stack.
type confirmHost interface {
	pop()
	confirmCard(title, question, consequence string, destructive bool, answer string) string
	runConfirmed(target confirmTarget) tea.Cmd
}

func newConfirmSurface(target confirmTarget, glyphMode presentation.GlyphMode) *confirmSurface {
	return &confirmSurface{confirmTarget: target, glyphMode: glyphMode}
}

// listClear is the list with no card over it: what "mode is the list" meant
// while the cards were modes. The quick bar and a resize sit on the list
// without covering it.
func (m *Model) listClear() bool {
	if m.base != baseList {
		return false
	}
	for _, s := range m.surfaces {
		switch s.(type) {
		case *quickSurface, *resizeSurface:
		default:
			return false
		}
	}
	return true
}

func (m *Model) openConfirm(target confirmTarget) {
	m.push(newConfirmSurface(target, m.prefs.glyphMode))
}

func (s *confirmSurface) prompt() uidialogs.Confirm {
	return uidialogs.Confirm{Action: s.action, Group: s.isGroup, Label: s.label}
}

func (s *confirmSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	switch s.prompt().Key(msg) {
	case uidialogs.ConfirmCancel:
		h.pop()
	case uidialogs.ConfirmAccept:
		target := s.confirmTarget
		target.sessions = append(target.sessions[:0:0], target.sessions...)
		h.pop()
		return true, h.(confirmHost).runConfirmed(target)
	}
	return true, nil
}

// Click does nothing: the painted keys take the clicks.
func (s *confirmSurface) Click(surfaceHost, int) tea.Cmd { return nil }

func (s *confirmSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

func (s *confirmSurface) View(h surfaceHost, _ string) string {
	prompt := s.prompt()
	question, consequence := uidialogs.SplitLabel(prompt.Label)
	return h.(confirmHost).confirmCard(prompt.Title(s.glyphMode), question, consequence, prompt.Destructive(), prompt.Answer())
}

func (s *confirmSurface) Footer() [][2]string { return uidialogs.ConfirmHints(s.prompt().Answer()) }

func (s *confirmSurface) Owner() inputOwner { return ownerManager }

// Dismissible: a stray click or ctrl+c is the cancel, there is no draft.
func (s *confirmSurface) Dismissible() bool { return true }

func (s *confirmSurface) Leave(surfaceHost) {}
