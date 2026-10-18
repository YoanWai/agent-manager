package quick

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// ctrl+o opens the saved prompts list above the prompt and asks the root
// for the catalog; a pick asks the root to put it at the caret.
func TestQuickBarPicksASavedPrompt(t *testing.T) {
	h := &fakeHost{prompt: &fakePrompt{}, settings: map[string]string{}, selected: true}
	q := NewBar(h, []string{"claude"}, 0, false)
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlO}); !request.LoadSaved || !q.SavedOpen() {
		t.Fatalf("ctrl+o asked for %+v", request)
	}
	q.LoadSaved([]spawn.Saved{{Name: "coordinator", Text: "lead"}, {Name: "reviewer", Text: "review"}}, nil)
	if legend := q.Legend(h); legend[0] != [2]string{"type", "filter"} {
		t.Fatalf("legend while picking = %v", legend)
	}
	st := spawn.Styles{Input: func(in textinput.Model) string { return in.View() }}
	view := q.View(h, Facts{Styles: st}, 40, 6)
	if !strings.Contains(strings.Split(view, "\n")[2], "reviewer") {
		t.Fatalf("the list paints above the prompt:\n%s", view)
	}
	hit, ok := q.HitAt(5, 2)
	if !ok || hit.Action != ClickSaved {
		t.Fatalf("no click on the reviewer line: %+v", hit)
	}
	request := q.Click(h, hit)
	if request.InsertSaved == nil || *request.InsertSaved != (spawn.Saved{Name: "reviewer", Text: "review"}) || q.SavedOpen() {
		t.Fatalf("a click on reviewer asked for %+v", request)
	}
	q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlO})
	if _, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEsc}); request != (Request{}) || q.SavedOpen() || h.prompt.released {
		t.Fatalf("esc in the list asked for %+v", request)
	}
}
