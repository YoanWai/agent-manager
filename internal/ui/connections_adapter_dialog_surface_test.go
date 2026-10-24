package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) connectionSurface() *uiconnections.Surface {
	s, ok := surfaceOf[*uiconnections.Surface](m)
	if !ok {
		panic("the connection card is not open")
	}
	return s
}

func (m *Model) connectionDialogOpen() bool {
	_, ok := surfaceOf[*uiconnections.Surface](m)
	return ok
}

func openConnectionModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 40
	m.ssh = uilink.New(testRemoteClient(t), nil)
	m.applyTestMsg(t, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if !m.connectionDialogOpen() {
		t.Fatal("the new connection key did not open the card")
	}
	return m
}

// esc closes the card and the status line it leaves is clear.
func TestConnectionCardEscCloses(t *testing.T) {
	m := openConnectionModel(t)
	m.reportErr("stale")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyTestMsg(t, cmd())
	if m.connectionDialogOpen() || len(m.surfaces) != 0 {
		t.Fatalf("esc left %d surfaces", len(m.surfaces))
	}
	if m.errBar.text != "" {
		t.Fatalf("status = %q, want it cleared", m.errBar.text)
	}
}

// The card holds typed fields, so a stray click keeps the draft.
func TestConnectionCardKeepsItsDraft(t *testing.T) {
	m := openConnectionModel(t)
	if top, _ := m.top(); top.Dismissible() {
		t.Fatal("a card holding fields says a stray click closes it")
	}
	typeConnection(m, "box", "me@box")
	m = clickBackdrop(t, m)
	if !m.connectionDialogOpen() {
		t.Fatal("a click off the card closed it")
	}
}

// A save that completes after its card closed, or after another card
// replaced it, neither closes nor releases the card on screen.
func TestConnectionSaveCompletionIsFenced(t *testing.T) {
	m := openConnectionModel(t)
	stale := connectionRequest{op: uilink.SaveAdd, next: store.Connection{Name: "box", Destination: "me@box"}, gen: m.connectionSurface().Gen()}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyConnectionEffect(stale, connectionEffectResult{}, nil)
	if m.connectionDialogOpen() {
		t.Fatal("a completion reopened a closed card")
	}
	m.openConnectionDialog(remote.Connection{})
	m.applyConnectionEffect(stale, connectionEffectResult{}, nil)
	if !m.connectionDialogOpen() {
		t.Fatal("a stale completion closed the card that replaced it")
	}
}
