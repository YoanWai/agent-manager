package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/federation"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func nativeFixture(m *Model) {
	m.federation = &nativeFederation{localName: "Mac", remoteNames: map[string]bool{"floripa": true}, hosts: []federation.HostSnapshot{{Name: "floripa", Groups: []string{"property"}, Rows: []federation.Row{
		{Ref: federation.Ref{Host: "floripa", ID: "same"}, Name: "remote-agent", Tool: "claude", Group: "property", Directory: "/srv/project", Status: "idle"},
		{Ref: federation.Ref{Host: "floripa", ID: "shell"}, Name: "rails", Tool: "terminal", Terminal: true, ParentID: "same", Group: "property", Directory: "/srv/project", Status: "idle"},
	}}}}
}

func TestNativeFederationKeepsLocalIDsAndRemoteParentsSeparate(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	msg := refreshMsg{sessions: []store.Session{{ID: "same", Name: "local-agent", Group: "brain"}}, groups: []string{"brain"}}
	m.mergeFederation(&msg)
	if msg.sessions[0].ID != "same" || msg.sessions[1].ID != "floripa::same" || msg.sessions[2].ParentID != "floripa::same" {
		t.Fatalf("host boundaries lost: %+v", msg.sessions)
	}
	m.sessions, m.groups = msg.sessions, msg.groups
	m.rebuildRows()
	if m.rows[0].group != "" {
		t.Fatal("local root changed")
	}
	var parent, child int
	for i, row := range m.rows {
		if row.sess.ID == "floripa::same" {
			parent = i
		}
		if row.sess.ID == "floripa::shell" {
			child = i
		}
	}
	if child != parent+1 || m.rows[child].depth != m.rows[parent].depth+1 {
		t.Fatalf("remote terminal not nested: %+v", m.rows)
	}
	if _, err := m.store.Get("floripa::same"); err == nil {
		t.Fatal("remote record persisted locally")
	}
}

func TestNativeFederationKeepsOfflineRowsAndRefusesActions(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.acceptRemoteSnapshots([]federation.HostSnapshot{{Name: "floripa", Error: "connection refused"}})
	if len(m.federation.hosts[0].Rows) != 2 || m.federation.hosts[0].Rows[0].Status != "unreachable" {
		t.Fatal("offline rows lost or shown as fresh")
	}
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Group: "floripa/property"}}}
	handled, cmd := m.guardRemoteAction(keybind.Revive)
	if !handled || cmd != nil || !strings.Contains(m.errBar.text, "unreachable") {
		t.Fatal("offline action not refused")
	}
}

func TestNativeFederationBlocksLocalActionsOnRemoteRows(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Group: "floripa/property"}}}
	for _, action := range []string{keybind.Delete, keybind.Move, keybind.Editor, keybind.Restart} {
		if handled, cmd := m.guardRemoteAction(action); !handled || cmd != nil {
			t.Errorf("%s would fall through to local implementation", action)
		}
	}
}

func TestNativeFederationAbsentConfigPreservesOrdinaryUI(t *testing.T) {
	m := buildModel(t)
	if err := m.EnableFederation(t.TempDir()); err != nil || m.federation != nil {
		t.Fatalf("optional config: %v", err)
	}
}

func TestNativeFederationTerminalRejectsQuickPrompt(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::shell", Name: "rails", Tool: "terminal", Group: "floripa/property"}}}
	m.openQuickMode()
	m.quick.input.SetValue("check the server")
	_, cmd := m.submitQuick()
	if cmd != nil || !strings.Contains(m.errBar.text, "shell") {
		t.Fatal("terminal accepted a prose prompt")
	}
}

func TestNativeFederationFocusQueuesLiteralTextAndNamedKeysInOrder(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.federation.inputBusy = true
	ref := federation.Ref{Host: "floripa", ID: "same"}
	m.remoteFocusKey(ref, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Enter")})
	m.remoteFocusKey(ref, tea.KeyMsg{Type: tea.KeyEnter})
	m.remoteFocusKey(ref, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line\nline"), Paste: true})
	queue := m.federation.inputQueue
	if len(queue) != 3 || queue[0].text != "Enter" || len(queue[0].keys) != 0 || queue[1].keys[0] != "Enter" || !queue[2].paste {
		t.Fatalf("input ordering/type lost: %+v", queue)
	}
}

func TestNativeFederationFocusUsesNativePaneAndDetach(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Tool: "claude", Group: "floripa/property"}}}
	_, cmd := m.focusSelected()
	if m.mode != modeFocus || cmd == nil {
		t.Fatal("remote did not open native focused pane")
	}
	m.handleFocusKey(tea.KeyMsg{Type: tea.KeyCtrlQ})
	if m.mode != modeList {
		t.Fatal("remote detach did not return to list")
	}
}

func TestStartupWithoutTmuxServerDoesNotApplyBindings(t *testing.T) {
	driver, err := tmux.NewWithSocket(fmt.Sprintf("amnone%x", time.Now().UnixNano()))
	if err != nil {
		t.Skip(err)
	}
	m := &Model{tmux: driver}
	if msg := m.refreshExistingSessionUX(); msg != nil {
		t.Fatalf("empty startup should be quiet: %+v", msg)
	}
}

func TestNativeFederationRemoteFormDoesNotValidatePathsOnMac(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.groups = []string{"floripa", "floripa/property"}
	m.groupPaths = map[string]string{"floripa/property": "/srv/remote-only-project"}
	m.rows = []treeRow{{isGroup: true, group: "floripa/property"}}
	m.openForm()
	if m.form.dir.Value() != "/srv/remote-only-project" {
		t.Fatal("remote default replaced by Mac directory")
	}
	_, cmd := m.submitForm()
	if cmd == nil || m.errBar.text != "" {
		t.Fatalf("remote directory checked locally: %s", m.errBar.text)
	}
}

func TestNativeFederationRemoteTerminalUsesRemoteTransport(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Cwd: "/srv/remote-only", Group: "floripa/property"}}}
	_, cmd := m.openTerminal()
	if cmd == nil || m.errBar.text != "" {
		t.Fatalf("remote terminal attempted local directory: %s", m.errBar.text)
	}
}

func TestUnknownRemoteTimestampDoesNotInventAge(t *testing.T) {
	if got := relSince(time.Time{}); got != "time unknown" {
		t.Fatalf("unknown time rendered as %q", got)
	}
}

func TestNativeFederationKeepsDraftWhenRemoteQueueFails(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.openQuickMode()
	m.quick.input.SetValue("keep my message")
	m.federation.promptBusy = true
	m.handleMsg(remotePromptResultMsg{text: "keep my message", err: errors.New("offline")})
	if m.quick.message() != "keep my message" || m.federation.promptBusy {
		t.Fatal("failed message lost its draft or stayed locked")
	}
}

func TestNativeFederationLocalHostFoldsWithoutChangingLocalGroups(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.sessions = []store.Session{{ID: "local", Group: "brain"}, {ID: "floripa::same", Group: "floripa/property"}}
	m.groups = []string{"brain", "floripa/property"}
	m.rebuildRows()
	m.cursor = 0
	m.toggleCollapse()
	for _, row := range m.rows {
		if row.sess.ID == "local" {
			t.Fatal("local session remained visible under folded Mac")
		}
	}
	if m.sessions[0].Group != "brain" || !m.rows[0].isRoot() {
		t.Fatal("folding changed native local identities")
	}
}

func TestRemoteFocusMouseSelectionAndCopyUsesNativeRouter(t *testing.T) {
	m := paneAt(t, "alpha beta")
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Group: "floripa/property"}}}
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 10, Y: 5})
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 15, Y: 5})
	_, cmd := m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 15, Y: 5})
	if m.selectionText() != "alpha" || cmd == nil {
		t.Fatalf("remote drag/copy lost: text=%q copy=%v", m.selectionText(), cmd != nil)
	}
}

func TestRemoteFocusEntryClearsPreviousPaneInteractionState(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Group: "floripa/property"}}}
	m.pane.forID = "old"
	m.pane.mouse = true
	m.pane.history = 100
	m.focusScroll = 30
	m.focusFetchInFlight = true
	m.focusSelected()
	if m.focusScroll != 0 || m.focusFetchInFlight || m.pane.mouse || m.pane.history != 0 || !m.cursorOn {
		t.Fatal("remote focus inherited previous pane state")
	}
}

func TestRemotePaneMetadataRoutesHistoryAndPreservesScrolledFrame(t *testing.T) {
	m := paneAt(t, "live")
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Group: "floripa/property"}}}
	m.handleMsg(remotePaneMsg{id: "floripa::same", state: federation.PaneState{Output: "live\n", CursorX: 3, CursorY: 0, CursorVisible: true, History: 50, Width: 40, Height: 1}})
	_, cmd := m.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 11, Y: 5})
	if m.focusScroll != focusScrollStep || cmd == nil || !m.pane.cursor.ok || m.pane.cursor.x != 3 {
		t.Fatal("remote metadata did not enable history/cursor")
	}
	m.preview = "scrolled\n"
	m.handleMsg(remotePaneMsg{id: "floripa::same", state: federation.PaneState{Output: "new live\n", History: 50, Width: 40, Height: 1}})
	if m.preview != "scrolled\n" {
		t.Fatal("live refresh overwrote historical viewport")
	}
}

func TestRemoteMouseAppWheelAndClickUseGuardedMouseQueue(t *testing.T) {
	m := paneAt(t, "hello")
	nativeFixture(m)
	m.rows = []treeRow{{sess: store.Session{ID: "floripa::same", Group: "floripa/property"}}}
	m.pane.mouse = true
	m.pane.sgr = true
	m.federation.inputBusy = true
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 11, Y: 5})
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 11, Y: 5})
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 11, Y: 5})
	queue := m.federation.inputQueue
	if len(queue) != 2 || !queue[0].mouse || !queue[1].mouse || !strings.Contains(queue[0].text, "[<64;") || !strings.Contains(queue[1].text, "m") {
		t.Fatalf("mouse events bypassed guarded transport: %+v", queue)
	}
}

func TestRemoteResizeMatchesPreviewWidthWithoutShrinkingHistory(t *testing.T) {
	m := buildModel(t)
	nativeFixture(m)
	m.width = 120
	m.height = 40
	width, _ := m.paneTargetSize()
	m.queueRemoteResize("floripa::same", federation.PaneState{Width: 20, Height: 100})
	queue := m.federation.inputQueue
	if len(queue) != 1 || queue[0].width != width || queue[0].height != 100 {
		t.Fatalf("resize did not preserve height: %+v", queue)
	}
	m.queueRemoteResize("floripa::same", federation.PaneState{Width: 20, Height: 100})
	if len(m.federation.inputQueue) != 1 {
		t.Fatal("duplicate resize queued")
	}
}
