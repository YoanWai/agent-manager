package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// applyMsg sends a message through the model and hands back the model it
// answered with, failing the test rather than panicking on a surprise.
func applyMsg(t *testing.T, m *Model, msg tea.Msg) *Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", updated)
	}
	return next
}

// The refusal the prompt's own roomForToken guards: a prompt with no room
// for the token says so and stays exactly as it was, rather than inserting
// a chip the textarea would truncate into text nothing points at.
func TestComposerPasteRefusedWhenThePromptIsFull(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.CharLimit = 40
	full := strings.Repeat("x", m.quick().Input.CharLimit)
	m.quick().Input.SetValue(full)

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})

	if cmd != nil {
		t.Fatal("a refused paste must not start a clipboard read")
	}
	if m.errBar.text != "prompt is full - shorten it before pasting an image" {
		t.Fatalf("errBar = %q, want the full-prompt refusal", m.errBar.text)
	}
	if got := m.quick().Input.Value(); got != full {
		t.Fatalf("value = %q, want the prompt untouched", got)
	}
	if len(m.quick().Attachments) != 0 {
		t.Fatalf("no chip should be reserved: %+v", m.quick().Attachments)
	}
}

// A clipboard holding text rather than an image still pastes. The read
// comes back as a message only the textarea can read, so what this pins is
// the routing: whatever the read yields has to reach the input the ctrl+v
// was typed into, rather than being dropped on the way past.
func TestComposerNoImageFallsThroughToATextPaste(t *testing.T) {
	orig := composer.ReadClipboardText
	t.Cleanup(func() { composer.ReadClipboardText = orig })
	composer.ReadClipboardText = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("from the clipboard")}
	}

	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.SetValue("see ")
	m.quick().Input.CursorEnd()
	m.quick().LastImageID = 1
	m.quick().Attachments = []composer.Attachment{{ID: 1}}
	m.quick().Input.InsertString(composer.Token(1))

	gen := m.quick().Gen
	updated, cmd := m.Update(composer.PasteImageMsg{Target: composer.Quick, Gen: gen, ID: 1, NoImage: true})
	m, ok := updated.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", updated)
	}
	if len(m.quick().Attachments) != 0 {
		t.Fatalf("the reserved chip should go back out: %+v", m.quick().Attachments)
	}
	if cmd == nil {
		t.Fatal("a clipboard with no image should still start a text paste")
	}

	// The command carries the clipboard read; its message is what has to
	// land in the input.
	text, ok := cmd().(composer.PasteTextMsg)
	if !ok {
		t.Fatalf("paste cmd returned %T", cmd())
	}
	m = applyMsg(t, m, text)
	if got := m.quick().Input.Value(); got != "see from the clipboard" {
		t.Fatalf("value = %q, want the clipboard text in place of the chip", got)
	}
}

// The same fallback on the New Session form, where the prompt is one field
// among several. Tabbing on while the read is in flight blurs the prompt,
// and the result still belongs to the text the ctrl+v was typed into.
func TestComposerNoImageFallsThroughToABlurredFormPrompt(t *testing.T) {
	orig := composer.ReadClipboardText
	t.Cleanup(func() { composer.ReadClipboardText = orig })
	composer.ReadClipboardText = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("from the clipboard")}
	}

	m := buildModel(t)
	m.openForm()
	m.form().FocusField(m.spawnHost(), spawn.FieldPrompt)
	m.form().FocusStep(m.spawnHost(), 0)
	m.form().prompt.Input.SetValue("see ")
	m.form().prompt.Input.CursorEnd()
	m.form().prompt.Attachments = []composer.Attachment{{ID: 1}}
	m.form().prompt.Input.InsertString(composer.Token(1))

	gen := m.form().prompt.Gen
	m.form().FocusStep(m.spawnHost(), 1)
	if m.form().prompt.Input.Focused() {
		t.Fatal("tabbing on to the next field blurs the prompt")
	}

	updated, cmd := m.Update(composer.PasteImageMsg{Target: composer.Form, Gen: gen, ID: 1, NoImage: true})
	m, ok := updated.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", updated)
	}
	if cmd == nil {
		t.Fatal("a clipboard with no image should still start a text paste")
	}
	text, ok := cmd().(composer.PasteTextMsg)
	if !ok {
		t.Fatalf("paste cmd returned %T", cmd())
	}
	m = applyMsg(t, m, text)
	if got := m.form().prompt.Input.Value(); got != "see from the clipboard" {
		t.Fatalf("value = %q, want the clipboard text in place of the chip", got)
	}
	if m.form().prompt.Input.Focused() {
		t.Fatal("a late paste must not steal focus back to the prompt")
	}
}

// The same fallback, once the bar it was typed into is gone.
func TestComposerTextPasteDroppedWhenItsBoxIsClosed(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	q := m.quick()
	q.Input.SetValue("kept")
	gen := q.Gen
	m.closeQuick()

	m = applyMsg(t, m, composer.PasteTextMsg{
		Target: composer.Quick,
		Gen:    gen,
		Inner:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("late")},
	})
	if got := q.Input.Value(); got != "kept" {
		t.Fatalf("value = %q, want the closed bar left alone", got)
	}
}

// A clipboard read outlives the box it was started in. Cancelling a form
// and opening another is quick enough to beat the read home, and the text
// belongs to the prompt that is gone, not to the one now on screen.
func TestComposerPasteFromAClosedBoxSkipsItsSuccessor(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openForm()
	stale := m.form().prompt.Gen
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.openForm()
	if m.form().prompt.Gen == stale {
		t.Fatal("a reopened form should be a different box")
	}
	focusFormPrompt(t, m)
	for _, r := range "typed since" {
		m.handleFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = applyMsg(t, m, composer.PasteTextMsg{
		Target: composer.Form,
		Gen:    stale,
		Inner:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" from the old form")},
	})
	if got := m.form().prompt.Input.Value(); got != "typed since" {
		t.Fatalf("value = %q, want the new form untouched by the old form's paste", got)
	}

	// An image read from the same dead box is dropped the same way, file
	// and all, rather than filling a chip the new form never reserved.
	path := tempImage(t, "stale.png")
	m.form().prompt.Attachments = []composer.Attachment{{ID: 1}}
	m = applyMsg(t, m, composer.PasteImageMsg{Target: composer.Form, Gen: stale, ID: 1, Path: path})
	if got := m.form().prompt.Attachments[0].Path; got != "" {
		t.Fatalf("chip path = %q, want the stale read ignored", got)
	}
	if !fileGone(path) {
		t.Fatal("a dropped read should not leave its file behind")
	}
}

// A paste result carries the box it was started from, so the two screens
// cannot land each other's images.
func TestComposerTargetsRouteToTheirOwnBox(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	if got := m.composerFor(composer.Quick); got != &m.quick().Composer {
		t.Fatal("composer.Quick should name the quick bar's box")
	}
	if !m.composerOpen(composer.Quick) || m.composerOpen(composer.Form) {
		t.Fatal("only the bar is open here")
	}
	// The form takes the list's place and the bar with it.
	m.openForm()
	if got := m.composerFor(composer.Form); got != &m.form().prompt {
		t.Fatal("composer.Form should name the form's prompt")
	}
	if m.composerOpen(composer.Quick) || !m.composerOpen(composer.Form) {
		t.Fatal("only the form is open here")
	}
	m.base = baseList
	m.clearSurfaces()
	if m.composerOpen(composer.Quick) || m.composerOpen(composer.Form) {
		t.Fatal("a closed box has nowhere for an image to land")
	}
}

func fileGone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
