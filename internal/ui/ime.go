package ui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	cursorAnchorValid = uint64(1) << 63
	// The marker never reaches the terminal. It travels through the same
	// layout and clipping as the cursor cell, then View removes it after
	// reading the cell's final screen coordinates.
	cursorAnchorMarker = "\x1b]1337;agent-manager-ime-cursor\x07"
)

// cursorAnchor is the real terminal cell an input method should open beside.
// Bubble Tea v1 finishes every alternate-screen render at the bottom-left,
// while focus mode paints a pane caret elsewhere, so an IME otherwise follows
// that renderer cursor instead of the caret the user is typing at.
type cursorAnchor struct {
	packed atomic.Uint64
}

func (a *cursorAnchor) set(col, row int, ok bool) {
	if !ok || col < 1 || row < 1 {
		a.packed.Store(0)
		return
	}
	a.packed.Store(cursorAnchorValid | uint64(uint32(row))<<32 | uint64(uint32(col)))
}

func (a *cursorAnchor) get() (col, row int, ok bool) {
	packed := a.packed.Load()
	if packed&cursorAnchorValid == 0 {
		return 0, 0, false
	}
	return int(uint32(packed)), int(uint32((packed &^ cursorAnchorValid) >> 32)), true
}

// syncCursorAnchor publishes the input coordinates in a finished frame and
// removes the private marker before Bubble Tea writes the frame. Focus mode
// uses its recorded pane box as a fallback because its caret is painted over
// captured terminal output rather than by a Bubbles input widget.
func (m *Model) syncCursorAnchor(frame string) string {
	if m.imeCursor == nil {
		return strings.ReplaceAll(frame, cursorAnchorMarker, "")
	}
	if m.mode == modeFocus {
		col, row, ok := m.focusCursorAnchor()
		m.imeCursor.set(col, row, ok)
		return strings.ReplaceAll(frame, cursorAnchorMarker, "")
	}
	frame, col, row, ok := cursorMarkerPosition(frame)
	m.imeCursor.set(col, row, ok)
	return frame
}

func cursorMarkerPosition(frame string) (clean string, col, row int, ok bool) {
	index := strings.Index(frame, cursorAnchorMarker)
	if index < 0 {
		return frame, 0, 0, false
	}
	clean = strings.ReplaceAll(frame, cursorAnchorMarker, "")
	prefix := frame[:index]
	row = strings.Count(prefix, "\n") + 1
	if newline := strings.LastIndexByte(prefix, '\n'); newline >= 0 {
		prefix = prefix[newline+1:]
	}
	return clean, ansi.StringWidth(prefix) + 1, row, true
}

// textInputView and textAreaView preserve the widgets' visual blink while
// marking the cursor cell in both blink phases. A visible-cursor copy reveals
// the exact rendered cell; when the real cursor is blinked off, the marker is
// inserted at that same ANSI-aware cell in the normal view.
func textInputView(input textinput.Model) string {
	if !input.Focused() {
		return input.View()
	}
	marked := input
	marked.Cursor.Blink = false
	marked.Cursor.Style = cursorMarkerStyle(marked.Cursor.Style)
	markedView := marked.View()
	if !input.Cursor.Blink {
		return markedView
	}
	return insertMarkerAtCursor(input.View(), markedView)
}

func textAreaView(input textarea.Model) string {
	if !input.Focused() {
		return input.View()
	}
	marked := input
	marked.Cursor.Blink = false
	marked.Cursor.Style = cursorMarkerStyle(marked.Cursor.Style)
	markedView := marked.View()
	if !input.Cursor.Blink {
		return markedView
	}
	return insertMarkerAtCursor(input.View(), markedView)
}

func cursorMarkerStyle(style lipgloss.Style) lipgloss.Style {
	transform := style.GetTransform()
	return style.Transform(func(value string) string {
		if transform != nil {
			value = transform(value)
		}
		return cursorAnchorMarker + value
	})
}

func insertMarkerAtCursor(view, markedView string) string {
	_, col, row, ok := cursorMarkerPosition(markedView)
	if !ok {
		return view
	}
	lines := strings.Split(view, "\n")
	row--
	col--
	if row < 0 || row >= len(lines) {
		return view
	}
	line := lines[row]
	cell, state := 0, byte(ansi.NormalState)
	for index := 0; index < len(line); {
		_, width, n, nextState := ansi.GraphemeWidth.DecodeSequenceInString(line[index:], state, nil)
		if n <= 0 {
			break
		}
		if width > 0 && cell+width > col {
			lines[row] = line[:index] + cursorAnchorMarker + line[index:]
			return strings.Join(lines, "\n")
		}
		cell += width
		index += n
		state = nextState
	}
	if cell == col {
		lines[row] += cursorAnchorMarker
		return strings.Join(lines, "\n")
	}
	return view
}

func (m *Model) focusCursorAnchor() (col, row int, ok bool) {
	if m.mode != modeFocus || m.scrolledBack() || !m.pane.box.ok || !m.pane.cursor.ok {
		return 0, 0, false
	}
	sess, selected := m.selected()
	if !selected || m.pane.forID != sess.ID {
		return 0, 0, false
	}
	box := m.pane.box
	row = m.pane.cursor.y - m.paneRowOffset(box.height)
	if row < 0 || row >= box.height {
		return 0, 0, false
	}
	col = min(max(m.pane.cursor.x, 0), box.width-1)
	return box.x + col + 1, box.y + row + 1, true
}

// cursorOutputWriter appends the active input position after each Bubble Tea
// render. It tracks alternate-screen lifecycle so an attached editor and the
// restored shell receive their output untouched.
type cursorOutputWriter struct {
	out             io.Writer
	anchor          *cursorAnchor
	mu              sync.Mutex
	altScreen       bool
	altScreenPrefix []byte
}

func (w *cursorOutputWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := w.out.Write(p)
	if err != nil || n != len(p) {
		return n, err
	}
	if incomplete := w.trackAltScreen(p); !w.altScreen || incomplete {
		return n, nil
	}
	col, row, ok := w.anchor.get()
	if !ok {
		return n, nil
	}
	_, err = io.WriteString(w.out, ansi.CursorPosition(col, row))
	return n, err
}

func (w *cursorOutputWriter) trackAltScreen(p []byte) bool {
	data := p
	if len(w.altScreenPrefix) > 0 {
		data = make([]byte, len(w.altScreenPrefix)+len(p))
		copy(data, w.altScreenPrefix)
		copy(data[len(w.altScreenPrefix):], p)
	}
	enterSequence := []byte(ansi.SetModeAltScreenSaveCursor)
	exitSequence := []byte(ansi.ResetModeAltScreenSaveCursor)
	enter := bytes.LastIndex(data, enterSequence)
	exit := bytes.LastIndex(data, exitSequence)
	if enter < 0 && exit < 0 {
		w.altScreenPrefix = trailingAltScreenPrefix(data, enterSequence, exitSequence)
		return len(w.altScreenPrefix) > 0
	}
	w.altScreen = enter > exit
	w.altScreenPrefix = trailingAltScreenPrefix(data, enterSequence, exitSequence)
	return len(w.altScreenPrefix) > 0
}

func trailingAltScreenPrefix(data, enter, exit []byte) []byte {
	for size := min(len(data), len(enter)-1); size > 0; size-- {
		suffix := data[len(data)-size:]
		if bytes.Equal(suffix, enter[:size]) || bytes.Equal(suffix, exit[:size]) {
			return bytes.Clone(suffix)
		}
	}
	return nil
}

// cursorTTYOutput preserves stdout's terminal identity for Bubble Tea while
// overriding writes with the cursor anchor behavior above.
type cursorTTYOutput struct {
	*os.File
	writer *cursorOutputWriter
}

func (w *cursorTTYOutput) Write(p []byte) (int, error) { return w.writer.Write(p) }

// *os.File's promoted WriteString would bypass Write when Bubble Tea emits
// alternate-screen lifecycle sequences through io.WriteString.
func (w *cursorTTYOutput) WriteString(s string) (int, error) {
	return w.writer.Write([]byte(s))
}

// CursorOutput keeps native TTY detection and terminal sizing while placing
// the host cursor at the active input caret after each rendered frame.
func (m *Model) CursorOutput(output *os.File) io.Writer {
	if m.imeCursor == nil {
		m.imeCursor = &cursorAnchor{}
	}
	return &cursorTTYOutput{
		File: output,
		writer: &cursorOutputWriter{
			out:    output,
			anchor: m.imeCursor,
		},
	}
}
