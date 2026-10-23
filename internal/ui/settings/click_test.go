package settings

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// paintedAt paints the dialog and finds the first body line holding text,
// and the column it starts at.
func paintedAt(t *testing.T, s *State, view ViewContext, text string) (col, line int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(s.View(view).Body), "\n")
	for line, painted := range lines {
		if at := strings.Index(painted, text); at >= 0 {
			return ansi.StringWidth(painted[:at]), line
		}
	}
	t.Fatalf("test setup: the dialog painted no %q:\n%s", text, strings.Join(lines, "\n"))
	return 0, 0
}

// clickText paints the dialog and clicks where it painted text.
func clickText(t *testing.T, s *State, ctx Context, view ViewContext, text string) Result {
	t.Helper()
	col, line := paintedAt(t, s, view, text)
	return s.Click(col, line, ctx)
}

// The first click on a row moves the cursor there; the second runs it the
// way ↵ runs an action row and → steps a value.
func TestClickMovesThenRunsTheRow(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldTool
	if result := clickText(t, &s, ctx, view, "background"); result.Background || s.Field != FieldBackground || s.TerminalBackground {
		t.Fatalf("first click = %+v, field %d, background %v", result, s.Field, s.TerminalBackground)
	}
	if result := clickText(t, &s, ctx, view, "background"); !result.Background || !s.TerminalBackground {
		t.Fatalf("second click on a value row should step it, got %+v", result)
	}
	for label, exit := range map[string]Exit{"report a bug": ReportBug, "suggest a change": OpenFeatureRequest} {
		clickText(t, &s, ctx, view, label)
		if result := clickText(t, &s, ctx, view, label); result.Exit != exit {
			t.Errorf("second click on %q = %v, want %v", label, result.Exit, exit)
		}
	}
	clickText(t, &s, ctx, view, "show or hide")
	if clickText(t, &s, ctx, view, "show or hide"); !s.CLIPicker {
		t.Fatal("second click on CLIs should open the picker")
	}
}

// The docs and saved prompts rows open on the first click, and the save
// button saves.
func TestClickOnTheButtonRowsRunsAtOnce(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	if result := clickText(t, &s, ctx, view, "open the docs"); result.Exit != OpenDocs || s.Field != FieldDocs {
		t.Fatalf("docs click = %v, field %d", result.Exit, s.Field)
	}
	if result := clickText(t, &s, ctx, view, "Save and close"); result.Exit != Save {
		t.Fatalf("save button click = %v, want Save", result.Exit)
	}
	col, line := paintedAt(t, &s, view, "Save and close")
	if result := s.Click(col+40, line, ctx); result.Exit != Stay {
		t.Fatalf("a click past the save button = %v, want Stay", result.Exit)
	}
	if clickText(t, &s, ctx, view, "manage named"); !s.Prompts.Open() {
		t.Fatal("one click on saved prompts should open the panel")
	}
}

// With no release to take, the version row reads, and a click on it must
// not save and close the way ↵ does there.
func TestClickOnTheVersionRowUpdatesOnlyWhenOneIsReady(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldUpdate
	if result := clickText(t, &s, ctx, view, "version"); result.Exit != Stay {
		t.Fatalf("no update known: click = %v, want Stay", result.Exit)
	}
	ctx.UpdateReady = true
	if result := clickText(t, &s, ctx, view, "version"); result.Exit != Update {
		t.Fatalf("update known: click = %v, want Update", result.Exit)
	}
}

func TestClickOnTheCustomEditorOpensItsLine(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldEditor
	press(&s, ctx, "left")
	if !s.Editor.Custom() {
		t.Fatalf("test setup: left from auto lands on custom, got %q", s.Editor.Label())
	}
	if clickText(t, &s, ctx, view, s.Editor.Label()); !s.Editor.Typing() {
		t.Fatal("a click on the focused custom row should open its line")
	}
}

func TestClickOnTheCLIPickerTogglesAtOnce(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldCLIs
	press(&s, ctx, "enter")
	if clickText(t, &s, ctx, view, "codex"); !s.CLIHidden["codex"] || s.CLICursor != 1 {
		t.Fatalf("one click should hide codex, hidden %v cursor %d", s.CLIHidden, s.CLICursor)
	}
	_, request := paintedAt(t, &s, view, "request CLI support")
	if s.Click(0, request-1, ctx); s.CLICursor != 1 {
		t.Fatalf("the blank line under the names is no row, cursor %d", s.CLICursor)
	}
	if result := clickText(t, &s, ctx, view, "request CLI support"); result.Exit != RequestCLISupport {
		t.Fatalf("the request line opens its form, got %v", result.Exit)
	}
}

// The picker records which action each painted line shows, so a click
// reads its row off the frame rather than re-deriving the headings.
func TestClickOnTheKeyPickerFollowsThePaintedRows(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldKeybindings
	press(&s, ctx, "enter")
	third := KeyRows(s.Tables)[2].Action.Name
	if s.Click(0, 0, ctx); s.KeyCursor != 0 {
		t.Fatalf("a heading is no row, cursor moved to %d", s.KeyCursor)
	}
	if clickText(t, &s, ctx, view, third); s.KeyCursor != 2 || s.KeyCapture {
		t.Fatalf("first click should move to row 2, cursor %d capture %v", s.KeyCursor, s.KeyCapture)
	}
	if clickText(t, &s, ctx, view, third); !s.KeyCapture {
		t.Fatal("a click on the row under the cursor starts capture")
	}
	_, line := paintedAt(t, &s, view, third)
	if s.Click(0, line-1, ctx); s.KeyCursor != 2 {
		t.Fatalf("a capture keeps the rows still, cursor %d", s.KeyCursor)
	}
}

// A scrolled picker holds its window while the cursor stays on screen, so
// the second click lands on the row the first one picked.
func TestKeyPickerRowsHoldStillUnderAClick(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldKeybindings
	press(&s, ctx, "enter")
	view.Height = 26
	lines := strings.Split(ansi.Strip(s.View(view).Body), "\n")
	// The last line is the "more" counter; the one above it is the lowest
	// painted row.
	line := len(lines) - 2
	row, ok := s.lines.At(line)
	if !ok || row <= 0 || !strings.Contains(lines[len(lines)-1], "more") {
		t.Fatalf("test setup: the picker should scroll:\n%s", strings.Join(lines, "\n"))
	}
	s.Click(0, line, ctx)
	s.View(view)
	if now, _ := s.lines.At(line); now != row {
		t.Fatalf("the window moved under the pointer: line %d showed row %d, now %d", line, row, now)
	}
	if s.Click(0, line, ctx); !s.KeyCapture {
		t.Fatal("the second click should start capture on the same row")
	}
}

// In the saved prompts list a click moves to a prompt and a second opens
// it in the editor; in the editor a click moves the focus between fields.
func TestClickOnSavedPrompts(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.OpenPrompts()
	s.LoadedPrompts(PromptsRead{Gen: s.Prompts.Gen(), Rows: []Prompt{{Name: "alpha", Text: "a"}, {Name: "beta", Text: "b"}}})
	view.InnerWidth = 60
	if clickText(t, &s, ctx, view, "beta"); s.Prompts.cursor != 1 || s.Prompts.Editing() {
		t.Fatalf("first click should move to beta, cursor %d editing %v", s.Prompts.cursor, s.Prompts.Editing())
	}
	if clickText(t, &s, ctx, view, "agents add these"); s.Prompts.cursor != 1 {
		t.Fatal("the hint line under the list is no prompt")
	}
	if clickText(t, &s, ctx, view, "beta"); !s.Prompts.Editing() || s.Prompts.name.Value() != "beta" {
		t.Fatalf("second click should open beta in the editor, editing %v", s.Prompts.Editing())
	}
	if clickText(t, &s, ctx, view, "text"); s.Prompts.focus != promptTextField {
		t.Fatalf("a click on the text field should focus it, focus %d", s.Prompts.focus)
	}
	if clickText(t, &s, ctx, view, "name"); s.Prompts.focus != promptNameField {
		t.Fatalf("a click on the name should focus it, focus %d", s.Prompts.focus)
	}
}

// The wheel walks the list that is up, and leaves typing alone.
func TestWheelStepsTheListThatIsUp(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	s.Field = FieldTool
	if s.Wheel(1, ctx); s.Field != FieldTool+1 {
		t.Fatalf("wheel down should move a field, field %d", s.Field)
	}
	s.Field = FieldKeybindings
	press(&s, ctx, "enter", "enter")
	cursor := s.KeyCursor
	if s.Wheel(1, ctx); s.KeyCursor != cursor || !s.KeyCapture {
		t.Fatalf("a key capture keeps the wheel out, cursor %d capture %v", s.KeyCursor, s.KeyCapture)
	}
}

// The key picker paints no more lines than the card has room for, down to
// a single row in a short terminal.
func TestKeyPickerFitsAShortTerminal(t *testing.T) {
	ctx, view := testContext(), viewContext()
	s := openState(ctx)
	s.Field = FieldKeybindings
	press(&s, ctx, "enter")
	for height := 30; height >= 8; height-- {
		view.Height = height
		room := height - view.Chrome(nil)
		lines := strings.Split(s.View(view).Body, "\n")
		if len(lines) > max(room, 3) {
			t.Fatalf("height %d: %d body lines for %d rows of room", height, len(lines), room)
		}
	}
}
