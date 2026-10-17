package spawn

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var savedRows = []Saved{{Name: "coordinator", Text: "lead"}, {Name: "reviewer", Text: "review"}}

func savedStyles() Styles {
	return Styles{Input: func(in textinput.Model) string { return in.View() }}
}

// Typing filters by name, the arrows move, and enter picks what the
// cursor is on.
func TestSavedListFiltersAndPicks(t *testing.T) {
	list := OpenSavedList(savedRows, false, false)
	list.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rev")})
	lines := list.Lines(40, 6, savedStyles())
	if len(lines) != 2 || !strings.Contains(lines[1], "reviewer") {
		t.Fatalf("filtered list = %q", lines)
	}
	_, pick := list.Key(tea.KeyMsg{Type: tea.KeyEnter})
	if !pick.Picked || pick.Index != 1 || pick.Saved != savedRows[1] || list.Open() {
		t.Fatalf("enter picked %+v, open %v", pick, list.Open())
	}
}

// The form's list offers none first, and a click picks the line it lands on.
func TestSavedListOffersNoneAndTakesClicks(t *testing.T) {
	list := OpenSavedList(savedRows, false, true)
	lines := list.Lines(40, 6, savedStyles())
	if len(lines) != 4 || !strings.Contains(lines[1], "none") {
		t.Fatalf("list = %q", lines)
	}
	if pick := list.Click(0); pick.Picked {
		t.Fatal("a click on the filter picked")
	}
	if pick := list.Click(1); !pick.Picked || pick.Index != -1 {
		t.Fatalf("a click on none picked %+v", pick)
	}
	list = OpenSavedList(savedRows, false, true)
	list.Lines(40, 6, savedStyles())
	if pick := list.Click(3); pick.Index != 1 {
		t.Fatalf("a click on the last line picked %+v", pick)
	}
}

// A list opened before its read says it is loading, takes no pick, and
// shows a failed read.
func TestSavedListWaitsForItsRead(t *testing.T) {
	list := OpenSavedList(nil, true, false)
	if lines := list.Lines(40, 6, savedStyles()); !strings.Contains(lines[0], "loading") {
		t.Fatalf("lines = %q", lines)
	}
	if _, pick := list.Key(tea.KeyMsg{Type: tea.KeyEnter}); pick.Picked || !list.Open() {
		t.Fatal("enter picked before the read answered")
	}
	list.Load(nil, errors.New("disk I/O error"))
	if lines := list.Lines(40, 6, savedStyles()); !strings.Contains(lines[0], "disk I/O error") {
		t.Fatalf("lines = %q", lines)
	}
	if _, pick := list.Key(tea.KeyMsg{Type: tea.KeyEsc}); !pick.Closed || list.Open() {
		t.Fatal("esc left the list open")
	}
}

// The form's saved prompts row steps with the arrows, opens its list with
// ctrl+o, and its pick rides the spawn.
func TestFormPicksASavedPrompt(t *testing.T) {
	h := newFakeFormHost()
	d := NewForm(h, []string{"plain"}, 0)
	if slices.Contains(d.Fields(h), FieldSaved) {
		t.Fatal("the row takes focus before the catalog is read")
	}
	d.SetSaved(savedRows, nil)
	d.FocusField(h, FieldSaved)
	d.HandleKey(h, tea.KeyMsg{Type: tea.KeyRight})
	d.HandleKey(h, tea.KeyMsg{Type: tea.KeyRight})
	if d.SavedText() != "review" {
		t.Fatalf("two steps right picked %q", d.SavedText())
	}
	d.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlO})
	d.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if d.SavedText() != "" {
		t.Fatalf("enter on none kept %q", d.SavedText())
	}
	d.stepSaved(1)
	h.prompt.text = "  task  "
	request := d.Submit(h)
	if request.Spawn == nil || request.Spawn.Saved != "lead" || request.Spawn.Prompt != "task" {
		t.Fatalf("spawn = %+v", request.Spawn)
	}
}
