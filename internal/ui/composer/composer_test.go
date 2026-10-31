package composer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// newComposer is a composer detached from any screen, so the chip logic can
// be exercised without a model behind it. Focused like the real ones: a
// blurred textarea drops the key setValue steers the caret with.
func newComposer(value string) *Composer {
	in := textarea.New()
	in.CharLimit = 2000
	in.ShowLineNumbers = false
	in.SetWidth(60)
	in.SetHeight(5)
	in.Focus()
	in.SetValue(value)
	return &Composer{Input: in, MaxRows: 5}
}

func setCursorAt(t *testing.T, c *Composer, offset int) {
	t.Helper()
	if strings.Contains(c.Input.Value(), "\n") {
		t.Fatal("setCursorAt places the caret on a single-row value only")
	}
	c.Input.SetCursor(offset)
	if got := c.CursorOffset(); got != offset {
		t.Fatalf("caret at %d, want %d", got, offset)
	}
}

func TestComposerTokenSpansNeedAnAttachmentBehindThem(t *testing.T) {
	c := newComposer("see " + Token(1) + " and " + Token(2))
	c.Attachments = []Attachment{{ID: 1, Path: "/tmp/a.png"}}

	spans := c.tokenSpans()
	if len(spans) != 1 || spans[0].id != 1 {
		t.Fatalf("only the pasted chip is a span, got %+v", spans)
	}
	if start := len("see "); spans[0].start != start || spans[0].end != start+len(Token(1)) {
		t.Fatalf("span = %+v, want the token's own offsets", spans[0])
	}
	// The second token is text the user typed that merely looks like a chip,
	// so it neither steps nor deletes as one, and it sends as itself.
	if _, ok := c.tokenEndingAt(len(c.Input.Value())); ok {
		t.Fatal("typed token-shaped text should not read as a chip")
	}
	if got := c.Message(); got != "see /tmp/a.png and "+Token(2) {
		t.Fatalf("message = %q", got)
	}
}

func TestComposerCursorOffsetCountsWholeRows(t *testing.T) {
	c := newComposer("first line\nsecond line")
	c.Input.CursorEnd()
	if got, want := c.CursorOffset(), len("first line\nsecond line"); got != want {
		t.Fatalf("offset = %d, want %d", got, want)
	}
	if got, want := c.cursorColumn(), len("second line"); got != want {
		t.Fatalf("column = %d, want %d — the column is within its own row", got, want)
	}
}

func TestComposerWithPaddingOnlyTakesBackSpacingThePasteAdded(t *testing.T) {
	// A paste between two words adds a space on each side, and removing the
	// chip gives both back so the sentence reads as it did.
	padded := newComposer("this " + Token(1) + " that")
	padded.Attachments = []Attachment{{ID: 1, Path: "/tmp/a.png", LeadPad: true, TrailPad: true}}
	runes := []rune(padded.Input.Value())
	span := padded.withPadding(padded.tokenSpans()[0], runes)
	if span.start != len("this") || span.end != len("this "+Token(1)+" ") {
		t.Fatalf("padded span = %+v, want both added spaces", span)
	}

	// A paste onto whitespace adds none, so the spacing around it belongs to
	// the text and stays.
	bare := newComposer("this " + Token(1) + " that")
	bare.Attachments = []Attachment{{ID: 1, Path: "/tmp/a.png"}}
	runes = []rune(bare.Input.Value())
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
			c.Attachments = []Attachment{{ID: 1}}
			c.insertToken(&c.Attachments[0])
			if got := c.Input.Value(); got != "look "+Token(1)+" now" {
				t.Fatalf("pasted value = %q", got)
			}

			c.removeToken(c.tokenSpans()[0])
			if got := c.Input.Value(); got != tc.text {
				t.Fatalf("value = %q, want the text back as %q", got, tc.text)
			}
			if got := c.CursorOffset(); got != len("look") {
				t.Fatalf("caret at %d, want where the chip was", got)
			}
			if len(c.Attachments) != 0 {
				t.Fatalf("the chip's attachment should go with it: %+v", c.Attachments)
			}
		})
	}
}

func TestComposerRoomForTokenGuardsTheCharLimit(t *testing.T) {
	c := newComposer("")
	// Two runes past the token are the spacing insertToken may add.
	c.Input.CharLimit = len(Token(1)) + 2
	if !c.roomForToken(1) {
		t.Fatal("a token that exactly fits should be allowed")
	}
	c.Input.SetValue("x")
	if c.roomForToken(1) {
		t.Fatal("a token that would be truncated must be refused")
	}

	// No limit is no guard: textarea only truncates when it has one.
	c.Input.CharLimit = 0
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
	if got := c.Input.Value(); got != "first line\nsecond line" {
		t.Fatalf("value = %q", got)
	}
	if got := c.CursorOffset(); got != len("first line\nsecond") {
		t.Fatalf("caret at %d, want mid-word on the second row", got)
	}

	// Out-of-range offsets clamp rather than panic on the rune slice.
	c.setValue("short", 99)
	if got := c.CursorOffset(); got != len("short") {
		t.Fatalf("caret at %d, want the end of the value", got)
	}
}

func TestComposerSnapCursorOutOfTokenFollowsAStepsDirection(t *testing.T) {
	c := newComposer("go " + Token(1) + " now")
	c.Attachments = []Attachment{{ID: 1, Path: "/tmp/a.png"}}
	span := c.tokenSpans()[0]

	setCursorAt(t, c, span.end-2)
	c.snapCursorOutOfToken(snapBack)
	if got := c.CursorOffset(); got != span.start {
		t.Fatalf("caret at %d, want the chip's start at %d", got, span.start)
	}

	setCursorAt(t, c, span.start+2)
	c.snapCursorOutOfToken(snapForward)
	if got := c.CursorOffset(); got != span.end {
		t.Fatalf("caret at %d, want the chip's end at %d", got, span.end)
	}
}

func TestComposerSnapCursorOutOfTokenTakesTheNearerEdge(t *testing.T) {
	c := newComposer("go " + Token(1) + " now")
	c.Attachments = []Attachment{{ID: 1, Path: "/tmp/a.png"}}
	span := c.tokenSpans()[0]

	setCursorAt(t, c, span.start+2)
	c.snapCursorOutOfToken(snapNearest)
	if got := c.CursorOffset(); got != span.start {
		t.Fatalf("caret at %d, want the chip's start at %d", got, span.start)
	}

	setCursorAt(t, c, span.end-2)
	c.snapCursorOutOfToken(snapNearest)
	if got := c.CursorOffset(); got != span.end {
		t.Fatalf("caret at %d, want the chip's end at %d", got, span.end)
	}

	// A caret outside every chip is left alone.
	setCursorAt(t, c, 1)
	c.snapCursorOutOfToken(snapNearest)
	if got := c.CursorOffset(); got != 1 {
		t.Fatalf("caret at %d, want it untouched outside a chip", got)
	}
}

func TestComposerPruneReleasesChipsCutByABulkEdit(t *testing.T) {
	first := tempImage(t, "first.png")
	second := tempImage(t, "second.png")
	c := newComposer("keep " + Token(1) + " and " + Token(2))
	c.Attachments = []Attachment{{ID: 1, Path: first}, {ID: 2, Path: second}}

	c.Input.SetValue("keep " + Token(2))
	c.prune()
	if len(c.Attachments) != 1 || c.Attachments[0].ID != 2 {
		t.Fatalf("attachments = %+v, want only the chip still in the text", c.Attachments)
	}
	if !fileGone(first) {
		t.Fatal("a chip cut out of the text should take its temp file with it")
	}
	if fileGone(second) {
		t.Fatal("the surviving chip's file must stay: the agent still opens it")
	}

	c.Release()
	if len(c.Attachments) != 0 || !fileGone(second) {
		t.Fatalf("release should empty the prompt's images: %+v", c.Attachments)
	}
}

func TestComposerInsertTokenSpacesOffTheWordsAroundIt(t *testing.T) {
	c := newComposer("look now")
	setCursorAt(t, c, len("look"))
	att := Attachment{ID: 1}
	c.insertToken(&att)
	if got := c.Input.Value(); got != "look "+Token(1)+" now" {
		t.Fatalf("value = %q, want the chip spaced off both words", got)
	}
	if !att.LeadPad || att.TrailPad {
		t.Fatalf("padding = lead %v trail %v, want only the space it added", att.LeadPad, att.TrailPad)
	}
}

func tempImage(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
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
			c.Input.EndOfBufferCharacter = '~'
			c.Input.SetWidth(width)
			c.Input.SetValue(value)
			_, total := c.DisplayRows()
			c.Input.SetHeight(200)
			lines := strings.Split(ansi.Strip(c.Input.View()), "\n")
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
			c.Input.SetValue("")
			c.Input.InsertString(value)
			_ = c.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
			prev, _ := c.DisplayRows()
			if prev != 0 {
				t.Errorf("width %d value %q: caret at start on row %d", width, value, prev)
			}
			for range len([]rune(value)) {
				_ = c.updateInput(tea.KeyMsg{Type: tea.KeyRight})
				row, _ := c.DisplayRows()
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

type fakeHost struct {
	errs    []string
	cleared int
}

func (h *fakeHost) ReportErr(text string) { h.errs = append(h.errs, text) }

func (h *fakeHost) ClearErr() { h.cleared++ }

// A clipboard result lands through the composer's host alone: a path fills
// its chip, and a failed read reports and takes the chip back out.
func TestComposerAcceptsImagesThroughAFakeHost(t *testing.T) {
	h := &fakeHost{}
	c := newComposer("look ")
	c.Input.CursorEnd()
	c.Gen = 3
	if _, handled := c.ChipKey(h, Quick, tea.KeyMsg{Type: tea.KeyCtrlV}); !handled || len(c.Attachments) != 1 {
		t.Fatalf("ctrl+v did not reserve a chip: %+v", c.Attachments)
	}
	first, shot := c.Attachments[0].ID, t.TempDir()+"/shot.png"
	c.AcceptImage(h, PasteImageMsg{Target: Quick, Gen: 3, ID: first, Path: shot}, true)
	if c.Attachments[0].Path != shot || h.cleared != 2 {
		t.Fatalf("path = %q cleared = %d", c.Attachments[0].Path, h.cleared)
	}
	if _, handled := c.ChipKey(h, Quick, tea.KeyMsg{Type: tea.KeyCtrlV}); !handled {
		t.Fatal("second paste not handled")
	}
	second := c.Attachments[1].ID
	c.AcceptImage(h, PasteImageMsg{Target: Quick, Gen: 3, ID: second, Err: os.ErrPermission}, true)
	if len(c.Attachments) != 1 || len(h.errs) != 1 {
		t.Fatalf("a failed read should drop its chip and report: %+v %v", c.Attachments, h.errs)
	}
}
