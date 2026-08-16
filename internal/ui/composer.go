package ui

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// composer is a prompt box that can hold pasted images. A paste lands at
// the caret as an "[Image #N]" token that renders as a chip and steps,
// deletes and wraps as one unit; on send each token becomes the path of
// the file the clipboard read wrote. The quick bar and the New Session
// form both compose prompts this way, so a first task can open with the
// screenshot that explains it.
type composer struct {
	input       textarea.Model
	attachments []imageAttachment
	lastImageID int
	// maxRows is the height the input is pinned to before an Update, which
	// repositions its viewport against the height set at the last render; a
	// keystroke adding a wrapped row would otherwise scroll the first row
	// away for good.
	maxRows int
}

// imageAttachment is one pasted image: the id its token carries, and the
// temp file the clipboard read wrote (empty while that read is in flight).
type imageAttachment struct {
	id   int
	path string
	// Spacing the paste added around the token, given back when the chip
	// goes away so the sentence reads as it did before.
	leadPad  bool
	trailPad bool
}

// composerID names the prompt box a clipboard read was started from, so
// the image lands in the one the user pasted into.
type composerID int

const (
	composerQuick composerID = iota
	composerForm
)

// pasteImageMsg is the result of an async clipboard image read started by
// a composer's ctrl+v handler.
type pasteImageMsg struct {
	target  composerID
	id      int
	path    string
	err     error
	noImage bool
}

// imageTokenPattern matches the plain-text token a pasted image leaves in
// the prompt value. The token carries the id, so a chip keeps its identity
// wherever the text around it moves.
var imageTokenPattern = regexp.MustCompile(`\[Image #(\d+)\]`)

func imageToken(id int) string { return fmt.Sprintf("[Image #%d]", id) }

// tokenSpan is one chip's place in the prompt value, in rune offsets.
type tokenSpan struct {
	id    int
	start int
	end   int
}

func (s tokenSpan) length() int { return s.end - s.start }

// tokenSpans locates the chips of live attachments. Text that merely looks
// like a token (no attachment behind it) stays ordinary typing.
func (c *composer) tokenSpans() []tokenSpan {
	value := c.input.Value()
	var spans []tokenSpan
	for _, match := range imageTokenPattern.FindAllStringSubmatchIndex(value, -1) {
		id, err := strconv.Atoi(value[match[2]:match[3]])
		if err != nil || c.attachment(id) == nil {
			continue
		}
		spans = append(spans, tokenSpan{
			id:    id,
			start: utf8.RuneCountInString(value[:match[0]]),
			end:   utf8.RuneCountInString(value[:match[1]]),
		})
	}
	return spans
}

func (c *composer) attachment(id int) *imageAttachment {
	for i := range c.attachments {
		if c.attachments[i].id == id {
			return &c.attachments[i]
		}
	}
	return nil
}

// pasting is true while a clipboard read is still in flight.
func (c *composer) pasting() bool {
	for _, att := range c.attachments {
		if att.path == "" {
			return true
		}
	}
	return false
}

// drop forgets an attachment and deletes the temp file it wrote, so a chip
// the user removed leaves nothing behind.
func (c *composer) drop(id int) {
	kept := c.attachments[:0]
	for _, att := range c.attachments {
		if att.id != id {
			kept = append(kept, att)
			continue
		}
		if att.path != "" {
			_ = os.Remove(att.path)
		}
	}
	c.attachments = kept
}

// prune drops attachments whose chip is no longer in the text: the
// catch-all for deletions that do not go through the chip-aware keys
// (ctrl+u, word delete, a fresh SetValue).
func (c *composer) prune() {
	if len(c.attachments) == 0 {
		return
	}
	value := c.input.Value()
	for _, att := range append([]imageAttachment(nil), c.attachments...) {
		if !strings.Contains(value, imageToken(att.id)) {
			c.drop(att.id)
		}
	}
}

// roomForToken guards the paste against the prompt's char limit:
// textarea.InsertString truncates silently there, which would leave a
// half-written token and an image nothing points at. The two extra runes
// are the spacing the insert may add.
func (c *composer) roomForToken(id int) bool {
	limit := c.input.CharLimit
	if limit <= 0 {
		return true
	}
	return c.input.Length()+utf8.RuneCountInString(imageToken(id))+2 <= limit
}

// release deletes every pasted image the prompt is holding. Used when the
// prompt is abandoned, where the text goes too.
func (c *composer) release() {
	for _, att := range append([]imageAttachment(nil), c.attachments...) {
		c.drop(att.id)
	}
}

// cursorOffset is the caret's rune offset into the whole prompt value.
func (c *composer) cursorOffset() int {
	rows := strings.Split(c.input.Value(), "\n")
	offset := 0
	for i := 0; i < c.input.Line() && i < len(rows); i++ {
		offset += utf8.RuneCountInString(rows[i]) + 1
	}
	return offset + c.cursorColumn()
}

// cursorColumn is the caret's rune offset into its own logical row, which
// is what textarea.SetCursor takes.
func (c *composer) cursorColumn() int {
	info := c.input.LineInfo()
	return info.StartColumn + info.ColumnOffset
}

// tokenEndingAt / tokenStartingAt answer "is the caret against a chip",
// which is what makes a chip delete and step as one unit.
func (c *composer) tokenEndingAt(offset int) (tokenSpan, bool) {
	for _, span := range c.tokenSpans() {
		if span.end == offset {
			return span, true
		}
	}
	return tokenSpan{}, false
}

func (c *composer) tokenStartingAt(offset int) (tokenSpan, bool) {
	for _, span := range c.tokenSpans() {
		if span.start == offset {
			return span, true
		}
	}
	return tokenSpan{}, false
}

// setValue rewrites the prompt with the caret left at the given rune
// offset. The textarea only exposes a column setter, so the value is laid
// down after the caret, the caret sent to the very beginning, and the head
// typed back in front of it.
func (c *composer) setValue(value string, cursor int) tea.Cmd {
	runes := []rune(value)
	cursor = max(0, min(cursor, len(runes)))
	c.input.SetValue(string(runes[cursor:]))
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
	c.input.InsertString(string(runes[:cursor]))
	return cmd
}

// removeToken cuts a chip out of the text and releases its image.
func (c *composer) removeToken(span tokenSpan) tea.Cmd {
	runes := []rune(c.input.Value())
	span = c.withPadding(span, runes)
	cursor := c.cursorOffset()
	switch {
	case cursor >= span.end:
		cursor -= span.length()
	case cursor > span.start:
		cursor = span.start
	}
	c.input.SetHeight(c.maxRows)
	cmd := c.setValue(string(runes[:span.start])+string(runes[span.end:]), cursor)
	c.drop(span.id)
	return cmd
}

// removeImage takes a chip out of the text by id, wherever it sits.
func (c *composer) removeImage(id int) tea.Cmd {
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
func (c *composer) withPadding(span tokenSpan, runes []rune) tokenSpan {
	att := c.attachment(span.id)
	if att == nil {
		return span
	}
	if att.leadPad && span.start > 0 && runes[span.start-1] == ' ' {
		span.start--
	}
	if att.trailPad && span.end < len(runes) && runes[span.end] == ' ' {
		span.end++
	}
	return span
}

// insertToken drops a chip in at the caret, spaced off the words around it
// so the paths still read as separate arguments on submit.
func (c *composer) insertToken(att *imageAttachment) {
	runes := []rune(c.input.Value())
	cursor := c.cursorOffset()
	token := imageToken(att.id)
	if cursor > 0 && !isSpaceRune(runes[cursor-1]) {
		token = " " + token
		att.leadPad = true
	}
	if cursor >= len(runes) || !isSpaceRune(runes[cursor]) {
		token += " "
		att.trailPad = true
	}
	c.input.SetHeight(c.maxRows)
	c.input.InsertString(token)
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// snapCursorOutOfToken keeps the caret off the inside of a chip, so the
// next keystroke can never land in the middle of one.
func (c *composer) snapCursorOutOfToken() {
	offset := c.cursorOffset()
	for _, span := range c.tokenSpans() {
		if offset <= span.start || offset >= span.end {
			continue
		}
		column := c.cursorColumn()
		if offset-span.start <= span.end-offset {
			c.input.SetCursor(column - (offset - span.start))
		} else {
			c.input.SetCursor(column + (span.end - offset))
		}
		return
	}
}

// message is the text delivered on submit: the typed prompt with each chip
// swapped back for its path, so the paths reach the agent in the order and
// the places the user pasted them.
func (c *composer) message() string {
	value := imageTokenPattern.ReplaceAllStringFunc(c.input.Value(), func(token string) string {
		id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(token, "[Image #"), "]"))
		if err != nil {
			return token
		}
		att := c.attachment(id)
		if att == nil || att.path == "" {
			return token
		}
		return att.path
	})
	return strings.TrimSpace(value)
}

// renderChips paints the image tokens inside an already rendered prompt.
// The styling adds color only, never characters, so the wrapping the
// textarea computed still matches what the terminal draws.
func (c *composer) renderChips(view string) string {
	for _, att := range c.attachments {
		token := imageToken(att.id)
		if att.path == "" {
			view = strings.ReplaceAll(view, token, imageChipPasting(token))
			continue
		}
		view = strings.ReplaceAll(view, token, imageChip(token))
	}
	return view
}

// typeKey sends an ordinary keystroke to the input, then repairs what the
// edit did to the chips around the caret: one that got swallowed releases
// its image, and the caret never rests inside a chip.
func (c *composer) typeKey(msg tea.KeyMsg) tea.Cmd {
	c.input.SetHeight(c.maxRows)
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	c.prune()
	c.snapCursorOutOfToken()
	return cmd
}

// paste reserves a chip at the caret and starts the clipboard read off the
// UI thread, so the chip holds the spot the user pasted at even while they
// keep typing. The false return is a prompt with no room for the token.
func (c *composer) paste(target composerID) (tea.Cmd, bool) {
	if !c.roomForToken(c.lastImageID + 1) {
		return nil, false
	}
	c.lastImageID++
	id := c.lastImageID
	c.attachments = append(c.attachments, imageAttachment{id: id})
	c.insertToken(&c.attachments[len(c.attachments)-1])
	return captureImageCmd(target, id), true
}

// captureImageCmd reads the clipboard image off the UI thread and writes
// it once into the pastes directory, keeping the TUI responsive while the
// OS clipboard is read.
func captureImageCmd(target composerID, id int) tea.Cmd {
	return func() tea.Msg {
		path, err := captureClipboardImage()
		if err != nil {
			if errors.Is(err, clipboard.ErrNoImage) {
				return pasteImageMsg{target: target, id: id, noImage: true}
			}
			return pasteImageMsg{target: target, id: id, err: err}
		}
		return pasteImageMsg{target: target, id: id, path: path}
	}
}

// composerFor is the prompt box a target names, whatever screen is up.
func (m *Model) composerFor(target composerID) *composer {
	switch target {
	case composerQuick:
		return &m.quick.composer
	case composerForm:
		return &m.form.prompt
	}
	return nil
}

// composerOpen reports whether that prompt box is still on screen, which
// is what says an image still has somewhere to land.
func (m *Model) composerOpen(target composerID) bool {
	switch target {
	case composerQuick:
		return m.quick.active
	case composerForm:
		return m.mode == modeForm
	}
	return false
}

// composerKey handles the keys a prompt box with chips owns: pasting an
// image, stepping over a chip, and deleting one whole. The false return is
// every other key, which the caller types into the input itself.
func (m *Model) composerKey(target composerID, msg tea.KeyMsg) (tea.Cmd, bool) {
	c := m.composerFor(target)
	if c == nil {
		return nil, false
	}
	switch msg.String() {
	case "ctrl+v":
		if c.pasting() {
			return nil, true
		}
		cmd, ok := c.paste(target)
		if !ok {
			m.errBar.text = "prompt is full - shorten it before pasting an image"
			return nil, true
		}
		m.errBar.text = ""
		return cmd, true
	case "left":
		if span, ok := c.tokenEndingAt(c.cursorOffset()); ok {
			c.input.SetCursor(c.cursorColumn() - span.length())
			return nil, true
		}
	case "right":
		if span, ok := c.tokenStartingAt(c.cursorOffset()); ok {
			c.input.SetCursor(c.cursorColumn() + span.length())
			return nil, true
		}
	case "backspace", "ctrl+h":
		if span, ok := c.tokenEndingAt(c.cursorOffset()); ok {
			cmd := c.removeToken(span)
			m.errBar.text = ""
			return cmd, true
		}
	case "delete":
		if span, ok := c.tokenStartingAt(c.cursorOffset()); ok {
			cmd := c.removeToken(span)
			m.errBar.text = ""
			return cmd, true
		}
	}
	return nil, false
}

// handlePasteImageMsg applies an async clipboard result to the chip the
// paste reserved: the path fills it in, a real error surfaces and takes
// the chip back out, and no-image falls through to a text paste.
func (m *Model) handlePasteImageMsg(msg pasteImageMsg) (tea.Model, tea.Cmd) {
	c := m.composerFor(msg.target)
	if c == nil {
		if msg.path != "" {
			_ = os.Remove(msg.path)
		}
		return m, nil
	}
	att := c.attachment(msg.id)
	if att == nil || !m.composerOpen(msg.target) {
		if msg.path != "" {
			_ = os.Remove(msg.path)
		}
		if att != nil {
			c.drop(msg.id)
		}
		return m, nil
	}
	if msg.err != nil {
		cmd := c.removeImage(msg.id)
		m.errBar.text = msg.err.Error()
		return m, cmd
	}
	if msg.noImage {
		cmd := c.removeImage(msg.id)
		c.input.SetHeight(c.maxRows)
		var pasteCmd tea.Cmd
		c.input, pasteCmd = c.input.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
		return m, tea.Batch(cmd, pasteCmd)
	}
	att.path = msg.path
	m.errBar.text = ""
	return m, nil
}
