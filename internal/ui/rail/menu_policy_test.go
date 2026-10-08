package rail

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
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
