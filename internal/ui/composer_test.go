package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// newComposer is a composer detached from any screen, so the chip logic can
// be exercised without a model behind it. Focused like the real ones: a
// blurred textarea drops the key setValue steers the caret with.
func newComposer(value string) *composer {
	in := textarea.New()
	in.CharLimit = 2000
	in.ShowLineNumbers = false
	in.SetWidth(60)
	in.SetHeight(quickBarMaxRows)
	in.Focus()
	in.SetValue(value)
	return &composer{input: in, maxRows: quickBarMaxRows}
}

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

// setCursorAt puts the caret at a rune offset into a single-row value, which
// is where the chip helpers are exercised; the textarea only takes a column.
func setCursorAt(t *testing.T, c *composer, offset int) {
	t.Helper()
	if strings.Contains(c.input.Value(), "\n") {
		t.Fatal("setCursorAt places the caret on a single-row value only")
	}
	c.input.SetCursor(offset)
	if got := c.cursorOffset(); got != offset {
		t.Fatalf("caret at %d, want %d", got, offset)
	}
}

func TestComposerTokenSpansNeedAnAttachmentBehindThem(t *testing.T) {
	c := newComposer("see " + imageToken(1) + " and " + imageToken(2))
	c.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}

	spans := c.tokenSpans()
	if len(spans) != 1 || spans[0].id != 1 {
		t.Fatalf("only the pasted chip is a span, got %+v", spans)
	}
	if start := len("see "); spans[0].start != start || spans[0].end != start+len(imageToken(1)) {
		t.Fatalf("span = %+v, want the token's own offsets", spans[0])
	}
	// The second token is text the user typed that merely looks like a chip,
	// so it neither steps nor deletes as one, and it sends as itself.
	if _, ok := c.tokenEndingAt(len(c.input.Value())); ok {
		t.Fatal("typed token-shaped text should not read as a chip")
	}
	if got := c.message(); got != "see /tmp/a.png and "+imageToken(2) {
		t.Fatalf("message = %q", got)
	}
}

func TestComposerCursorOffsetCountsWholeRows(t *testing.T) {
	c := newComposer("first line\nsecond line")
	c.input.CursorEnd()
	if got, want := c.cursorOffset(), len("first line\nsecond line"); got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
	if got, want := c.cursorColumn(), len("second line"); got != want {
		t.Fatalf("column = %d, want %d — the column is within its own row", got, want)
	}
}

func TestComposerWithPaddingOnlyTakesBackSpacingThePasteAdded(t *testing.T) {
	// A paste between two words adds a space on each side, and removing the
	// chip gives both back so the sentence reads as it did.
	padded := newComposer("this " + imageToken(1) + " that")
	padded.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png", leadPad: true, trailPad: true}}
	runes := []rune(padded.input.Value())
	span := padded.withPadding(padded.tokenSpans()[0], runes)
	if span.start != len("this") || span.end != len("this "+imageToken(1)+" ") {
		t.Fatalf("padded span = %+v, want both added spaces", span)
	}

	// A paste onto whitespace adds none, so the spacing around it belongs to
	// the text and stays.
	bare := newComposer("this " + imageToken(1) + " that")
	bare.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}
	runes = []rune(bare.input.Value())
	plain := bare.tokenSpans()[0]
	if got := bare.withPadding(plain, runes); got != plain {
		t.Fatalf("span = %+v, want the token alone at %+v", got, plain)
	}
}

// A chip pasted in and then removed leaves the text it was pasted into
// exactly as it was, whichever spacing the paste had to add.
func TestComposerPasteAndRemoveRoundTripsTheText(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		{"between words", "look now"},
		{"mid-word", "looknow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newComposer(tc.text)
			setCursorAt(t, c, len("look"))
			c.attachments = []imageAttachment{{id: 1}}
			c.insertToken(&c.attachments[0])
			if got := c.input.Value(); got != "look "+imageToken(1)+" now" {
				t.Fatalf("pasted value = %q", got)
			}

			c.removeToken(c.tokenSpans()[0])
			if got := c.input.Value(); got != tc.text {
				t.Fatalf("value = %q, want the text back as %q", got, tc.text)
			}
			if got := c.cursorOffset(); got != len("look") {
				t.Fatalf("caret at %d, want where the chip was", got)
			}
			if len(c.attachments) != 0 {
				t.Fatalf("the chip's attachment should go with it: %+v", c.attachments)
			}
		})
	}
}

func TestComposerRoomForTokenGuardsTheCharLimit(t *testing.T) {
	c := newComposer("")
	// Two runes past the token are the spacing insertToken may add.
	c.input.CharLimit = len(imageToken(1)) + 2
	if !c.roomForToken(1) {
		t.Fatal("a token that exactly fits should be allowed")
	}
	c.input.SetValue("x")
	if c.roomForToken(1) {
		t.Fatal("a token that would be truncated must be refused")
	}

	// No limit is no guard: textarea only truncates when it has one.
	c.input.CharLimit = 0
	if !c.roomForToken(1) {
		t.Fatal("an unlimited prompt always has room")
	}
}

// The value here spans two rows on purpose: setValue rewinds through the
// InputBegin binding, and anything that only reaches the start of the
// caret's own row leaves the head typed into the wrong line.
func TestComposerSetValueLeavesTheCaretWhereItWasAsked(t *testing.T) {
	c := newComposer("")
	c.setValue("first line\nsecond line", len("first line\nsecond"))
	if got := c.input.Value(); got != "first line\nsecond line" {
		t.Fatalf("value = %q", got)
	}
	if got := c.cursorOffset(); got != len("first line\nsecond") {
		t.Fatalf("caret at %d, want mid-word on the second row", got)
	}

	// Out-of-range offsets clamp rather than panic on the rune slice.
	c.setValue("short", 99)
	if got := c.cursorOffset(); got != len("short") {
		t.Fatalf("caret at %d, want the end of the value", got)
	}
}

func TestComposerSnapCursorOutOfTokenFollowsAStepsDirection(t *testing.T) {
	c := newComposer("go " + imageToken(1) + " now")
	c.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}
	span := c.tokenSpans()[0]

	setCursorAt(t, c, span.end-2)
	c.snapCursorOutOfToken(snapBack)
	if got := c.cursorOffset(); got != span.start {
		t.Fatalf("caret at %d, want the chip's start at %d", got, span.start)
	}

	setCursorAt(t, c, span.start+2)
	c.snapCursorOutOfToken(snapForward)
	if got := c.cursorOffset(); got != span.end {
		t.Fatalf("caret at %d, want the chip's end at %d", got, span.end)
	}
}

func TestComposerSnapCursorOutOfTokenTakesTheNearerEdge(t *testing.T) {
	c := newComposer("go " + imageToken(1) + " now")
	c.attachments = []imageAttachment{{id: 1, path: "/tmp/a.png"}}
	span := c.tokenSpans()[0]

	setCursorAt(t, c, span.start+2)
	c.snapCursorOutOfToken(snapNearest)
	if got := c.cursorOffset(); got != span.start {
		t.Fatalf("caret at %d, want the chip's start at %d", got, span.start)
	}

	setCursorAt(t, c, span.end-2)
	c.snapCursorOutOfToken(snapNearest)
	if got := c.cursorOffset(); got != span.end {
		t.Fatalf("caret at %d, want the chip's end at %d", got, span.end)
	}

	// A caret outside every chip is left alone.
	setCursorAt(t, c, 1)
	c.snapCursorOutOfToken(snapNearest)
	if got := c.cursorOffset(); got != 1 {
		t.Fatalf("caret moved to %d from outside a chip", got)
	}
}

func TestComposerPruneReleasesChipsCutByABulkEdit(t *testing.T) {
	first := tempImage(t, "first.png")
	second := tempImage(t, "second.png")
	c := newComposer("keep " + imageToken(1) + " and " + imageToken(2))
	c.attachments = []imageAttachment{{id: 1, path: first}, {id: 2, path: second}}

	c.input.SetValue("keep " + imageToken(2))
	c.prune()
	if len(c.attachments) != 1 || c.attachments[0].id != 2 {
		t.Fatalf("attachments = %+v, want only the chip still in the text", c.attachments)
	}
	if !fileGone(first) {
		t.Fatal("a chip cut out of the text should take its temp file with it")
	}
	if fileGone(second) {
		t.Fatal("the surviving chip's file must stay: the agent still opens it")
	}

	c.release()
	if len(c.attachments) != 0 || !fileGone(second) {
		t.Fatalf("release should empty the prompt's images: %+v", c.attachments)
	}
}

func TestComposerInsertTokenSpacesOffTheWordsAroundIt(t *testing.T) {
	c := newComposer("look now")
	setCursorAt(t, c, len("look"))
	att := imageAttachment{id: 1}
	c.insertToken(&att)
	if got := c.input.Value(); got != "look "+imageToken(1)+" now" {
		t.Fatalf("value = %q, want the chip spaced off both words", got)
	}
	if !att.leadPad || att.trailPad {
		t.Fatalf("padding = lead %v trail %v, want only the space it added", att.leadPad, att.trailPad)
	}
}

// The refusal roomForToken guards: a prompt with no room for the token
// says so and stays exactly as it was, rather than inserting a chip the
// textarea would truncate into text nothing points at.
func TestComposerPasteRefusedWhenThePromptIsFull(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.CharLimit = 40
	full := strings.Repeat("x", m.quick.input.CharLimit)
	m.quick.input.SetValue(full)

	_, cmd := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlV})

	if cmd != nil {
		t.Fatal("a refused paste must not start a clipboard read")
	}
	if m.errBar.text != "prompt is full - shorten it before pasting an image" {
		t.Fatalf("errBar = %q, want the full-prompt refusal", m.errBar.text)
	}
	if got := m.quick.input.Value(); got != full {
		t.Fatalf("value = %q, want the prompt untouched", got)
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("no chip should be reserved: %+v", m.quick.attachments)
	}
}

// A clipboard holding text rather than an image still pastes. The read
// comes back as a message only the textarea can read, so what this pins is
// the routing: whatever the read yields has to reach the input the ctrl+v
// was typed into, rather than being dropped on the way past.
func TestComposerNoImageFallsThroughToATextPaste(t *testing.T) {
	orig := readClipboardText
	t.Cleanup(func() { readClipboardText = orig })
	readClipboardText = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("from the clipboard")}
	}

	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetValue("see ")
	m.quick.input.CursorEnd()
	m.quick.lastImageID = 1
	m.quick.attachments = []imageAttachment{{id: 1}}
	m.quick.input.InsertString(imageToken(1))

	gen := m.quick.gen
	updated, cmd := m.Update(pasteImageMsg{target: composerQuick, gen: gen, id: 1, noImage: true})
	m, ok := updated.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", updated)
	}
	if len(m.quick.attachments) != 0 {
		t.Fatalf("the reserved chip should go back out: %+v", m.quick.attachments)
	}
	if cmd == nil {
		t.Fatal("a clipboard with no image should still start a text paste")
	}

	// The command carries the clipboard read; its message is what has to
	// land in the input.
	text, ok := cmd().(pasteTextMsg)
	if !ok {
		t.Fatalf("paste cmd returned %T", cmd())
	}
	m = applyMsg(t, m, text)
	if got := m.quick.input.Value(); got != "see from the clipboard" {
		t.Fatalf("value = %q, want the clipboard text in place of the chip", got)
	}
}

// The same fallback on the New Session form, where the prompt is one field
// among several. Tabbing on while the read is in flight blurs the prompt,
// and the result still belongs to the text the ctrl+v was typed into.
func TestComposerNoImageFallsThroughToABlurredFormPrompt(t *testing.T) {
	orig := readClipboardText
	t.Cleanup(func() { readClipboardText = orig })
	readClipboardText = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("from the clipboard")}
	}

	m := buildModel(t)
	m.openForm()
	m.form.focus = fieldPrompt
	m.form.focusStep(m, 0)
	m.form.prompt.input.SetValue("see ")
	m.form.prompt.input.CursorEnd()
	m.form.prompt.attachments = []imageAttachment{{id: 1}}
	m.form.prompt.input.InsertString(imageToken(1))

	gen := m.form.prompt.gen
	m.form.focusStep(m, 1)
	if m.form.prompt.input.Focused() {
		t.Fatal("tabbing on to the next field blurs the prompt")
	}

	updated, cmd := m.Update(pasteImageMsg{target: composerForm, gen: gen, id: 1, noImage: true})
	m, ok := updated.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", updated)
	}
	if cmd == nil {
		t.Fatal("a clipboard with no image should still start a text paste")
	}
	text, ok := cmd().(pasteTextMsg)
	if !ok {
		t.Fatalf("paste cmd returned %T", cmd())
	}
	m = applyMsg(t, m, text)
	if got := m.form.prompt.input.Value(); got != "see from the clipboard" {
		t.Fatalf("value = %q, want the clipboard text in place of the chip", got)
	}
	if m.form.prompt.input.Focused() {
		t.Fatal("a late paste must not steal focus back to the prompt")
	}
}

// The same fallback, once the bar it was typed into is gone.
func TestComposerTextPasteDroppedWhenItsBoxIsClosed(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetValue("kept")
	gen := m.quick.gen
	m.quick.active = false

	m = applyMsg(t, m, pasteTextMsg{
		target: composerQuick,
		gen:    gen,
		inner:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("late")},
	})
	if got := m.quick.input.Value(); got != "kept" {
		t.Fatalf("value = %q, want the closed bar left alone", got)
	}
}

// A clipboard read outlives the box it was started in. Cancelling a form
// and opening another is quick enough to beat the read home, and the text
// belongs to the prompt that is gone, not to the one now on screen.
func TestComposerPasteFromAClosedBoxSkipsItsSuccessor(t *testing.T) {
	m := buildModel(t)
	m.openForm()
	stale := m.form.prompt.gen
	m.handleFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	m.openForm()
	if m.form.prompt.gen == stale {
		t.Fatal("a reopened form should be a different box")
	}
	focusFormPrompt(t, m)
	for _, r := range "typed since" {
		m.handleFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	m = applyMsg(t, m, pasteTextMsg{
		target: composerForm,
		gen:    stale,
		inner:  tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" from the old form")},
	})
	if got := m.form.prompt.input.Value(); got != "typed since" {
		t.Fatalf("value = %q, want the new form untouched by the old form's paste", got)
	}

	// An image read from the same dead box is dropped the same way, file
	// and all, rather than filling a chip the new form never reserved.
	path := tempImage(t, "stale.png")
	m.form.prompt.attachments = []imageAttachment{{id: 1}}
	m = applyMsg(t, m, pasteImageMsg{target: composerForm, gen: stale, id: 1, path: path})
	if got := m.form.prompt.attachments[0].path; got != "" {
		t.Fatalf("chip path = %q, want the stale read ignored", got)
	}
	if !fileGone(path) {
		t.Fatal("a dropped read should not leave its file behind")
	}
}

// A paste result carries the box it was started from, so the two screens
// cannot land each other's images.
func TestComposerTargetsRouteToTheirOwnBox(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.openForm()

	if got := m.composerFor(composerQuick); got != &m.quick.composer {
		t.Fatal("composerQuick should name the quick bar's box")
	}
	if got := m.composerFor(composerForm); got != &m.form.prompt {
		t.Fatal("composerForm should name the form's prompt")
	}
	// The form is up and the bar is still armed behind it, so each box
	// answers for itself rather than for whatever is on screen.
	if !m.composerOpen(composerQuick) || !m.composerOpen(composerForm) {
		t.Fatal("both boxes are open here")
	}
	m.quick.active = false
	m.mode = modeList
	if m.composerOpen(composerQuick) || m.composerOpen(composerForm) {
		t.Fatal("a closed box has nowhere for an image to land")
	}
}

func fileGone(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func TestComposerDisplayRowsMatchTheTextarea(t *testing.T) {
	values := []string{
		"",
		"x",
		"1234567890",
		"1234567890\nx",
		"one two three four five six seven eight nine ten eleven twelve",
		"averyveryverylongwordthatcannotwrapanywhereatall and more",
		"日本語のテキストをここに書きます日本語のテキストをここに書きます",
		"emoji 🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉 done",
		"\n\n\nlast",
		"first\n\nthird line that is long enough to wrap around the box\n",
		strings.Repeat("word ", 40),
		strings.Repeat("ab ", 33) + "\n" + strings.Repeat("x", 31),
	}
	for _, width := range []int{5, 8, 12, 13, 22, 40} {
		for _, value := range values {
			c := newComposer("")
			c.input.EndOfBufferCharacter = '~'
			c.input.SetWidth(width)
			c.input.SetValue(value)
			_, total := c.displayRows()
			c.input.SetHeight(200)
			lines := strings.Split(ansi.Strip(c.input.View()), "\n")
			content := 0
			for content < len(lines) && !strings.Contains(lines[content], "~") {
				content++
			}
			if value == "" {
				content = 1
			}
			if total != content {
				t.Errorf("width %d value %q: displayRows total %d, textarea paints %d rows", width, value, total, content)
			}
			c.input.SetValue("")
			c.input.InsertString(value)
			_ = c.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
			prev, _ := c.displayRows()
			if prev != 0 {
				t.Errorf("width %d value %q: caret at start on row %d", width, value, prev)
			}
			for range len([]rune(value)) {
				_ = c.updateInput(tea.KeyMsg{Type: tea.KeyRight})
				row, _ := c.displayRows()
				if row < prev || row > prev+1 {
					t.Errorf("width %d value %q: caret jumped from row %d to %d", width, value, prev, row)
				}
				prev = row
			}
			if value != "" && prev != total-1 {
				t.Errorf("width %d value %q: caret at end on row %d of %d", width, value, prev, total)
			}
		}
	}
}

type fakeComposerHost struct {
	errs    []string
	cleared int
}

func (h *fakeComposerHost) reportErr(text string) { h.errs = append(h.errs, text) }

func (h *fakeComposerHost) clearErr() { h.cleared++ }

// A clipboard result lands through the composer's host alone: a path fills
// its chip, and a failed read reports and takes the chip back out.
func TestComposerAcceptsImagesThroughAFakeHost(t *testing.T) {
	h := &fakeComposerHost{}
	c := newComposer("look ")
	c.input.CursorEnd()
	c.gen = 3
	if _, handled := c.handleChipKey(h, composerQuick, tea.KeyMsg{Type: tea.KeyCtrlV}); !handled || len(c.attachments) != 1 {
		t.Fatalf("ctrl+v did not reserve a chip: %+v", c.attachments)
	}
	first, shot := c.attachments[0].id, t.TempDir()+"/shot.png"
	c.acceptImage(h, pasteImageMsg{target: composerQuick, gen: 3, id: first, path: shot}, true)
	if c.attachments[0].path != shot || h.cleared != 2 {
		t.Fatalf("path = %q cleared = %d", c.attachments[0].path, h.cleared)
	}
	if _, handled := c.handleChipKey(h, composerQuick, tea.KeyMsg{Type: tea.KeyCtrlV}); !handled {
		t.Fatal("second paste not handled")
	}
	second := c.attachments[1].id
	c.acceptImage(h, pasteImageMsg{target: composerQuick, gen: 3, id: second, err: os.ErrPermission}, true)
	if len(c.attachments) != 1 || len(h.errs) != 1 {
		t.Fatalf("a failed read should drop its chip and report: %+v %v", c.attachments, h.errs)
	}
}
