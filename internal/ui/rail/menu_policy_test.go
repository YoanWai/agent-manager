package rail

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func TestGroupMenuReturnsTypedGroupIntent(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{
		Groups:   []string{"work"},
		Sessions: []Session{{ID: "agent", Name: "agent", Group: "work"}},
	})
	model.Focus(Selection{Kind: GroupRow})

	decision := model.openMenu(Selection{Kind: GroupRow, Group: "work"}, 4, 5, false)
	if !decision.SelectionChanged || decision.SelectionEffect != SelectionEffectPreview {
		t.Fatalf("open decision = %+v, want preview selection", decision)
	}

	rename := -1
	for index, item := range model.menu.items {
		if item.action == Attach {
			t.Fatal("group menu offered attach")
		}
		if item.action == RenameAction {
			rename = index
		}
	}
	if rename < 0 {
		t.Fatal("group menu omitted rename")
	}

	decision = model.runMenuItem(rename)
	if decision.Intent.Kind != RenameAction || decision.Intent.Target != (Selection{Kind: GroupRow, Group: "work"}) {
		t.Fatalf("rename decision = %+v", decision)
	}
}

func TestArchivedToggleWaitsForFreshInventory(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Sessions: []Session{{ID: "active", Name: "active"}}})

	decision := model.action(keybind.Archived, KeyContext{})
	if !decision.Refresh || !model.ShowArchived() {
		t.Fatalf("archive toggle = %+v showArchived=%v", decision, model.ShowArchived())
	}
	if rows := model.Rows(); len(rows) != 2 || rows[1].SessionID != "active" {
		t.Fatalf("toggle rebuilt stale inventory: %+v", rows)
	}
}

func TestPendingEndMenuOffersCancelOnLiveAndDeadRows(t *testing.T) {
	for _, state := range []string{"idle", "dead"} {
		model := New(nil)
		model.Reconcile(Snapshot{Sessions: []Session{{ID: "agent", Name: "agent", Status: state, AfterTurn: "kill", AfterTurnGlyph: "■"}}})
		selection := Selection{Kind: SessionRow, SessionID: "agent"}
		model.Focus(selection)
		model.openMenu(selection, 0, 0, false)
		cancel := -1
		for index, item := range model.menu.items {
			if item.label == "Cancel kill" && item.action == CancelEnd {
				cancel = index
			}
		}
		if cancel < 0 {
			t.Fatalf("%s row menu has no Cancel kill: %+v", state, model.menu.items)
		}
		if decision := model.runMenuItem(cancel); decision.Intent.Kind != CancelEnd || decision.Intent.Target != selection {
			t.Fatalf("%s cancel decision = %+v", state, decision)
		}
	}
}

func TestCancelEndKeyReturnsTypedIntent(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Sessions: []Session{{ID: "agent", Name: "agent", AfterTurn: "archive"}}})
	model.Focus(Selection{Kind: SessionRow, SessionID: "agent"})
	if decision := model.action(keybind.CancelEnd, KeyContext{}); decision.Intent.Kind != CancelEnd {
		t.Fatalf("cancel key decision = %+v", decision)
	}
}

func TestGroupAndRootMenusOfferQuickPromptMode(t *testing.T) {
	model := New(nil)
	model.Reconcile(Snapshot{Groups: []string{"work"}})
	for _, selection := range []Selection{{Kind: GroupRow, Group: "work"}, {Kind: GroupRow}} {
		model.openMenu(selection, 0, 0, false)
		if len(model.menu.items) == 0 || model.menu.items[0].label != "Quick prompt mode" || model.menu.items[0].action != Prompt {
			t.Fatalf("menu for %+v = %+v, want quick prompt mode first", selection, model.menu.items)
		}
	}
}

func TestArchivedRowsOfferRestoreWithoutRelaunch(t *testing.T) {
	model := New(nil)
	model.SetArchived(true)
	model.Reconcile(Snapshot{Groups: []string{"work"}, Sessions: []Session{{ID: "agent", Name: "agent", Group: "work", Archived: true, Status: "dead"}}})
	for _, selection := range []Selection{{Kind: GroupRow, Group: "work"}, {Kind: SessionRow, SessionID: "agent", Group: "work"}} {
		if !model.Focus(selection) {
			t.Fatalf("missing archived selection: %+v", selection)
		}
		for _, action := range []string{keybind.Revive, keybind.ReviveAll, keybind.Restart, keybind.Kill, keybind.KillAll} {
			if decision := model.action(action, KeyContext{}); !decision.Consumed || decision.Intent.Kind != NoAction {
				t.Fatalf("archived %v %s = %+v", selection, action, decision)
			}
		}
		model.openMenu(selection, 0, 0, false)
		restore := -1
		for index, item := range model.menu.items {
			if item.action == Revive || item.action == ReviveAll || item.action == Restart {
				t.Fatalf("archived menu offers relaunch: %+v", model.menu.items)
			}
			if item.action == Restore {
				restore = index
			}
		}
		if restore < 0 {
			t.Fatalf("archived menu missing restore: %+v", model.menu.items)
		}
		if decision := model.runMenuItem(restore); decision.Intent.Kind != Restore {
			t.Fatalf("restore = %+v", decision)
		}
		legend := model.RowLegend(LegendContext{ListKeys: keybind.DefaultList(), Row: selection, RowOK: true})
		for _, pair := range legend.Pairs {
			if pair[1] == "revive" || pair[1] == "restart" {
				t.Fatalf("archived legend offers relaunch: %+v", legend)
			}
		}
	}
}

func TestArchivedRowsKeepManagementKeysMenusAndFooter(t *testing.T) {
	model := New(nil)
	model.SetArchived(true)
	model.Reconcile(Snapshot{Groups: []string{"work"}, Sessions: []Session{{ID: "agent", Name: "agent", Group: "work", Archived: true, Status: "dead"}}})
	keys := keybind.DefaultList()
	for _, selection := range []Selection{{Kind: GroupRow, Group: "work"}, {Kind: SessionRow, SessionID: "agent", Group: "work"}} {
		if !model.Focus(selection) {
			t.Fatalf("missing archived selection: %+v", selection)
		}
		legend := model.RowLegend(LegendContext{ListKeys: keys, Row: selection, RowOK: true})
		for _, action := range []struct {
			key   string
			kind  ActionKind
			label string
		}{
			{keybind.Editor, OpenEditor, "editor"},
			{keybind.Rename, RenameAction, "rename"},
			{keybind.Move, MoveToGroup, "move"},
			{keybind.Restore, Restore, "restore"},
		} {
			if decision := model.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(keys.Binding(action.key).Label())}, Frame{}, KeyContext{ListKeys: keys}); decision.Intent.Kind != action.kind || decision.Intent.Target != selection {
				t.Fatalf("archived %+v key %s = %+v", selection, action.key, decision)
			}
			model.openMenu(selection, 0, 0, false)
			index := -1
			for i, item := range model.menu.items {
				if item.action == action.kind {
					index = i
				}
			}
			if index < 0 {
				t.Fatalf("archived %+v menu missing %s: %+v", selection, action.key, model.menu.items)
			}
			if decision := model.runMenuItem(index); decision.Intent.Kind != action.kind || decision.Intent.Target != selection {
				t.Fatalf("archived %+v menu %s = %+v", selection, action.key, decision)
			}
			found := false
			for _, pair := range legend.Pairs {
				if pair[1] == action.label && pair[0] == keys.Binding(action.key).Glyph("/") {
					found = true
				}
			}
			if !found {
				t.Fatalf("archived %+v footer missing %s: %+v", selection, action.key, legend.Pairs)
			}
		}
	}
}

func TestArchivedCreationActionsKeepNewSessionAvailable(t *testing.T) {
	model := New(nil)
	model.SetArchived(true)
	model.Reconcile(Snapshot{Sessions: []Session{{ID: "archived", Name: "archived", Archived: true}}})
	model.Focus(Selection{Kind: SessionRow, SessionID: "archived"})
	for _, action := range []string{keybind.Fork, keybind.Terminal, keybind.Prompt} {
		if decision := model.action(action, KeyContext{}); decision.Intent.Kind != NoAction {
			t.Errorf("archived %s admitted intent %+v", action, decision.Intent)
		}
	}
	if decision := model.action(keybind.NewSession, KeyContext{}); decision.Intent.Kind != NewSession {
		t.Fatal("new session must remain available")
	}
}

func TestRootMenuOffersOnlyApplicableActions(t *testing.T) {
	for _, archived := range []bool{false, true} {
		model := New(nil)
		model.SetArchived(archived)
		model.Reconcile(Snapshot{Sessions: []Session{{ID: "live", Name: "live", Status: "working", Archived: archived}, {ID: "dead", Name: "dead", Status: "dead", Archived: archived}}})
		model.openMenu(Selection{Kind: GroupRow}, 0, 0, false)
		offered := map[ActionKind]bool{}
		for _, item := range model.menu.items {
			offered[item.action] = true
		}
		if !offered[NewSession] || !offered[OpenEditor] {
			t.Fatal("root menu omitted valid global creation/editor action")
		}
		if archived {
			for _, action := range []ActionKind{NewTerminal, Prompt, Fork, Kill, Revive} {
				if offered[action] {
					t.Errorf("archived root menu offers %v", action)
				}
			}
		} else if !offered[Kill] || !offered[Revive] {
			t.Fatal("active root menu omitted valid lifecycle actions")
		}
	}
}

func TestArchivedRemoteMenusMatchContextualCreationKeys(t *testing.T) {
	for _, selection := range []Selection{{Kind: ConnectionRow, Host: "box"}, {Kind: GroupRow, Host: "box", Group: "ops"}, {Kind: SessionRow, Host: "box", SessionID: "deep", Group: "ops"}} {
		model := New(nil)
		snapshot := hostsSnapshot(HostOnline)
		for i := range snapshot.Hosts[0].Sessions {
			snapshot.Hosts[0].Sessions[i].Archived = true
		}
		model.SetArchived(true)
		model.Reconcile(snapshot)
		if !model.Focus(selection) {
			t.Fatalf("remote target not visible: %+v", selection)
		}
		model.openMenu(selection, 0, 0, false)
		offered := map[ActionKind]bool{}
		for _, item := range model.menu.items {
			offered[item.action] = true
		}
		if offered[NewTerminal] || offered[Prompt] {
			t.Errorf("archived remote menu offers contextual creation: %+v", selection)
		}
		model.menu = rowMenu{}
		for _, action := range []string{keybind.Terminal, keybind.Prompt} {
			if got := model.action(action, KeyContext{}); got.Intent.Kind != NoAction {
				t.Errorf("archived remote key %s admitted action: %+v", action, selection)
			}
		}
		if selection.Kind != SessionRow && !offered[NewSession] {
			t.Errorf("remote global new session omitted: %+v", selection)
		}
	}
}
