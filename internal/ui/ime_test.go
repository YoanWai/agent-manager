package ui

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestFocusCursorAnchorTracksMirroredCaret(t *testing.T) {
	m := &Model{
		mode:      modeFocus,
		rows:      []treeRow{{sess: store.Session{ID: "focused"}}},
		preview:   "first\nsecond\nthird\n",
		cursorOn:  false,
		imeCursor: &cursorAnchor{},
		pane: paneMirror{
			forID:  "focused",
			box:    paneBox{x: 40, y: 8, width: 30, height: 3, ok: true},
			cursor: paneCursor{x: 7, y: 1, ok: true},
		},
	}
	m.syncCursorAnchor("frame")
	col, row, ok := m.imeCursor.get()
	if !ok || col != 48 || row != 10 {
		t.Fatalf("cursor anchor = (%d, %d, %v), want (48, 10, true)", col, row, ok)
	}

	m.focusScroll = 1
	m.syncCursorAnchor("frame")
	if _, _, ok := m.imeCursor.get(); ok {
		t.Fatal("scrolled pane kept a live IME cursor anchor")
	}
}

func TestFocusCursorAnchorRemovesListSearchMarker(t *testing.T) {
	m := &Model{
		mode:      modeFocus,
		searching: true,
		search:    "active",
		rows:      []treeRow{{sess: store.Session{ID: "focused"}}},
		imeCursor: &cursorAnchor{},
		pane: paneMirror{
			forID:  "focused",
			box:    paneBox{x: 40, y: 8, width: 30, height: 3, ok: true},
			cursor: paneCursor{x: 7, y: 1, ok: true},
		},
	}
	frame := m.syncCursorAnchor(m.searchFieldLine(40) + "\nfocused pane")
	if strings.Contains(frame, cursorAnchorMarker) {
		t.Fatal("private list search marker leaked from the focused frame")
	}
	col, row, ok := m.imeCursor.get()
	if !ok || col != 48 || row != 10 {
		t.Fatalf("cursor anchor = (%d, %d, %v), want (48, 10, true)", col, row, ok)
	}
}

func TestFocusCursorAnchorAccountsForDroppedCaptureRows(t *testing.T) {
	m := &Model{
		mode:      modeFocus,
		rows:      []treeRow{{sess: store.Session{ID: "focused"}}},
		preview:   "one\ntwo\nthree\nfour\n",
		imeCursor: &cursorAnchor{},
		pane: paneMirror{
			forID:  "focused",
			box:    paneBox{x: 20, y: 5, width: 12, height: 2, ok: true},
			cursor: paneCursor{x: 4, y: 3, ok: true},
		},
	}
	m.syncCursorAnchor("frame")
	col, row, ok := m.imeCursor.get()
	if !ok || col != 25 || row != 7 {
		t.Fatalf("cropped cursor anchor = (%d, %d, %v), want (25, 7, true)", col, row, ok)
	}
}

func TestTextInputCursorMarkerSurvivesBothBlinkPhases(t *testing.T) {
	input := textField("", 60)
	input.Prompt = "» "
	input.SetValue("甲x")
	input.SetCursor(1)
	input.Focus()

	var position [2]int
	for _, blink := range []bool{false, true} {
		input.Cursor.Blink = blink
		view := textInputView(input)
		if !strings.Contains(view, cursorAnchorMarker) {
			t.Fatalf("blink=%v: input view has no cursor marker", blink)
		}
		clean, col, row, ok := cursorMarkerPosition(view)
		if !ok {
			t.Fatalf("blink=%v: cursor marker was not found", blink)
		}
		if strings.Contains(clean, cursorAnchorMarker) {
			t.Fatalf("blink=%v: cursor marker leaked into clean view", blink)
		}
		if row != 1 || col != 5 { // two prompt cells, then the two-cell CJK rune
			t.Fatalf("blink=%v: cursor = (%d, %d), want (5, 1)", blink, col, row)
		}
		if blink {
			if position != [2]int{col, row} {
				t.Fatalf("blink-off cursor moved from %v to (%d, %d)", position, col, row)
			}
		} else {
			position = [2]int{col, row}
		}
	}
}

func TestTextAreaCursorMarkerTracksWrappedWideText(t *testing.T) {
	input := promptField().input
	input.Placeholder = ""
	input.SetWidth(6)
	input.SetHeight(2)
	input.SetValue("甲乙ab")
	input.SetCursor(3)
	input.Focus()

	var position [2]int
	for _, blink := range []bool{false, true} {
		input.Cursor.Blink = blink
		view := textAreaView(input)
		_, col, row, ok := cursorMarkerPosition(view)
		if !ok {
			t.Fatalf("blink=%v: textarea view has no cursor marker", blink)
		}
		if col != 4 || row != 2 {
			t.Fatalf("blink=%v: textarea cursor = (%d, %d), want (4, 2)", blink, col, row)
		}
		if blink && position != [2]int{col, row} {
			t.Fatalf("blink-off textarea cursor moved from %v to (%d, %d)", position, col, row)
		}
		position = [2]int{col, row}
	}
}

func TestCursorMarkerPositionHandlesANSIAndWideGlyphs(t *testing.T) {
	frame := "first\n\x1b[31m甲乙\x1b[0m" + cursorAnchorMarker + "x"
	clean, col, row, ok := cursorMarkerPosition(frame)
	if !ok || col != 5 || row != 2 {
		t.Fatalf("cursor = (%d, %d, %v), want (5, 2, true)", col, row, ok)
	}
	if strings.Contains(clean, cursorAnchorMarker) || ansi.Strip(clean) != "first\n甲乙x" {
		t.Fatalf("clean frame = %q", clean)
	}
}

func TestPreviewLineStripsCapturedCursorMarker(t *testing.T) {
	got := previewLine("pane"+cursorAnchorMarker+"output", 40)
	if strings.Contains(got, cursorAnchorMarker) {
		t.Fatal("captured pane retained the private cursor marker")
	}
	if plain := strings.TrimSpace(ansi.Strip(got)); plain != "paneoutput" {
		t.Fatalf("sanitized pane = %q, want paneoutput", plain)
	}
}

func TestCustomSearchCursorsEmitMarkersOnlyWhileTyping(t *testing.T) {
	m := &Model{searching: true, search: "中文", help: helpState{searching: true, query: "定位"}}
	if line := m.searchFieldLine(40); !strings.Contains(line, cursorAnchorMarker) {
		t.Fatal("list search cursor has no marker")
	}
	if line := m.helpSearchLine(nil); !strings.Contains(line, cursorAnchorMarker) {
		t.Fatal("help search cursor has no marker")
	}

	m.searching = false
	m.help.searching = false
	if line := m.searchFieldLine(40); strings.Contains(line, cursorAnchorMarker) {
		t.Fatal("closed list search kept a cursor marker")
	}
	if line := m.helpSearchLine(nil); strings.Contains(line, cursorAnchorMarker) {
		t.Fatal("closed help search kept a cursor marker")
	}
}

func TestNoActiveInputClearsCursorAnchor(t *testing.T) {
	m := &Model{imeCursor: &cursorAnchor{}}
	m.imeCursor.set(9, 7, true)
	if got := m.syncCursorAnchor("plain frame"); got != "plain frame" {
		t.Fatalf("frame changed to %q", got)
	}
	if _, _, ok := m.imeCursor.get(); ok {
		t.Fatal("inactive frame kept the previous cursor anchor")
	}
}

func TestFinalHelpLayoutPublishesAndRemovesCursorMarker(t *testing.T) {
	m := &Model{
		width:     100,
		height:    28,
		mode:      modeHelp,
		help:      helpState{searching: true, query: "中文"},
		imeCursor: &cursorAnchor{},
	}
	frame := m.View()
	if strings.Contains(frame, cursorAnchorMarker) {
		t.Fatal("private cursor marker leaked from the final frame")
	}
	col, row, ok := m.imeCursor.get()
	if !ok || col < 1 || col > m.width || row < 1 || row > m.height {
		t.Fatalf("final cursor anchor = (%d, %d, %v), frame is %dx%d", col, row, ok, m.width, m.height)
	}
	plain := strings.Split(ansi.Strip(frame), "\n")
	if row > len(plain) || !strings.Contains(plain[row-1], "search 中文▏") {
		t.Fatalf("anchor row %d does not contain the help input: %q", row, plain[row-1])
	}
}

func TestCursorOutputAnchorsOnlyInsideAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	anchor := &cursorAnchor{}
	anchor.set(14, 9, true)
	w := &cursorOutputWriter{out: &out, anchor: anchor}

	if _, err := w.Write([]byte("shell")); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "shell" {
		t.Fatalf("ordinary output changed to %q", got)
	}

	out.Reset()
	if _, err := w.Write([]byte(ansi.SetModeAltScreenSaveCursor + "frame")); err != nil {
		t.Fatal(err)
	}
	if want := ansi.SetModeAltScreenSaveCursor + "frame" + ansi.CursorPosition(14, 9); out.String() != want {
		t.Fatalf("alternate-screen output = %q, want %q", out.String(), want)
	}

	out.Reset()
	if _, err := w.Write([]byte(ansi.ResetModeAltScreenSaveCursor)); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != ansi.ResetModeAltScreenSaveCursor {
		t.Fatalf("alternate-screen exit was anchored: %q", got)
	}
}

func TestCursorOutputTracksSplitAlternateScreenEntry(t *testing.T) {
	sequence := ansi.SetModeAltScreenSaveCursor
	for split := 1; split < len(sequence); split++ {
		t.Run(fmt.Sprintf("split_at_%d", split), func(t *testing.T) {
			var out bytes.Buffer
			anchor := &cursorAnchor{}
			anchor.set(14, 9, true)
			w := &cursorOutputWriter{out: &out, anchor: anchor}

			if _, err := w.Write([]byte(sequence[:split])); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != sequence[:split] {
				t.Fatalf("partial entry output = %q, want %q", got, sequence[:split])
			}
			if _, err := w.Write([]byte(sequence[split:] + "frame")); err != nil {
				t.Fatal(err)
			}
			want := sequence + "frame" + ansi.CursorPosition(14, 9)
			if got := out.String(); got != want {
				t.Fatalf("completed entry output = %q, want %q", got, want)
			}
		})
	}
}

func TestCursorOutputTracksSplitAlternateScreenExit(t *testing.T) {
	sequence := ansi.ResetModeAltScreenSaveCursor
	for split := 1; split < len(sequence); split++ {
		t.Run(fmt.Sprintf("split_at_%d", split), func(t *testing.T) {
			var out bytes.Buffer
			anchor := &cursorAnchor{}
			anchor.set(14, 9, true)
			w := &cursorOutputWriter{out: &out, anchor: anchor, altScreen: true}

			if _, err := w.Write([]byte(sequence[:split])); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != sequence[:split] {
				t.Fatalf("partial exit output = %q, want %q", got, sequence[:split])
			}
			if _, err := w.Write([]byte(sequence[split:] + "shell")); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != sequence+"shell" {
				t.Fatalf("completed exit output = %q, want %q", got, sequence+"shell")
			}
		})
	}
}

func TestCursorOutputAnchorsConsecutiveAlternateScreenWrites(t *testing.T) {
	var out bytes.Buffer
	anchor := &cursorAnchor{}
	anchor.set(14, 9, true)
	w := &cursorOutputWriter{out: &out, anchor: anchor, altScreen: true}

	for _, frame := range []string{"first", "second"} {
		if _, err := w.Write([]byte(frame)); err != nil {
			t.Fatal(err)
		}
	}
	want := "first" + ansi.CursorPosition(14, 9) + "second" + ansi.CursorPosition(14, 9)
	if got := out.String(); got != want {
		t.Fatalf("consecutive output = %q, want %q", got, want)
	}
}

func TestCursorTTYOutputRoutesStringWritesThroughCursorWriter(t *testing.T) {
	var out bytes.Buffer
	anchor := &cursorAnchor{}
	anchor.set(14, 9, true)
	w := &cursorOutputWriter{out: &out, anchor: anchor}
	tty := &cursorTTYOutput{writer: w}

	if _, err := io.WriteString(tty, ansi.SetModeAltScreenSaveCursor); err != nil {
		t.Fatal(err)
	}
	if !w.altScreen {
		t.Fatal("alternate-screen string write bypassed cursor writer")
	}
	if want := ansi.SetModeAltScreenSaveCursor + ansi.CursorPosition(14, 9); out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

func TestCursorOutputStopsWhenAnchorClears(t *testing.T) {
	var out bytes.Buffer
	anchor := &cursorAnchor{}
	w := &cursorOutputWriter{out: &out, anchor: anchor, altScreen: true}
	anchor.set(4, 3, true)
	if _, err := w.Write([]byte("focused")); err != nil {
		t.Fatal(err)
	}
	anchor.set(0, 0, false)
	if _, err := w.Write([]byte("list")); err != nil {
		t.Fatal(err)
	}
	if want := "focused" + ansi.CursorPosition(4, 3) + "list"; out.String() != want {
		t.Fatalf("output = %q, want %q", out.String(), want)
	}
}

type scriptedWriter struct {
	writes  [][]byte
	failOn  int
	shortOn int
	call    int
}

func (w *scriptedWriter) Write(p []byte) (int, error) {
	w.call++
	w.writes = append(w.writes, append([]byte(nil), p...))
	if w.call == w.failOn {
		return 0, io.ErrClosedPipe
	}
	if w.call == w.shortOn {
		return len(p) - 1, io.ErrShortWrite
	}
	return len(p), nil
}

func TestCursorOutputDisablesAnchoringAfterShortWrite(t *testing.T) {
	anchor := &cursorAnchor{}
	anchor.set(4, 3, true)
	out := &scriptedWriter{shortOn: 1}
	w := &cursorOutputWriter{out: out, anchor: anchor, altScreen: true}

	if _, err := w.Write([]byte("frame")); err != io.ErrShortWrite {
		t.Fatalf("short write error = %v, want %v", err, io.ErrShortWrite)
	}
	if w.altScreen || len(w.altScreenPrefix) != 0 {
		t.Fatalf("short write left terminal state active: alt=%v prefix=%q", w.altScreen, w.altScreenPrefix)
	}
}

func TestCursorOutputDisablesAnchoringAfterWriteFailure(t *testing.T) {
	anchor := &cursorAnchor{}
	anchor.set(4, 3, true)
	out := &scriptedWriter{failOn: 1}
	w := &cursorOutputWriter{out: out, anchor: anchor, altScreen: true}

	if _, err := w.Write([]byte("frame")); err != io.ErrClosedPipe {
		t.Fatalf("write error = %v, want %v", err, io.ErrClosedPipe)
	}
	if w.altScreen || len(w.altScreenPrefix) != 0 {
		t.Fatalf("write failure left terminal state active: alt=%v prefix=%q", w.altScreen, w.altScreenPrefix)
	}
}

func TestCursorOutputReturnsCursorPositionWriteFailure(t *testing.T) {
	anchor := &cursorAnchor{}
	anchor.set(4, 3, true)
	out := &scriptedWriter{failOn: 2}
	w := &cursorOutputWriter{out: out, anchor: anchor, altScreen: true}

	if _, err := w.Write([]byte("frame")); err != io.ErrClosedPipe {
		t.Fatalf("cursor position error = %v, want %v", err, io.ErrClosedPipe)
	}
	if len(out.writes) != 2 {
		t.Fatalf("writes = %d, want initial frame and cursor position", len(out.writes))
	}
}
