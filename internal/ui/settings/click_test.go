package settings

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The first click on a row moves the cursor there; the second runs it the
// way ↵ runs an action row and → steps a value.
func TestClickMovesThenRunsTheRow(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldTool
	if result := s.Click(FieldBackground, ctx); result.Background || s.Field != FieldBackground || s.TerminalBackground {
		t.Fatalf("first click = %+v, field %d, background %v", result, s.Field, s.TerminalBackground)
	}
	if result := s.Click(FieldBackground, ctx); !result.Background || !s.TerminalBackground {
		t.Fatalf("second click on a value row should step it, got %+v", result)
	}
	for field, exit := range map[int]Exit{FieldDocs: OpenDocs, FieldBugReport: ReportBug, FieldFeatureRequest: OpenFeatureRequest} {
		s.Click(field, ctx)
		if result := s.Click(field, ctx); result.Exit != exit {
			t.Errorf("second click on field %d = %v, want %v", field, result.Exit, exit)
		}
	}
	s.Click(FieldCLIs, ctx)
	if s.Click(FieldCLIs, ctx); !s.CLIPicker {
		t.Fatal("second click on CLIs should open the picker")
	}
}

// With no release to take, the version row reads, and a click on it must
// not save and close the way ↵ does there.
func TestClickOnTheVersionRowUpdatesOnlyWhenOneIsReady(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldUpdate
	if result := s.Click(FieldUpdate, ctx); result.Exit != Stay {
		t.Fatalf("no update known: click = %v, want Stay", result.Exit)
	}
	ctx.UpdateReady = true
	if result := s.Click(FieldUpdate, ctx); result.Exit != Update {
		t.Fatalf("update known: click = %v, want Update", result.Exit)
	}
}

func TestClickOnTheCustomEditorOpensItsLine(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldEditor
	press(&s, ctx, "left")
	if !s.Editor.Custom() {
		t.Fatalf("test setup: left from auto lands on custom, got %q", s.Editor.Label())
	}
	if s.Click(FieldEditor, ctx); !s.Editor.Typing() {
		t.Fatal("a click on the focused custom row should open its line")
	}
}

func TestClickOnTheCLIPickerTogglesAtOnce(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldCLIs
	press(&s, ctx, "enter")
	if s.ClickCLI(1, ctx); !s.CLIHidden["codex"] || s.CLICursor != 1 {
		t.Fatalf("one click should hide codex, hidden %v cursor %d", s.CLIHidden, s.CLICursor)
	}
	if s.ClickCLI(len(s.CLINames), ctx); s.CLICursor != 1 {
		t.Fatalf("the blank line under the names is no row, cursor %d", s.CLICursor)
	}
	if result := s.ClickCLI(len(s.CLINames)+1, ctx); result.Exit != RequestCLISupport {
		t.Fatalf("the request line opens its form, got %v", result.Exit)
	}
}

// The picker records which action each painted line shows, so a click
// reads its row off the frame rather than re-deriving the headings.
func TestClickOnTheKeyPickerFollowsThePaintedRows(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldKeybindings
	press(&s, ctx, "enter")
	lines := strings.Split(ansi.Strip(s.View(viewContext()).Body), "\n")
	target := -1
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), KeyRows(s.Tables)[2].Action.Name) {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatalf("test setup: the third action is not painted:\n%s", strings.Join(lines, "\n"))
	}
	if s.ClickKey(0); s.KeyCursor != 0 {
		t.Fatalf("a heading is no row, cursor moved to %d", s.KeyCursor)
	}
	if s.ClickKey(target); s.KeyCursor != 2 || s.KeyCapture {
		t.Fatalf("first click should move to row 2, cursor %d capture %v", s.KeyCursor, s.KeyCapture)
	}
	if s.ClickKey(target); !s.KeyCapture {
		t.Fatal("a click on the row under the cursor starts capture")
	}
	if s.ClickKey(target - 1); s.KeyCursor != 2 {
		t.Fatalf("a capture keeps the rows still, cursor %d", s.KeyCursor)
	}
}

// A scrolled picker holds its window while the cursor stays on screen, so
// the second click lands on the row the first one picked.
func TestKeyPickerRowsHoldStillUnderAClick(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldKeybindings
	press(&s, ctx, "enter")
	view := viewContext()
	view.Height = 26
	s.View(view)
	// The last line is the "more" counter; the one above it is the lowest
	// painted row.
	line := len(s.keyLines) - 2
	if s.keyLines[line] <= 0 || len(s.keyLines) >= len(KeyRows(s.Tables)) {
		t.Fatalf("test setup: the picker should scroll, painted %v", s.keyLines)
	}
	row := s.keyLines[line]
	s.ClickKey(line)
	s.View(view)
	if s.keyLines[line] != row {
		t.Fatalf("the window moved under the pointer: line %d showed row %d, now %d", line, row, s.keyLines[line])
	}
	if s.ClickKey(line); !s.KeyCapture {
		t.Fatal("the second click should start capture on the same row")
	}
}

// In the saved prompts list a click moves to a prompt and a second opens
// it in the editor; in the editor a click moves the focus between fields.
func TestClickOnSavedPrompts(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.OpenPrompts()
	s.LoadedPrompts(PromptsRead{Gen: s.Prompts.Gen(), Rows: []Prompt{{Name: "alpha", Text: "a"}, {Name: "beta", Text: "b"}}})
	view := viewContext()
	view.InnerWidth = 60
	lines := strings.Split(ansi.Strip(s.View(view).Body), "\n")
	beta := -1
	for i, line := range lines {
		if strings.Contains(line, "beta") {
			beta = i
		}
	}
	if beta < 0 {
		t.Fatalf("test setup: beta is not painted:\n%s", strings.Join(lines, "\n"))
	}
	if s.ClickPrompts(beta); s.Prompts.cursor != 1 || s.Prompts.Editing() {
		t.Fatalf("first click should move to beta, cursor %d editing %v", s.Prompts.cursor, s.Prompts.Editing())
	}
	if s.ClickPrompts(len(lines) - 1); s.Prompts.cursor != 1 {
		t.Fatal("the hint line under the list is no prompt")
	}
	if s.ClickPrompts(beta); !s.Prompts.Editing() || s.Prompts.name.Value() != "beta" {
		t.Fatalf("second click should open beta in the editor, editing %v", s.Prompts.Editing())
	}
	s.View(view)
	text := -1
	for i, holds := range s.Prompts.lines {
		if holds == promptTextField {
			text = i
			break
		}
	}
	if s.ClickPrompts(text); s.Prompts.focus != 1 {
		t.Fatalf("a click on the text field should focus it, focus %d", s.Prompts.focus)
	}
	if s.ClickPrompts(0); s.Prompts.focus != 0 {
		t.Fatalf("a click on the name should focus it, focus %d", s.Prompts.focus)
	}
}
