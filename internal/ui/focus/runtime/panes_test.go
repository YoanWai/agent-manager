package runtime

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// sized is a state holding one session at a known size.
func sized(id string, size [2]int) *State {
	s := &State{}
	s.SetSize(id, size)
	return s
}

func TestResizeTargetsRepinsOnlyWhatMoved(t *testing.T) {
	t.Parallel()
	s := sized("a", [2]int{80, 20})
	s.SetSize("b", [2]int{60, 20})
	plan := Plan{
		Box:         [2]int{80, 20},
		Sessions:    []store.Session{{ID: "a"}, {ID: "b"}},
		Panes:       map[string]tmux.Pane{},
		FitsHeight:  func(string) bool { return false },
		FullFocusID: "",
	}
	targets := s.ResizeTargets(plan)
	if len(targets) != 1 || targets[0].ID != "b" || targets[0].Height != 20 {
		t.Fatalf("targets = %+v, want only b at 20", targets)
	}
}

// A shorter box leaves a pane on the normal screen tall: a height shrink makes
// Codex clear the pane's scrollback (#369).
func TestResizeTargetsKeepATallerPaneWhenTheBoxShrinks(t *testing.T) {
	t.Parallel()
	s := sized("a", [2]int{80, 30})
	plan := Plan{
		Box:        [2]int{80, 20},
		Sessions:   []store.Session{{ID: "a"}},
		Panes:      map[string]tmux.Pane{},
		FitsHeight: func(string) bool { return false },
	}
	targets := s.ResizeTargets(plan)
	if len(targets) != 1 || targets[0].Height != 30 {
		t.Fatalf("targets = %+v, want the pane kept at 30", targets)
	}
}

// A width change of the same taller pane keeps its height too.
func TestResizeTargetsKeepHeightOnAWidthRepin(t *testing.T) {
	t.Parallel()
	s := sized("a", [2]int{60, 30})
	plan := Plan{
		Box:        [2]int{80, 20},
		Sessions:   []store.Session{{ID: "a"}},
		Panes:      map[string]tmux.Pane{},
		FitsHeight: func(string) bool { return false },
	}
	targets := s.ResizeTargets(plan)
	if len(targets) != 1 || targets[0].Height != 30 {
		t.Fatalf("targets = %+v, want a width re-pin at 30", targets)
	}
}

// A tool that fits its height, and a pane on the alternate screen, follow the
// box down: neither holds a scrollback a shrink would clear.
func TestResizeTargetsShrinkWhatFitsItsHeight(t *testing.T) {
	t.Parallel()
	s := sized("fits", [2]int{80, 30})
	s.SetSize("alt", [2]int{80, 30})
	plan := Plan{
		Box:        [2]int{80, 20},
		Sessions:   []store.Session{{ID: "fits", Tool: "codex"}, {ID: "alt"}},
		Panes:      map[string]tmux.Pane{"alt": {AltScreen: true}},
		FitsHeight: func(tool string) bool { return tool == "codex" },
	}
	targets := s.ResizeTargets(plan)
	if len(targets) != 2 {
		t.Fatalf("targets = %+v, want both rows resized", targets)
	}
	for _, target := range targets {
		if target.Height != 20 {
			t.Fatalf("%s height = %d, want the box's 20", target.ID, target.Height)
		}
	}
}

func TestResizeTargetsSkipArchivedAndTheFullScreenPane(t *testing.T) {
	t.Parallel()
	s := &State{}
	plan := Plan{
		Box:         [2]int{80, 20},
		Sessions:    []store.Session{{ID: "archived", Archived: true}, {ID: "full"}, {ID: "shown"}},
		Panes:       map[string]tmux.Pane{},
		FitsHeight:  func(string) bool { return false },
		FullFocusID: "full",
	}
	targets := s.ResizeTargets(plan)
	if len(targets) != 1 || targets[0].ID != "shown" {
		t.Fatalf("targets = %+v, want only the row on the list", targets)
	}
}

// A pin this run still owes is not a measurement, so seeding leaves it owed.
func TestSeedLeavesAnOwedPinAlone(t *testing.T) {
	t.Parallel()
	s := &State{}
	s.PinOwed("fresh")
	s.SetSize("adopted", [2]int{100, 40})
	s.Seed(map[string]tmux.Pane{
		"fresh":   {Width: 80, Height: 24},
		"adopted": {Width: 100, Height: 40},
	})
	if got, _ := s.SizeOf("fresh"); got != [2]int{0, 0} {
		t.Fatalf("fresh pin = %v, want it still owed", got)
	}
	if got, _ := s.SizeOf("adopted"); got != [2]int{100, 40} {
		t.Fatalf("adopted = %v, want the measured size kept", got)
	}
}

// An unsized session takes the listing's geometry, and a split window is taken
// every pass because the agent moved the pane out from under the box.
func TestSeedTakesUnsizedAndSplitWindows(t *testing.T) {
	t.Parallel()
	s := &State{}
	s.SetSize("split", [2]int{100, 40})
	s.Seed(map[string]tmux.Pane{
		"first": {Width: 80, Height: 24},
		"split": {Width: 50, Height: 20, Panes: 2},
	})
	if got, _ := s.SizeOf("first"); got != [2]int{80, 24} {
		t.Fatalf("first = %v, want the listing's 80x24", got)
	}
	if got, _ := s.SizeOf("split"); got != [2]int{50, 20} {
		t.Fatalf("split = %v, want the split window's 50x20", got)
	}
}

// A pane process that changed means a window this process never sized, so the
// session takes the exact pin again.
func TestMarksFreshRepinsAReplacedPane(t *testing.T) {
	t.Parallel()
	s := &State{}
	s.SetPID("revived", 41)
	s.MarksFresh(map[string]tmux.Pane{
		"revived": {PID: 42, Width: 80, Height: 24},
		"new":     {PID: 7, Width: 80, Height: 24},
	})
	if got, sized := s.SizeOf("revived"); !sized || got != [2]int{0, 0} {
		t.Fatalf("revived pin = %v (sized %t), want it owed", got, sized)
	}
	if _, sized := s.SizeOf("new"); sized {
		t.Fatal("a first sighting is not a replacement")
	}
	if got := s.PIDOf("revived"); got != 42 {
		t.Fatalf("pid = %d, want 42", got)
	}
}

func TestWindowTakesRowsUpFromTheBottom(t *testing.T) {
	t.Parallel()
	capture := "one\ntwo\nthree\nfour\nfive\n"
	if got := Window(capture, 2, 1); got != "four\nfive\n" {
		t.Fatalf("Window = %q, want the last two rows", got)
	}
	if got := Window(capture, 2, 4); got != "one\n" {
		t.Fatalf("Window = %q, want the clamped first row", got)
	}
}
