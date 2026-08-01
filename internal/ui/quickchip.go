package ui

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

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

// quickTokenSpans locates the chips of live attachments. Text that merely
// looks like a token (no attachment behind it) stays ordinary typing.
func (m *Model) quickTokenSpans() []tokenSpan {
	value := m.quick.input.Value()
	var spans []tokenSpan
	for _, match := range imageTokenPattern.FindAllStringSubmatchIndex(value, -1) {
		id, err := strconv.Atoi(value[match[2]:match[3]])
		if err != nil || m.quickAttachment(id) == nil {
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

func (m *Model) quickAttachment(id int) *quickAttachment {
	for i := range m.quick.attachments {
		if m.quick.attachments[i].id == id {
			return &m.quick.attachments[i]
		}
	}
	return nil
}

// quickPasting is true while a clipboard read is still in flight.
func (m *Model) quickPasting() bool {
	for _, att := range m.quick.attachments {
		if att.path == "" {
			return true
		}
	}
	return false
}

// dropQuickAttachment forgets an attachment and deletes the temp file it
// wrote, so a chip the user removed leaves nothing behind.
func (m *Model) dropQuickAttachment(id int) {
	kept := m.quick.attachments[:0]
	for _, att := range m.quick.attachments {
		if att.id != id {
			kept = append(kept, att)
			continue
		}
		if att.path != "" {
			_ = os.Remove(att.path)
		}
	}
	m.quick.attachments = kept
}

// pruneQuickAttachments drops attachments whose chip is no longer in the
// text: the catch-all for deletions that do not go through the chip-aware
// keys (ctrl+u, word delete, a fresh SetValue).
func (m *Model) pruneQuickAttachments() {
	if len(m.quick.attachments) == 0 {
		return
	}
	value := m.quick.input.Value()
	for _, att := range append([]quickAttachment(nil), m.quick.attachments...) {
		if !strings.Contains(value, imageToken(att.id)) {
			m.dropQuickAttachment(att.id)
		}
	}
}

// quickRoomForToken guards the paste against the prompt's char limit:
// textarea.InsertString truncates silently there, which would leave a
// half-written token and an image nothing points at. The two extra runes
// are the spacing the insert may add.
func (m *Model) quickRoomForToken(id int) bool {
	limit := m.quick.input.CharLimit
	if limit <= 0 {
		return true
	}
	return m.quick.input.Length()+utf8.RuneCountInString(imageToken(id))+2 <= limit
}

// releaseQuickAttachments deletes every pasted image the prompt is holding.
// Used when the prompt is abandoned, where the text goes too.
func (m *Model) releaseQuickAttachments() {
	for _, att := range append([]quickAttachment(nil), m.quick.attachments...) {
		m.dropQuickAttachment(att.id)
	}
}

// quickCursorOffset is the caret's rune offset into the whole prompt value.
func (m *Model) quickCursorOffset() int {
	rows := strings.Split(m.quick.input.Value(), "\n")
	offset := 0
	for i := 0; i < m.quick.input.Line() && i < len(rows); i++ {
		offset += utf8.RuneCountInString(rows[i]) + 1
	}
	return offset + m.quickCursorColumn()
}

// quickCursorColumn is the caret's rune offset into its own logical row,
// which is what textarea.SetCursor takes.
func (m *Model) quickCursorColumn() int {
	info := m.quick.input.LineInfo()
	return info.StartColumn + info.ColumnOffset
}

// tokenEndingAt / tokenStartingAt answer "is the caret against a chip",
// which is what makes a chip delete and step as one unit.
func (m *Model) tokenEndingAt(offset int) (tokenSpan, bool) {
	for _, span := range m.quickTokenSpans() {
		if span.end == offset {
			return span, true
		}
	}
	return tokenSpan{}, false
}

func (m *Model) tokenStartingAt(offset int) (tokenSpan, bool) {
	for _, span := range m.quickTokenSpans() {
		if span.start == offset {
			return span, true
		}
	}
	return tokenSpan{}, false
}

// setQuickValue rewrites the prompt with the caret left at the given rune
// offset. The textarea only exposes a column setter, so the value is laid
// down after the caret, the caret sent to the very beginning, and the head
// typed back in front of it.
func (m *Model) setQuickValue(value string, cursor int) tea.Cmd {
	runes := []rune(value)
	cursor = max(0, min(cursor, len(runes)))
	m.quick.input.SetValue(string(runes[cursor:]))
	var cmd tea.Cmd
	m.quick.input, cmd = m.quick.input.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'<'}, Alt: true})
	m.quick.input.InsertString(string(runes[:cursor]))
	return cmd
}

// removeQuickToken cuts a chip out of the text and releases its image.
func (m *Model) removeQuickToken(span tokenSpan) tea.Cmd {
	runes := []rune(m.quick.input.Value())
	if span.end > len(runes) {
		return nil
	}
	span = m.withPadding(span, runes)
	cursor := m.quickCursorOffset()
	switch {
	case cursor >= span.end:
		cursor -= span.length()
	case cursor > span.start:
		cursor = span.start
	}
	m.quick.input.SetHeight(quickBarMaxRows)
	cmd := m.setQuickValue(string(runes[:span.start])+string(runes[span.end:]), cursor)
	m.dropQuickAttachment(span.id)
	m.errBar.text = ""
	return cmd
}

// withPadding grows a span over the spacing the paste itself added, so
// removing a chip leaves the words around it as they were.
func (m *Model) withPadding(span tokenSpan, runes []rune) tokenSpan {
	att := m.quickAttachment(span.id)
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

// insertQuickToken drops a chip in at the caret, spaced off the words
// around it so the paths still read as separate arguments on submit.
func (m *Model) insertQuickToken(att *quickAttachment) {
	runes := []rune(m.quick.input.Value())
	cursor := m.quickCursorOffset()
	token := imageToken(att.id)
	if cursor > 0 && !isSpaceRune(runes[cursor-1]) {
		token = " " + token
		att.leadPad = true
	}
	if cursor >= len(runes) || !isSpaceRune(runes[cursor]) {
		token += " "
		att.trailPad = true
	}
	m.quick.input.SetHeight(quickBarMaxRows)
	m.quick.input.InsertString(token)
}

func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' || r == '\n' }

// snapQuickCursorOutOfToken keeps the caret off the inside of a chip, so
// the next keystroke can never land in the middle of one.
func (m *Model) snapQuickCursorOutOfToken() {
	offset := m.quickCursorOffset()
	for _, span := range m.quickTokenSpans() {
		if offset <= span.start || offset >= span.end {
			continue
		}
		column := m.quickCursorColumn()
		if offset-span.start <= span.end-offset {
			m.quick.input.SetCursor(column - (offset - span.start))
		} else {
			m.quick.input.SetCursor(column + (span.end - offset))
		}
		return
	}
}

// renderQuickChips paints the image tokens inside an already rendered
// prompt. The styling adds color only, never characters, so the wrapping
// the textarea computed still matches what the terminal draws.
func (m *Model) renderQuickChips(view string) string {
	for _, att := range m.quick.attachments {
		token := imageToken(att.id)
		if att.path == "" {
			view = strings.ReplaceAll(view, token, imageChipPasting(token))
			continue
		}
		view = strings.ReplaceAll(view, token, imageChip(token))
	}
	return view
}
