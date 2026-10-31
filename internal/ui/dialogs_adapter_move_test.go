package ui

import (
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiapply "github.com/YoanWai/agent-manager/internal/ui/dialogs/apply"
	tea "github.com/charmbracelet/bubbletea"
)

func moveOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.MoveSurface](m)
	return ok
}

// movingSession is the session the open card is placing.
func movingSession(m *Model) string {
	if s, ok := surfaceOf[*uidialogs.MoveSurface](m); ok {
		return s.SessionID()
	}
	return ""
}

func openMoveFor(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "wanderer", t.TempDir(), "")
	m.selectSessionRow(t, "wanderer")
	m.openMove()
	if !moveOpen(m) {
		t.Fatalf("test setup did not open the card, err %q", m.errBar.text)
	}
	return m
}

func TestMoveClosesOnEscAndBackdrop(t *testing.T) {
	for name, close := range map[string]func(*Model) *Model{
		"esc":      func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return m },
		"backdrop": func(m *Model) *Model { return clickBackdrop(t, m) },
	} {
		m := openMoveFor(t)
		if !mustTop(t, m).Dismissible() {
			t.Fatalf("%s: the move card holds no draft and must be dismissible", name)
		}
		m = close(m)
		if moveOpen(m) || m.effects.Quitting || !m.onList() {
			t.Errorf("%s: open=%t quitting=%t screen = %v, want the card closed and the manager running", name, moveOpen(m), m.effects.Quitting, m.screenID())
		}
	}
}

// A close follow-up from a card that was popped, or replaced by a newer
// one, leaves the screen alone.
func TestStaleMoveCloseIsDropped(t *testing.T) {
	m := openMoveFor(t)
	stale := uiapply.MoveClose{Gen: m.gens.dialog}
	m.Pop()
	m.applyMoveClose(stale)
	if moveOpen(m) || len(m.surfaces) != 0 {
		t.Fatal("a stale close touched the stack")
	}
	m.openMove()
	m.applyMoveClose(stale)
	if !moveOpen(m) {
		t.Fatal("a close from an older card closed the newer one")
	}
	m.applyMoveClose(uiapply.MoveClose{Gen: m.gens.dialog})
	if moveOpen(m) {
		t.Fatal("the close of the open card did not close it")
	}
}

func TestMoveArchivedSessionEnterPreservesCurrentGroup(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "hidden/deep", "target")
	createSession(t, m, "kept", t.TempDir(), "hidden/deep")
	sess := sessionRow(t, m, "kept").sess
	if err := m.services.store.SetArchived(sess.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetGroupArchived("hidden", true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, "kept")
	m.openMove()
	if got := m.selectedGroupPath(); got != "hidden/deep" {
		t.Errorf("initial group=%q", got)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.services.store.Get(sess.ID)
	if err != nil || got.Group != "hidden/deep" {
		t.Fatalf("move=%+v err=%v", got, err)
	}
	m.selectSessionRow(t, "kept")
	m.openMove()
	pickGroup(t, m, "target")
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err = m.services.store.Get(sess.ID)
	if err != nil || got.Group != "target" || !got.Archived {
		t.Fatalf("explicit move=%+v err=%v", got, err)
	}
}

func TestMoveArchivedNestedTerminalEnterPreservesParent(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "hidden/deep")
	createSession(t, m, "parent", t.TempDir(), "hidden/deep")
	m.selectSessionRow(t, "parent")
	child := spawnTerminal(t, m)
	for _, sess := range m.workspace.sessions {
		if err := m.services.store.SetArchived(sess.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.services.store.SetGroupArchived("hidden", true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectSessionRow(t, child.Name)
	m.openMove()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	got, err := m.services.store.Get(child.ID)
	if err != nil || got.Group != child.Group || got.ParentID != child.ParentID || !got.Archived {
		t.Fatalf("move=%+v err=%v", got, err)
	}
}

func TestMoveArchivedGroupEnterPreservesCurrentParent(t *testing.T) {
	m := buildModel(t)
	seedGroups(t, m, "hidden/deep")
	if err := m.services.store.SetGroupArchived("hidden", true); err != nil {
		t.Fatal(err)
	}
	m.rail.SetArchived(true)
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "hidden/deep")
	m.openMove()
	if got := m.selectedGroupPath(); got != "hidden" {
		t.Fatalf("initial parent=%q", got)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.applyCmd(t, cmd)
	groups, err := m.services.store.Groups()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, group := range groups {
		if group.Name == "hidden/deep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("group moved: %v", groups)
	}
}
