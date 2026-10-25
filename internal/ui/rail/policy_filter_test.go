package rail

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAttentionFilterMatchesOnlyActionableStatuses(t *testing.T) {
	want := map[string]bool{
		"waiting": true, "finished": true, "errored": true,
		"working": false, "idle": false, "dead": false, "starting": false,
	}
	for state, keep := range want {
		if got := attentionStatus(state); got != keep {
			t.Fatalf("attentionStatus(%q) = %v want %v", state, got, keep)
		}
	}
}

func TestAttentionFilterTogglesBackToAll(t *testing.T) {
	model := New(nil)
	model.SetFilteringAttention(true)
	if !model.FilteringAttention() {
		t.Fatal("attention filter did not become active")
	}
	model.SetFilteringAttention(false)
	if model.FilteringAttention() {
		t.Fatal("attention filter did not return to all")
	}
}

func TestRevealGroupOpensWholeDestinationWithOneFoldSave(t *testing.T) {
	model := New([]string{"work", "work/api"})
	model.Reconcile(Snapshot{Groups: []string{"work", "work/api"}, Sessions: []Session{{ID: "created", Name: "created", Group: "work/api"}}})
	model.SetArchived(true)
	model.SetSearch("unrelated", false)
	model.SetHideEmptyGroups(true)
	decision := model.RevealGroup("work/api")
	if !model.HideEmptyGroups() {
		t.Fatal("session reveal cleared hide-empty preference")
	}
	if model.IsCollapsed("work") || model.IsCollapsed("work/api") {
		t.Fatal("created session destination stays folded")
	}
	if model.ShowArchived() || model.Search() != "" {
		t.Fatal("creation destination stays narrowed")
	}
	if !model.FocusSession("created") {
		t.Fatal("created session cannot be selected after reveal")
	}
	if len(decision.Mutations) != 1 || decision.Mutations[0].Kind != SaveCollapsed {
		t.Fatalf("fold changes = %+v, want one persisted snapshot", decision.Mutations)
	}
}

func TestArchivedFoldAllPreservesActiveFolds(t *testing.T) {
	model := New([]string{"one"})
	model.Reconcile(Snapshot{Groups: []string{"one", "two"}})
	model.SetArchived(true)
	result := model.action(keybind.FoldAll, KeyContext{})
	if len(result.Mutations) != 0 || len(model.Collapsed()) != 1 || model.Collapsed()[0] != "one" {
		t.Fatalf("archived Fold All changed active state: %+v folds=%v", result, model.Collapsed())
	}
	for _, pair := range model.ViewLegend(LegendContext{ListKeys: keybind.DefaultList()}).Pairs {
		if pair[1] == "fold all" || pair[1] == "unfold all" {
			t.Fatalf("archived footer offers folding: %v", pair)
		}
	}
}

func TestArchivedGroupFoldingPreservesActiveFolds(t *testing.T) {
	for _, folded := range []bool{false, true} {
		for _, input := range []string{"enter", "right", "left", "f6", "double click"} {
			t.Run(fmt.Sprintf("folded=%v/%s", folded, input), func(t *testing.T) {
				model := New([]string{"one"})
				model.SetCollapsed("two", folded)
				model.Reconcile(Snapshot{
					Groups: []string{"one", "two"}, ArchivedGroups: map[string]bool{"two": true},
					Sessions: []Session{{ID: "archived", Group: "two", Archived: true}},
				})
				model.SetArchived(true)
				target := Selection{Kind: GroupRow, Group: "two"}
				if !model.Focus(target) {
					t.Fatal("archived group is missing")
				}
				before := slices.Clone(model.Collapsed())
				var result Decision
				if input == "double click" {
					frame := Frame{Width: 40, Lines: []Line{{Hit: target, HitOK: true}}, visible: map[string]bool{target.key(): true}}
					msg := tea.MouseMsg{X: 12, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
					now := time.Now()
					model.Mouse(msg, frame, MouseContext{FullWidth: true, Now: now})
					result = model.Mouse(msg, frame, MouseContext{FullWidth: true, Now: now.Add(time.Millisecond)})
				} else {
					keys := keybind.DefaultList()
					kind := map[string]tea.KeyType{"enter": tea.KeyEnter, "right": tea.KeyRight, "left": tea.KeyLeft, "f6": tea.KeyF6}[input]
					if input == "f6" {
						key, err := keybind.Parse(input)
						if err != nil {
							t.Fatal(err)
						}
						keys = keys.With(keybind.Open, keybind.Keys(key))
					}
					result = model.Key(tea.KeyMsg{Type: kind}, Frame{}, KeyContext{ListKeys: keys, ArrowStep: true})
				}
				if !result.Consumed || len(result.Mutations) != 0 || !slices.Equal(before, model.Collapsed()) {
					t.Fatalf("archived input changed active folds: before=%v after=%v decision=%+v", before, model.Collapsed(), result)
				}
				model.SetArchived(false)
				if !model.Focus(Selection{Kind: GroupRow, Group: "one"}) {
					t.Fatal("active group is missing")
				}
				result = model.action(keybind.Open, KeyContext{})
				if model.IsCollapsed("one") || len(result.Mutations) != 1 || result.Mutations[0].Kind != SaveCollapsed {
					t.Fatalf("active folding stopped working: %+v", result)
				}
			})
		}
	}
}
