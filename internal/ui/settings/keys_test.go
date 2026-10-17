package settings

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

func testContext() Context {
	return Context{
		Tools:   Tools{Agents: []string{"claude", "codex"}, Known: map[string]bool{"claude": true, "codex": true}},
		Session: keybind.DefaultSession(),
		List:    keybind.DefaultList(),
	}
}

func openState(ctx Context) State {
	s := FromSnapshot(Snapshot{Values: map[string]string{}, Tools: ctx.Tools, Theme: "classic"})
	s.MarkBaseline()
	return s
}

func key(k string) tea.KeyMsg {
	switch k {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func press(s *State, ctx Context, keys ...string) Result {
	var result Result
	for _, k := range keys {
		result = s.Key(key(k), ctx)
	}
	return result
}

func viewContext() ViewContext {
	ctx := testContext()
	return ViewContext{
		Session:   ctx.Session,
		List:      ctx.List,
		Version:   "v1.0.0",
		Height:    40,
		TextInput: func(input textinput.Model) string { return input.View() },
	}
}

// Keys step the dialog and hand the root what it must run: a preview, an
// exit, and a save narrowed to the rows that changed.
func TestKeysStepAndAskTheRoot(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	if got := strings.Join(s.ToolNames, ","); got != "claude,codex" {
		t.Fatalf("tool names %q do not come from the tools", got)
	}

	s.Field = FieldBackground
	if result := press(&s, ctx, "right"); !result.Background || !s.TerminalBackground || !s.Dirty {
		t.Fatalf("background step = %+v, terminal %v dirty %v", result, s.TerminalBackground, s.Dirty)
	}
	s.Field = FieldUpdate
	if result := press(&s, ctx, "enter"); result.Exit != Update {
		t.Fatalf("enter on the version row = %v, want Update", result.Exit)
	}
	s.Field = FieldBugReport
	if result := press(&s, ctx, "enter"); result.Exit != ReportBug {
		t.Fatalf("enter on report a bug = %v, want ReportBug", result.Exit)
	}
	if result := press(&s, ctx, "esc"); result.Exit != Save {
		t.Fatalf("esc = %v, want Save", result.Exit)
	}
	changed := s.ChangedValues()
	if len(changed) != 1 || changed[0].Key != BackgroundSetting || changed[0].Value != "terminal" {
		t.Fatalf("changed values = %#v; want only the background key", changed)
	}
	if s.HiddenAtBaseline(s.HiddenList()) != true {
		t.Fatal("an untouched CLI set should match the baseline")
	}
}

// A theme step repaints; turning auto on asks the root to detect first.
func TestThemeStepsAskForARepaint(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldTheme
	if result := press(&s, ctx, "right"); !result.Palette || result.DetectTheme || s.ManualTheme != "solarized dark" {
		t.Fatalf("theme step = %+v, manual %q", result, s.ManualTheme)
	}
	s.Field = FieldThemeAuto
	if result := press(&s, ctx, "right"); !result.Palette || !result.DetectTheme || !s.ThemeAuto {
		t.Fatalf("auto on = %+v, auto %v", result, s.ThemeAuto)
	}
	s.ShowTheme("solarized light")
	if values := s.Values(); values[1].Value != "solarized dark" {
		t.Fatalf("auto keeps the manual theme in the theme key, got %q", values[1].Value)
	}
}

// The CLI picker keeps one CLI enabled and asks for the hidden save on esc.
func TestCLIPickerKeepsOneEnabled(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldCLIs
	press(&s, ctx, "enter")
	if !s.CLIPicker || len(s.CLINames) != 2 {
		t.Fatalf("CLI picker = %v with %v", s.CLIPicker, s.CLINames)
	}
	press(&s, ctx, " ", "down")
	if result := press(&s, ctx, " "); result.Notice.Text != "keep at least one CLI enabled" {
		t.Fatalf("hiding the last CLI = %+v", result.Notice)
	}
	if result := press(&s, ctx, "esc"); !result.SaveHidden {
		t.Fatalf("esc = %+v, want the hidden save", result)
	}
	if hidden := s.HiddenList(); len(hidden) != 1 || hidden[0] != "claude" {
		t.Fatalf("hidden = %v", hidden)
	}
	if strings.Join(s.ToolNames, ",") != "codex" {
		t.Fatalf("tool row = %v, want the enabled CLIs", s.ToolNames)
	}
}

// The key picker opens on the live tables and saves only a change from what
// the store will hold.
func TestKeyPickerSavesOnlyAChange(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldKeybindings
	if result := press(&s, ctx, "enter"); !result.Notice.Changed || result.Notice.Text != "" {
		t.Fatalf("opening the picker = %+v, want a cleared bar", result.Notice)
	}
	if !s.KeyPicker || !s.Tables[0].Equal(ctx.Session) || !s.Tables[1].Equal(ctx.List) {
		t.Fatal("key picker did not open on the live tables")
	}
	frame := s.View(viewContext())
	if frame.Title != "⚙ Keybindings" || !strings.Contains(frame.Body, s.PickedRow().Action.Name) {
		t.Fatalf("key picker frame: %+v", frame)
	}
	if result := press(&s, ctx, "down", "d", "esc"); !result.SaveKeys {
		t.Fatalf("esc = %+v, want the key save", result)
	}
	change, ok := s.KeysToSave(ctx.Session, ctx.List)
	if !ok || !change.SessionChanged || change.ListChanged {
		t.Fatalf("keys change = %#v, %v; want only the session table", change, ok)
	}
	if _, ok := s.KeysToSave(change.Session, change.List); ok {
		t.Fatal("a table the lane already commits was offered again")
	}
}

// The dialog paints from its own state plus the release facts it is handed.
func TestViewReadsReleaseFacts(t *testing.T) {
	s := openState(testContext())
	ctx := viewContext()
	ctx.Latest = "v2.0.0"
	frame := s.View(ctx)
	if frame.Card != Flex || !strings.Contains(frame.Body, "version v1.0.0") || !strings.Contains(frame.Body, "update to v2.0.0") {
		t.Fatalf("version row did not read the release:\n%s", frame.Body)
	}
	if !strings.Contains(frame.Body, "keybindings") || !strings.Contains(frame.Body, "defaults") {
		t.Fatalf("keybindings row did not summarise the tables:\n%s", frame.Body)
	}
}
