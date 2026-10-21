package rail

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func hostsSnapshot(state HostState) Snapshot {
	return Snapshot{
		Groups: []string{"work"},
		Sessions: []Session{
			{ID: "local", Name: "local", Status: "idle", CreatedAt: time.Now()},
			{ID: "near", Name: "near", Group: "work", Status: "idle", CreatedAt: time.Now()},
		},
		Hosts: []Host{{
			Name:   "box",
			State:  state,
			Groups: []string{"ops"},
			Sessions: []Session{
				{ID: "far", Name: "far", Tool: "claude", Status: "working"},
				{ID: "deep", Name: "deep", Tool: "codex", Group: "ops", Status: "waiting"},
				{ID: "shell", Name: "shell", Tool: "terminal", Group: "ops", ParentID: "deep", IsShell: true, Status: "idle"},
			},
		}},
	}
}

func hostsContext(width, height int) RenderContext {
	return RenderContext{
		Width: width, Height: height, TerminalWidth: 80, TerminalHeight: 24,
		ListKeys: keybind.DefaultList(),
		Theme: Theme{
			Bg: "#101010", Surface: "#202020", Overlay: "#303030", Border: "#404040",
			Bright: "#ffffff", Text: "#dddddd", Dim: "#aaaaaa", Subtle: "#777777",
			Accent: "#00aaaa", Accent2: "#008888", Remote: "#cc66aa", Working: "#ffaa00",
			Waiting: "#aa88ff", Finished: "#00aa00", Errored: "#ff0000", Idle: "#777777",
		},
	}
}

func runes(key string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

func rowKeys(m Model) []string {
	var keys []string
	for _, row := range m.rows {
		keys = append(keys, strings.Repeat(" ", row.depth)+row.key())
	}
	return keys
}

func TestConnectionRowsNestUnderTheConnectionAfterTheLocalTree(t *testing.T) {
	model := New(nil)
	model.Reconcile(hostsSnapshot(HostOnline))
	want := []string{
		"g:", "s:local", "g:work", " s:near",
		"c:box", " rs:box::far", " rg:box::ops", "  rs:box::deep", "   rs:box::shell",
	}
	if got := rowKeys(model); !slices.Equal(got, want) {
		t.Fatalf("rows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, row := range model.Rows() {
		if remote := strings.Contains(row.Selection.key(), "box"); remote != row.Remote() {
			t.Errorf("row %+v: Remote() = %v", row, row.Remote())
		}
	}
}

func TestRemoteSelectionIsNeverALocalSessionOrGroup(t *testing.T) {
	model := New(nil)
	model.Reconcile(hostsSnapshot(HostOnline))
	if !model.Focus(Selection{Kind: SessionRow, SessionID: "far", Host: "box"}) {
		t.Fatal("remote session row not found")
	}
	if _, ok := model.SelectedSession(); ok {
		t.Error("a remote session reads as a local selected session")
	}
	selected, _ := model.Selected()
	if selected.Host != "box" || selected.SessionID != "far" {
		t.Errorf("selected = %+v", selected)
	}
	model.Focus(Selection{Kind: GroupRow, Group: "ops", Host: "box"})
	if _, ok := model.SelectedGroup(); ok {
		t.Error("a remote group reads as a local selected group")
	}
	if model.FocusSession("far") {
		t.Error("FocusSession found a local session on a remote row")
	}
	if ids := model.ListedSessionIDs(); !slices.Equal(ids, []string{"local", "near"}) {
		t.Errorf("listed local sessions = %v", ids)
	}
}

func TestConnectionRowFoldsLikeAGroup(t *testing.T) {
	model := New(nil)
	model.Reconcile(hostsSnapshot(HostOnline))
	model.Focus(Selection{Kind: ConnectionRow, Host: "box"})
	ctx := KeyContext{ListKeys: keybind.DefaultList()}
	decision := model.Key(tea.KeyMsg{Type: tea.KeyEnter}, Frame{}, ctx)
	if len(decision.Mutations) != 1 || !slices.Contains(decision.Mutations[0].HostCollapsed, HostFold{Host: "box"}) || len(decision.Mutations[0].Collapsed) != 0 {
		t.Fatalf("fold decision = %+v", decision)
	}
	if got := rowKeys(model); got[len(got)-1] != "c:box" {
		t.Fatalf("folded connection still shows rows: %v", got)
	}
	if !model.SelectionCollapsed(Selection{Kind: ConnectionRow, Host: "box"}) {
		t.Error("connection does not report folded")
	}
	model.Key(tea.KeyMsg{Type: tea.KeyEnter}, Frame{}, ctx)
	model.Focus(Selection{Kind: GroupRow, Group: "ops", Host: "box"})
	model.Key(tea.KeyMsg{Type: tea.KeyEnter}, Frame{}, ctx)
	if model.collapsed["ops"] || !model.hostFolds[HostFold{Host: "box", Path: "ops"}] {
		t.Errorf("remote group fold landed on %v", model.Collapsed())
	}
}

func TestLocalGroupNamedLikeARemotePathFoldsAlone(t *testing.T) {
	snapshot := hostsSnapshot(HostOnline)
	snapshot.Groups = append(snapshot.Groups, "box::ops")
	snapshot.Sessions = append(snapshot.Sessions, Session{ID: "twin", Name: "twin", Group: "box::ops", Status: "idle", CreatedAt: time.Now()})
	model := New(nil)
	model.Reconcile(snapshot)
	ctx := KeyContext{ListKeys: keybind.DefaultList()}
	model.Focus(Selection{Kind: GroupRow, Group: "ops", Host: "box"})
	model.Key(tea.KeyMsg{Type: tea.KeyEnter}, Frame{}, ctx)
	if model.SelectionCollapsed(Selection{Kind: GroupRow, Group: "box::ops"}) {
		t.Error("folding the remote group folded the local one")
	}
	if !model.SelectionCollapsed(Selection{Kind: GroupRow, Group: "ops", Host: "box"}) {
		t.Error("remote group did not fold")
	}
	model.Reconcile(snapshot)
	if selected, _ := model.Selected(); selected.Host != "box" || selected.Group != "ops" {
		t.Errorf("refresh moved the cursor to %+v", selected)
	}
	model.Focus(Selection{Kind: GroupRow, Group: "box::ops"})
	decision := model.Key(tea.KeyMsg{Type: tea.KeyEnter}, Frame{}, ctx)
	if !slices.Equal(decision.Mutations[0].Collapsed, []string{"box::ops"}) || !slices.Equal(decision.Mutations[0].HostCollapsed, []HostFold{{Host: "box", Path: "ops"}}) {
		t.Errorf("saved folds = %+v", decision.Mutations[0])
	}
	restored := New(decision.Mutations[0].Collapsed)
	restored.RestoreHostFolds(decision.Mutations[0].HostCollapsed)
	if !restored.folded("", "box::ops") || !restored.folded("box", "ops") || restored.folded("box", "") {
		t.Errorf("restored folds: local %v, remote %v", restored.Collapsed(), restored.HostCollapsed())
	}
}

func TestRemoteRowsRefuseReorder(t *testing.T) {
	model := New(nil)
	model.Reconcile(hostsSnapshot(HostOnline))
	ctx := KeyContext{ListKeys: keybind.DefaultList()}
	for _, selection := range []Selection{
		{Kind: ConnectionRow, Host: "box"},
		{Kind: SessionRow, SessionID: "far", Host: "box"},
		{Kind: GroupRow, Group: "ops", Host: "box"},
	} {
		model.Focus(selection)
		decision := model.Key(runes("K"), Frame{}, ctx)
		if len(decision.Mutations) != 0 || !strings.Contains(decision.Error, "isn't available on SSH connections yet") {
			t.Errorf("%+v: decision = %+v", selection, decision)
		}
	}
	// The last local root session stepping down must not swap with the
	// remote root session that shares its empty group.
	model.Focus(Selection{Kind: SessionRow, SessionID: "local"})
	if decision := model.Key(runes("J"), Frame{}, ctx); len(decision.Mutations) != 0 {
		t.Errorf("local session swapped across the host boundary: %+v", decision.Mutations)
	}
	model.Focus(Selection{Kind: SessionRow, SessionID: "near", Group: "work"})
	model.enterReorder(model.rows[model.cursor].key())
	if _, ok := model.resolveDrop(Selection{Kind: GroupRow, Group: "ops", Host: "box"}); ok {
		t.Error("a local session can be dropped into a remote group")
	}
	if _, ok := model.resolveDrop(Selection{Kind: ConnectionRow, Host: "box"}); ok {
		t.Error("a local session can be dropped onto a connection")
	}
}

func TestConnectionRowRendersItsLabelAndState(t *testing.T) {
	for _, tc := range []struct {
		state HostState
		want  string
	}{
		{HostConnecting, "connecting…"},
		{HostOffline, "unreachable"},
		{HostOnline, "◐ 1  ◆ 1"},
	} {
		model := New(nil)
		snapshot := hostsSnapshot(tc.state)
		snapshot.Hosts[0].Trouble = "unreachable"
		model.Reconcile(snapshot)
		frame := model.Render(hostsContext(48, 14), Frame{})
		var line string
		for _, painted := range frame.Lines {
			if painted.HitOK && painted.Hit.Kind == ConnectionRow {
				line = ansi.Strip(painted.Text)
			}
		}
		if !strings.Contains(line, "box  ssh") || !strings.Contains(line, tc.want) {
			t.Errorf("state %d: connection line %q, want %q", tc.state, line, tc.want)
		}
		if _, grip := frame.Handles["c:box"]; grip {
			t.Error("a connection row offers a reorder grip")
		}
		if _, grip := frame.Handles["rs:box::far"]; grip {
			t.Error("a remote session row offers a reorder grip")
		}
	}
}

func TestConnectionMenusOfferOnlyRemoteActions(t *testing.T) {
	model := New(nil)
	model.Reconcile(hostsSnapshot(HostOnline))
	labels := func(selection Selection) []string {
		index := model.rowIndex(selection.key())
		var got []string
		for _, item := range model.rowMenuItems(model.rows[index]) {
			if item.label != "" {
				got = append(got, item.label)
			}
		}
		return got
	}
	if got := labels(Selection{Kind: ConnectionRow, Host: "box"}); !slices.Contains(got, "Remove connection") || !slices.Contains(got, "Edit connection") {
		t.Errorf("connection menu = %v", got)
	}
	session := labels(Selection{Kind: SessionRow, SessionID: "far", Host: "box"})
	for _, refused := range []string{"Rename", "Move to group", "Fork", "Review changes", "Open in editor", "Restart", "Delete"} {
		if slices.Contains(session, refused) {
			t.Errorf("remote session menu offers %s: %v", refused, session)
		}
	}
	if !slices.Contains(session, "Kill") || !slices.Contains(session, "Attach") {
		t.Errorf("remote session menu = %v", session)
	}
	// The host archives and revives agents only.
	if got := labels(Selection{Kind: SessionRow, SessionID: "shell", Group: "ops", Host: "box"}); !slices.Equal(got, []string{"Attach", "Kill"}) {
		t.Errorf("remote terminal menu = %v, want Attach and Kill", got)
	}
	if got := labels(Selection{Kind: GroupRow}); !slices.Contains(got, "New connection") {
		t.Errorf("root menu = %v, want New connection", got)
	}
}

func TestHostFoldsFollowTheirConnection(t *testing.T) {
	model := New([]string{"work"})
	model.RestoreHostFolds([]HostFold{{Host: "box"}, {Host: "box", Path: "ops"}, {Host: "lab", Path: "ops"}})
	decision := model.RenameHost("box", "gpu")
	want := []HostFold{{Host: "gpu"}, {Host: "gpu", Path: "ops"}, {Host: "lab", Path: "ops"}}
	if got := decision.Mutations[0].HostCollapsed; !slices.Equal(got, want) {
		t.Fatalf("after rename: %+v, want %+v", got, want)
	}
	decision = model.ForgetHost("gpu")
	if got := decision.Mutations[0].HostCollapsed; !slices.Equal(got, []HostFold{{Host: "lab", Path: "ops"}}) {
		t.Fatalf("after removal: %+v", got)
	}
	if got := decision.Mutations[0].Collapsed; !slices.Equal(got, []string{"work"}) {
		t.Fatalf("local folds changed: %v", got)
	}
}
