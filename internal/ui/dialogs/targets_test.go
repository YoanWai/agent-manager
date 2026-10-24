package dialogs

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
)

// A group cannot land inside its own subtree, and the picker keeps the row
// it had selected.
func TestPruneTargetsDropsTheMovedSubtree(t *testing.T) {
	picker := spawn.Picker{GroupIndex: 2, Groups: []spawn.GroupOption{
		{Path: ""}, {Path: "web"}, {Path: "web/api"}, {Path: "web/api/deep"}, {Path: "ops"},
	}}
	PruneTargets(&picker, "web/api")
	var paths []string
	for _, opt := range picker.Groups {
		paths = append(paths, opt.Path)
	}
	if len(paths) != 3 || paths[0] != "" || paths[1] != "web" || paths[2] != "ops" {
		t.Fatalf("targets = %q, want the root, web and ops", paths)
	}
	if got := picker.GroupPath(); got != "" {
		t.Fatalf("selection = %q, want the root the cursor falls back to", got)
	}
}

// A shell may be nested under each agent in the group it runs in, and the
// cursor lands back on a group row.
func TestNestAgentsPutsEachAgentUnderItsGroup(t *testing.T) {
	picker := spawn.Picker{Groups: []spawn.GroupOption{{Path: ""}, {Path: "web"}}}
	NestAgents(&picker, []Agent{{ID: "s1", Group: "web", Name: "api"}, {ID: "s2", Group: "", Name: "top"}})
	var got []string
	for _, opt := range picker.Groups {
		got = append(got, opt.Path+"/"+opt.Name)
	}
	want := []string{"/", "/top", "web/", "web/api"}
	if len(got) != len(want) {
		t.Fatalf("targets = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("targets = %q, want %q", got, want)
		}
	}
	if picker.Groups[1].SessionID != "s2" || picker.Groups[3].SessionID != "s1" {
		t.Fatalf("agents nested wrong: %+v", picker.Groups)
	}
}

// The picker's index and the stored value round-trip, and an unknown value
// reads as inherit.
func TestWorktreeChoiceRoundTrips(t *testing.T) {
	for index, want := range map[int]string{0: "", 1: "on", 2: "off"} {
		if got := WorktreeValue(index); got != want {
			t.Fatalf("WorktreeValue(%d) = %q, want %q", index, got, want)
		}
		if got := WorktreeIndex(want); got != index {
			t.Fatalf("WorktreeIndex(%q) = %d, want %d", want, got, index)
		}
	}
	if got := WorktreeValue(len(WorktreeOptions)); got != "" {
		t.Fatalf("an index past the list = %q, want inherit", got)
	}
	if got := WorktreeIndex("sometimes"); got != 0 {
		t.Fatalf("an unknown value = %d, want inherit", got)
	}
}

// A submit resolves to the picked row's placement, and a group that would
// land where it already is resolves to nothing.
func TestPlacementOfResolvesThePickedRow(t *testing.T) {
	picker := spawn.Picker{GroupIndex: 1, Groups: []spawn.GroupOption{{Path: ""}, {Path: "web", SessionID: "s1"}}}
	placed, ok := PlacementOf(MoveSession("s2"), &picker)
	if !ok || placed != (Placement{SessionID: "s2", Group: "web", ParentID: "s1"}) {
		t.Fatalf("a session place = %+v, %t", placed, ok)
	}
	grouped := spawn.Picker{GroupIndex: 1, Groups: []spawn.GroupOption{{Path: ""}, {Path: "ops"}}}
	if placed, ok := PlacementOf(MoveGroup("web"), &grouped); !ok || placed != (Placement{GroupPath: "web", Group: "ops"}) {
		t.Fatalf("a group place = %+v, %t", placed, ok)
	}
	atRoot := spawn.Picker{GroupIndex: 0, Groups: []spawn.GroupOption{{Path: ""}}}
	if _, ok := PlacementOf(MoveGroup("web"), &atRoot); ok {
		t.Fatal("a group landing where it already is is not a move")
	}
}
