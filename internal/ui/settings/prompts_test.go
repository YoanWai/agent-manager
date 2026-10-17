package settings

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func paste(s *State, ctx Context, text string) Result {
	return s.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}, ctx)
}

func openPromptsOn(t *testing.T, rows ...Prompt) (*State, Context) {
	t.Helper()
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldPrompts
	if result := press(&s, ctx, "enter"); !result.LoadPrompts || !s.Prompts.Open() {
		t.Fatalf("enter on the saved prompts row = %+v", result)
	}
	s.LoadedPrompts(PromptsRead{Gen: s.Prompts.Gen(), Rows: rows})
	return &s, ctx
}

// The panel asks the root for every read and write and keeps what the user
// typed until the write's answer comes back.
func TestPromptsPanelAsksTheRootToSaveAndDelete(t *testing.T) {
	s, ctx := openPromptsOn(t)
	view := viewContext()
	view.InnerWidth = 56
	if !strings.Contains(s.View(view).Body, "No saved prompts.") {
		t.Fatal("an empty catalog says so")
	}
	press(s, ctx, "n")
	paste(s, ctx, "  Role  ")
	press(s, ctx, "tab")
	paste(s, ctx, " \n literal\n\n")
	result := s.Key(tea.KeyMsg{Type: tea.KeyCtrlS}, ctx)
	if result.SavePrompt == nil || *result.SavePrompt != (PromptSave{Prompt: Prompt{Name: "  Role  ", Text: " \n literal\n\n"}}) {
		t.Fatalf("ctrl+s asked for %+v", result.SavePrompt)
	}
	if again := s.Key(tea.KeyMsg{Type: tea.KeyCtrlS}, ctx); again.SavePrompt != nil {
		t.Fatal("a second ctrl+s saved twice")
	}
	if press(s, ctx, "esc"); !s.Prompts.editing {
		t.Fatal("esc dropped an editor whose save is still running")
	}
	s.LoadedPrompts(PromptsRead{Gen: s.Prompts.Gen(), Rows: []Prompt{{Name: "Role", Text: " \n literal\n\n"}}, Mutation: true, Committed: true, Selected: result.SavePrompt.SelectedName()})
	if s.Prompts.editing || s.Prompts.cursor != 0 {
		t.Fatalf("a landed save keeps the editor: %+v", s.Prompts)
	}

	press(s, ctx, "enter")
	if s.Prompts.previous != "Role" || s.Prompts.text.Value() != " \n literal\n\n" {
		t.Fatalf("edit opened on %q / %q", s.Prompts.previous, s.Prompts.text.Value())
	}
	if result := press(s, ctx, "esc"); !result.LoadPrompts || s.Prompts.editing {
		t.Fatalf("esc in the editor = %+v", result)
	}

	s.LoadedPrompts(PromptsRead{Gen: s.Prompts.Gen(), Rows: []Prompt{{Name: "Role", Text: "x"}}})
	if result := press(s, ctx, "d"); result.DeletePrompt != "" || !s.Prompts.confirm {
		t.Fatalf("d deleted without asking: %+v", result)
	}
	if result := press(s, ctx, "enter"); result.DeletePrompt != "Role" {
		t.Fatalf("confirming deleted %q", result.DeletePrompt)
	}
}

// A refused save leaves the editor and the typing in place, and says why.
func TestPromptsPanelKeepsTheEditorWhenASaveIsRefused(t *testing.T) {
	s, ctx := openPromptsOn(t, Prompt{Name: "Taken", Text: "existing"})
	press(s, ctx, "n")
	paste(s, ctx, "Taken")
	s.Key(tea.KeyMsg{Type: tea.KeyCtrlS}, ctx)
	notice := s.LoadedPrompts(PromptsRead{Gen: s.Prompts.Gen(), Err: errors.New("UNIQUE constraint failed"), Mutation: true})
	if !s.Prompts.editing || s.Prompts.busy || s.Prompts.name.Value() != "Taken" || notice.Text == "" {
		t.Fatalf("refused save: editing %v busy %v name %q notice %+v", s.Prompts.editing, s.Prompts.busy, s.Prompts.name.Value(), notice)
	}
}

// A read the panel moved on from changes nothing: an editor opened while
// the list was still loading keeps its typing.
func TestPromptsPanelIgnoresAReadItMovedOnFrom(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldPrompts
	press(&s, ctx, "enter")
	late := s.Prompts.Gen()
	press(&s, ctx, "n")
	paste(&s, ctx, "unsaved")
	s.LoadedPrompts(PromptsRead{Gen: late, Rows: []Prompt{{Name: "old"}}})
	if !s.Prompts.editing || s.Prompts.name.Value() != "unsaved" || len(s.Prompts.rows) != 0 {
		t.Fatalf("a late read replaced the editor: %+v", s.Prompts)
	}
}

// The list scrolls with its cursor and the editor fits, on terminals down
// to a short one.
func TestPromptsPanelFitsSmallTerminals(t *testing.T) {
	var rows []Prompt
	for i := range 30 {
		rows = append(rows, Prompt{Name: fmt.Sprintf("Role %02d", i), Text: "text"})
	}
	for _, size := range [][2]int{{22, 18}, {34, 18}, {34, 22}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, ctx := openPromptsOn(t, rows...)
			view := viewContext()
			view.InnerWidth, view.Height = size[0], size[1]
			for range 29 {
				press(s, ctx, "j")
			}
			frame := s.View(view)
			lines := strings.Split(frame.Body, "\n")
			if !strings.Contains(frame.Body, "Role 29") || len(lines) > size[1]-6 {
				t.Fatalf("list of %d lines lost its cursor:\n%s", len(lines), frame.Body)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) > size[0] {
					t.Fatalf("line wider than %d: %q", size[0], line)
				}
			}
			press(s, ctx, "e")
			frame = s.View(view)
			if len(strings.Split(frame.Body, "\n")) > size[1]-6 || !strings.Contains(fmt.Sprint(frame.Hints), "ctrl+s") {
				t.Fatalf("editor does not fit:\n%s", frame.Body)
			}
		})
	}
}

// Opening a prompt to rename it keeps its text byte for byte, long ones
// included.
func TestPromptsPanelEditorKeepsTextAsWritten(t *testing.T) {
	for _, text := range []string{"literal\nend", strings.Repeat("line\n", 120)} {
		s, ctx := openPromptsOn(t, Prompt{Name: "Literal", Text: text})
		press(s, ctx, "e")
		result := s.Key(tea.KeyMsg{Type: tea.KeyCtrlS}, ctx)
		if result.SavePrompt == nil || result.SavePrompt.Prompt.Text != text || result.SavePrompt.Previous != "Literal" {
			t.Fatalf("save of a %d byte prompt = %+v", len(text), result.SavePrompt)
		}
	}
}
