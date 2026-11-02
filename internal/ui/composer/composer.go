// Package composer is the prompt box that holds pasted images: their chips,
// the text they leave on send, and the keys that step, delete and paste them.
// The root owns where a box lives; the quick bar and the New Session form
// reach theirs through spawn.Prompt.
package composer

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// Composer is a prompt box that can hold pasted images. A paste lands at
// the caret as an "[Image #N]" token that renders as a chip and steps,
// deletes and wraps as one unit; on send each token becomes the path of
// the file the clipboard read wrote. The quick bar and the New Session
// form both compose prompts this way, so a first task can open with the
// screenshot that explains it.
type Composer struct {
	Input       textarea.Model
	Attachments []Attachment
	// Saved are the saved prompt chips, each with the text it was picked
	// with, which takes the chip's place on send.
	Saved       []spawn.Saved
	LastImageID int
	// MaxRows is the most rows View paints before it scrolls.
	MaxRows int
	top     int
	// Gen tells this box from the one that stood in the same place before
	// it. A clipboard read outlives the prompt that started it, and closing
	// a form and opening another is fast enough to beat one home; without
	// an identity, that read would type into a box its user never pasted in.
	Gen int
}

// Attachment is one pasted image: the id its token carries, and the temp
// file the clipboard read wrote (empty while that read is in flight).
type Attachment struct {
	ID   int
	Path string
	// Spacing the paste added around the token, given back when the chip
	// goes away so the sentence reads as it did before.
	LeadPad  bool
	TrailPad bool
}

// ID names the prompt box a clipboard read was started from, so the image
// lands in the one the user pasted into.
type ID int

const (
	Quick ID = iota
	Form
)

type Host interface {
	ReportErr(text string)
	ClearErr()
}

// PasteImageMsg is the result of an async clipboard image read started by
// a composer's ctrl+v handler.
type PasteImageMsg struct {
	Target  ID
	Gen     int
	ID      int
	Path    string
	Err     error
	NoImage bool
}

// PasteTextMsg carries a clipboard that turned out to hold text. The
// textarea reads its own clipboard into a message only it can interpret,
// and a command's message comes back to the app rather than to the widget
// that asked for it, so the read is wrapped here and handed over on
// arrival. Reading it in the command rather than on the way past keeps the
// exec off the update path.
type PasteTextMsg struct {
	Target ID
	Gen    int
	Inner  tea.Msg
}

// ReadClipboardText is the textarea's own clipboard read, seamed so a test
// can drive the fallback without an OS clipboard.
var ReadClipboardText = textarea.Paste

// PasteTextCmd reads the clipboard as text off the UI thread.
func PasteTextCmd(target ID, gen int) tea.Cmd {
	return func() tea.Msg {
		return PasteTextMsg{Target: target, Gen: gen, Inner: ReadClipboardText()}
	}
}

// CaptureImage is the clipboard image read behind a paste, seamed so a test
// can drive a paste without an OS clipboard.
var CaptureImage = func() (string, error) { return clipboard.SaveImage() }

// imageTokenPattern matches the plain-text token a pasted image leaves in
// the prompt value. The token carries the id, so a chip keeps its identity
// wherever the text around it moves.
var imageTokenPattern = regexp.MustCompile(`\[Image #(\d+)\]`)
var savedTokenPattern = regexp.MustCompile(`\[saved prompt:([^\[\]]+)\]`)

func Token(id int) string { return fmt.Sprintf("[Image #%d]", id) }

func SavedToken(name string) string { return "[saved prompt:" + name + "]" }

const (
	tokenImage = iota
	tokenSaved
)

type tokenSpan struct {
	kind  int
	id    int
	name  string
	start int
	end   int
}

func (s tokenSpan) length() int { return s.end - s.start }

// tokenSpans locates the chips of live attachments. Text that merely looks
// like a token (no attachment behind it) stays ordinary typing.
func (c *Composer) tokenSpans() []tokenSpan {
	value := c.Input.Value()
	var spans []tokenSpan
	for _, match := range imageTokenPattern.FindAllStringSubmatchIndex(value, -1) {
		id, err := strconv.Atoi(value[match[2]:match[3]])
		if err != nil || c.attachment(id) == nil {
			continue
		}
		spans = append(spans, tokenSpan{
			kind:  tokenImage,
			id:    id,
			start: utf8.RuneCountInString(value[:match[0]]),
			end:   utf8.RuneCountInString(value[:match[1]]),
		})
	}
	for _, match := range savedTokenPattern.FindAllStringSubmatchIndex(value, -1) {
		name := value[match[2]:match[3]]
		if c.savedChip(name) == nil {
			continue
		}
		spans = append(spans, tokenSpan{
			kind:  tokenSaved,
			name:  name,
			start: utf8.RuneCountInString(value[:match[0]]),
			end:   utf8.RuneCountInString(value[:match[1]]),
		})
	}
	slices.SortFunc(spans, func(a, b tokenSpan) int { return a.start - b.start })
	return spans
}

func (c *Composer) savedChip(name string) *spawn.Saved {
	for i := range c.Saved {
		if c.Saved[i].Name == name {
			return &c.Saved[i]
		}
	}
	return nil
}

// InsertSaved drops a saved prompt chip in at the caret. Picking a name
// already in the prompt takes the newer text for both.
func (c *Composer) InsertSaved(saved spawn.Saved) {
	if chip := c.savedChip(saved.Name); chip != nil {
		chip.Text = saved.Text
	} else {
		c.Saved = append(c.Saved, saved)
	}
	runes := []rune(c.Input.Value())
	cursor := c.CursorOffset()
	token := SavedToken(saved.Name)
	if cursor > 0 && !isSpaceRune(runes[cursor-1]) {
		token = " " + token
	}
	if cursor >= len(runes) || !isSpaceRune(runes[cursor]) {
		token += " "
	}
	c.Input.InsertString(token)
}

func (c *Composer) attachment(id int) *Attachment {
	for i := range c.Attachments {
		if c.Attachments[i].ID == id {
			return &c.Attachments[i]
		}
	}
	return nil
}

// Pasting is true while a clipboard read is still in flight.
func (c *Composer) Pasting() bool {
	for _, att := range c.Attachments {
		if att.Path == "" {
			return true
		}
	}
	return false
}

// drop forgets an attachment and deletes the temp file it wrote, so a chip
// the user removed leaves nothing behind.
func (c *Composer) drop(id int) {
	kept := c.Attachments[:0]
	for _, att := range c.Attachments {
		if att.ID != id {
			kept = append(kept, att)
			continue
		}
		if att.Path != "" {
			_ = os.Remove(att.Path)
		}
	}
	c.Attachments = kept
}

// prune drops attachments whose chip is no longer in the text: the
// catch-all for deletions that do not go through the chip-aware keys
// (ctrl+u, word delete, a fresh SetValue).
func (c *Composer) prune() {
	value := c.Input.Value()
	for _, att := range append([]Attachment(nil), c.Attachments...) {
		if !strings.Contains(value, Token(att.ID)) {
			c.drop(att.ID)
		}
	}
	c.Saved = slices.DeleteFunc(c.Saved, func(saved spawn.Saved) bool {
		return !strings.Contains(value, SavedToken(saved.Name))
	})
}

// roomForToken guards the paste against the prompt's char limit:
// textarea.InsertString truncates silently there, which would leave a
// half-written token and an image nothing points at. The two extra runes
// are the spacing the insert may add.
func (c *Composer) roomForToken(id int) bool {
	limit := c.Input.CharLimit
	if limit <= 0 {
		return true
	}
	return c.Input.Length()+utf8.RuneCountInString(Token(id))+2 <= limit
}

// Release deletes every pasted image the prompt is holding. Used when the
// prompt is abandoned, where the text goes too.
func (c *Composer) Release() {
	for _, att := range append([]Attachment(nil), c.Attachments...) {
		c.drop(att.ID)
	}
	c.Saved = nil
}

func (c *Composer) CursorOffset() int {
	rows := strings.Split(c.Input.Value(), "\n")
	offset := 0
	for i := 0; i < c.Input.Line() && i < len(rows); i++ {
		offset += utf8.RuneCountInString(rows[i]) + 1
	}
	return offset + c.cursorColumn()
}

// cursorColumn is the caret's rune offset into its own logical row, which
// is what textarea.SetCursor takes.
func (c *Composer) cursorColumn() int {
	info := c.Input.LineInfo()
	return info.StartColumn + info.ColumnOffset
}

// CaretOnFirstRow / CaretOnLastRow count soft-wrapped rows, so ↑↓ leave
// the prompt only from its edge rows.
func (c *Composer) CaretOnFirstRow() bool {
	return c.Input.Line() == 0 && c.Input.LineInfo().RowOffset == 0
}

func (c *Composer) CaretOnLastRow() bool {
	info := c.Input.LineInfo()
	return c.Input.Line() == c.Input.LineCount()-1 && info.RowOffset+1 >= info.Height
}

// tokenEndingAt / tokenStartingAt answer "is the caret against a chip",
// which is what makes a chip delete and step as one unit.
func (c *Composer) tokenEndingAt(offset int) (tokenSpan, bool) {
	for _, span := range c.tokenSpans() {
		if span.end == offset {
			return span, true
		}
	}
	return tokenSpan{}, false
}

func (c *Composer) tokenStartingAt(offset int) (tokenSpan, bool) {
	for _, span := range c.tokenSpans() {
		if span.start == offset {
			return span, true
		}
	}
	return tokenSpan{}, false
}

// updateInput hands the textarea a message, focusing it first when focus
// has moved on to another field. A blurred textarea returns from Update
// without reading the message, and a clipboard read lands whenever it
// lands: without this the caret rewrite below and a delivered text paste
// are both dropped on arrival.
func (c *Composer) updateInput(msg tea.Msg) tea.Cmd {
	blurred := !c.Input.Focused()
	if blurred {
		c.Input.Focus()
	}
	var cmd tea.Cmd
	c.Input, cmd = c.Input.Update(msg)
	if blurred {
		c.Input.Blur()
	}
	return cmd
}

// setValue rewrites the prompt with the caret left at the given rune
// offset. The textarea only exposes a column setter, so the value is laid
// down after the caret, the caret sent to the very beginning, and the head
// typed back in front of it. The beginning is reached by the InputBegin
// binding because the method behind it is unexported, and CursorStart is
// the start of the caret's own row rather than of the value.
func (c *Composer) setValue(value string, cursor int) tea.Cmd {
	runes := []rune(value)
	cursor = max(0, min(cursor, len(runes)))
	c.Input.SetValue(string(runes[cursor:]))
	cmd := c.updateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
	c.Input.InsertString(string(runes[:cursor]))
	return cmd
}

func (c *Composer) removeToken(span tokenSpan) tea.Cmd {
	runes := []rune(c.Input.Value())
	span = c.withPadding(span, runes)
	cursor := c.CursorOffset()
	switch {
	case cursor >= span.end:
		cursor -= span.length()
	case cursor > span.start:
		cursor = span.start
	}
	cmd := c.setValue(string(runes[:span.start])+string(runes[span.end:]), cursor)
	if span.kind == tokenImage {
		c.drop(span.id)
	} else {
		c.prune()
	}
	return cmd
}

// removeImage takes a chip out of the text by id, wherever it sits.
func (c *Composer) removeImage(id int) tea.Cmd {
	for _, span := range c.tokenSpans() {
		if span.id == id {
			return c.removeToken(span)
		}
	}
	c.drop(id)
	return nil
}

// withPadding grows a span over the spacing the paste itself added, so
// removing a chip leaves the words around it as they were.
func (c *Composer) withPadding(span tokenSpan, runes []rune) tokenSpan {
	if span.kind != tokenImage {
		return span
	}
	att := c.attachment(span.id)
	if att.LeadPad && span.start > 0 && runes[span.start-1] == ' ' {
		span.start--
	}
	if att.TrailPad && span.end < len(runes) && runes[span.end] == ' ' {
		span.end++
	}
	return span
}

// insertToken drops a chip in at the caret, spaced off the words around it
// so the paths still read as separate arguments on submit.
func (c *Composer) insertToken(att *Attachment) {
	runes := []rune(c.Input.Value())
	cursor := c.CursorOffset()
	token := Token(att.ID)
	if cursor > 0 && !isSpaceRune(runes[cursor-1]) {
		token = " " + token
		att.LeadPad = true
	}
	if cursor >= len(runes) || !isSpaceRune(runes[cursor]) {
		token += " "
		att.TrailPad = true
	}
	c.Input.InsertString(token)
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// snapEdge is the side of a chip the caret leaves by. A row step names its
// own direction: leaving by the nearer edge can put the caret back on the
// row the step just left, which reads as a dead key.
type snapEdge int

const (
	snapNearest snapEdge = iota
	snapBack
	snapForward
)

// snapCursorOutOfToken keeps the caret off the inside of a chip, so the
// next keystroke can never land in the middle of one.
func (c *Composer) snapCursorOutOfToken(edge snapEdge) {
	offset := c.CursorOffset()
	for _, span := range c.tokenSpans() {
		if offset <= span.start || offset >= span.end {
			continue
		}
		back := edge == snapBack
		if edge == snapNearest {
			back = offset-span.start <= span.end-offset
		}
		column := c.cursorColumn()
		if back {
			c.Input.SetCursor(column - (offset - span.start))
		} else {
			c.Input.SetCursor(column + (span.end - offset))
		}
		return
	}
}

// Message is the text delivered on submit: the typed prompt with each chip
// swapped back for its path, so the paths reach the agent in the order and
// the places the user pasted them.
func (c *Composer) Message() string {
	value := imageTokenPattern.ReplaceAllStringFunc(c.Input.Value(), func(token string) string {
		id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(token, "[Image #"), "]"))
		if err != nil {
			return token
		}
		att := c.attachment(id)
		if att == nil || att.Path == "" {
			return token
		}
		return att.Path
	})
	// A saved prompt's text goes in as written, so it is put in after the
	// trim.
	value = strings.TrimSpace(value)
	for _, saved := range c.Saved {
		value = strings.ReplaceAll(value, SavedToken(saved.Name), saved.Text)
	}
	return value
}

// HoldOpen keeps the textarea viewport at row zero so View owns scrolling.
func HoldOpen(input *textarea.Model) {
	input.MaxHeight = 0
	input.SetHeight(input.CharLimit + 1)
}

// Paint is how the root styles the prompt, handed in per frame: the caret
// marker on its textarea, and the color of a chip the clipboard read has
// filled and one still waiting on it.
type Paint struct {
	Input   func(textarea.Model) string
	Chip    func(token string) string
	Pasting func(token string) string
}

func (c *Composer) View(paint Paint) string {
	caret, total := c.DisplayRows()
	rows := max(min(total, c.MaxRows), 1)
	if caret < c.top {
		c.top = caret
	}
	if caret >= c.top+rows {
		c.top = caret - rows + 1
	}
	c.top = max(min(c.top, total-rows), 0)
	c.Input.SetHeight(total)
	lines := strings.Split(c.renderChips(paint.Input(c.Input), paint), "\n")
	HoldOpen(&c.Input)
	return strings.Join(lines[c.top:min(c.top+rows, len(lines))], "\n")
}

// DisplayRows walks copies to reuse textarea wrapping without moving the
// live caret. The caret's row and the prompt's row count are what View
// scrolls to.
func (c *Composer) DisplayRows() (caret, total int) {
	info := c.Input.LineInfo()
	caret, total = info.RowOffset, info.Height
	above := c.Input
	for above.Line() > 0 {
		above.CursorStart()
		above.CursorUp()
		height := above.LineInfo().Height
		caret += height
		total += height
	}
	below := c.Input
	for below.Line() < below.LineCount()-1 {
		below.CursorEnd()
		below.CursorDown()
		total += below.LineInfo().Height
	}
	return caret, total
}

// renderChips paints the image tokens inside an already rendered prompt.
// The styling adds color only, never characters, so the wrapping the
// textarea computed still matches what the terminal draws.
func (c *Composer) renderChips(view string, paint Paint) string {
	for _, att := range c.Attachments {
		token := Token(att.ID)
		if att.Path == "" {
			view = strings.ReplaceAll(view, token, paint.Pasting(token))
			continue
		}
		view = strings.ReplaceAll(view, token, paint.Chip(token))
	}
	for _, saved := range c.Saved {
		token := SavedToken(saved.Name)
		view = strings.ReplaceAll(view, token, paint.Chip(token))
	}
	return view
}

// TypeKey sends an ordinary keystroke to the input, then repairs what the
// edit did to the chips around the caret: one that got swallowed releases
// its image, and the caret never rests inside a chip.
func (c *Composer) TypeKey(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	c.Input, cmd = c.Input.Update(msg)
	c.prune()
	c.snapCursorOutOfToken(snapNearest)
	return cmd
}

// StepRow moves the caret one rendered row, soft-wrapped rows included, and
// reports whether the prompt had a row to move to. The false return is an
// edge row, which is where ↑↓ go back to meaning the list or the next field.
func (c *Composer) StepRow(msg tea.KeyMsg) (tea.Cmd, bool) {
	var edge snapEdge
	switch msg.String() {
	case "up":
		if c.CaretOnFirstRow() {
			return nil, false
		}
		edge = snapBack
	case "down":
		if c.CaretOnLastRow() {
			return nil, false
		}
		edge = snapForward
	default:
		return nil, false
	}
	var cmd tea.Cmd
	c.Input, cmd = c.Input.Update(msg)
	c.snapCursorOutOfToken(edge)
	return cmd, true
}

// paste reserves a chip at the caret and starts the clipboard read off the
// UI thread, so the chip holds the spot the user pasted at even while they
// keep typing. The false return is a prompt with no room for the token.
func (c *Composer) paste(target ID) (tea.Cmd, bool) {
	if !c.roomForToken(c.LastImageID + 1) {
		return nil, false
	}
	c.LastImageID++
	id := c.LastImageID
	c.Attachments = append(c.Attachments, Attachment{ID: id})
	c.insertToken(&c.Attachments[len(c.Attachments)-1])
	return captureImageCmd(target, c.Gen, id), true
}

// captureImageCmd reads the clipboard image off the UI thread and writes
// it once into the pastes directory, keeping the TUI responsive while the
// OS clipboard is read.
func captureImageCmd(target ID, gen, id int) tea.Cmd {
	return func() tea.Msg {
		path, err := CaptureImage()
		if err != nil {
			if errors.Is(err, clipboard.ErrNoImage) {
				return PasteImageMsg{Target: target, Gen: gen, ID: id, NoImage: true}
			}
			return PasteImageMsg{Target: target, Gen: gen, ID: id, Err: err}
		}
		return PasteImageMsg{Target: target, Gen: gen, ID: id, Path: path}
	}
}

// ChipKey handles the keys a prompt box with chips owns: pasting an image,
// stepping over a chip, and deleting one whole. The false return is every
// other key, which the caller types into the input itself.
func (c *Composer) ChipKey(h Host, target ID, msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "ctrl+v":
		if c.Pasting() {
			return nil, true
		}
		cmd, ok := c.paste(target)
		if !ok {
			h.ReportErr("prompt is full - shorten it before pasting an image")
			return nil, true
		}
		h.ClearErr()
		return cmd, true
	case "left":
		if span, ok := c.tokenEndingAt(c.CursorOffset()); ok {
			c.Input.SetCursor(c.cursorColumn() - span.length())
			return nil, true
		}
	case "right":
		if span, ok := c.tokenStartingAt(c.CursorOffset()); ok {
			c.Input.SetCursor(c.cursorColumn() + span.length())
			return nil, true
		}
	case "backspace", "ctrl+h":
		if span, ok := c.tokenEndingAt(c.CursorOffset()); ok {
			cmd := c.removeToken(span)
			h.ClearErr()
			return cmd, true
		}
	case "delete":
		if span, ok := c.tokenStartingAt(c.CursorOffset()); ok {
			cmd := c.removeToken(span)
			h.ClearErr()
			return cmd, true
		}
	}
	return nil, false
}

// AcceptImage applies an async clipboard result to the chip the paste
// reserved: the path fills it in, a real error surfaces and takes the chip
// back out, and no-image falls through to a text paste. open says the box
// is still on screen.
func (c *Composer) AcceptImage(h Host, msg PasteImageMsg, open bool) tea.Cmd {
	// A read started in a box that has since been closed and replaced: the
	// one standing there now never asked for it, and its own chips are not
	// this message's to take away.
	if c.Gen != msg.Gen {
		if msg.Path != "" {
			_ = os.Remove(msg.Path)
		}
		return nil
	}
	att := c.attachment(msg.ID)
	if att == nil || !open {
		if msg.Path != "" {
			_ = os.Remove(msg.Path)
		}
		if att != nil {
			c.drop(msg.ID)
		}
		return nil
	}
	if msg.Err != nil {
		cmd := c.removeImage(msg.ID)
		h.ReportErr(msg.Err.Error())
		return cmd
	}
	if msg.NoImage {
		cmd := c.removeImage(msg.ID)
		return tea.Batch(cmd, PasteTextCmd(msg.Target, c.Gen))
	}
	att.Path = msg.Path
	h.ClearErr()
	return nil
}

// AcceptText delivers a text clipboard to the box that asked for it, as
// though the paste had been typed there.
func (c *Composer) AcceptText(msg PasteTextMsg, open bool) tea.Cmd {
	if c.Gen != msg.Gen || !open {
		return nil
	}
	cmd := c.updateInput(msg.Inner)
	c.prune()
	c.snapCursorOutOfToken(snapNearest)
	return cmd
}
