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
