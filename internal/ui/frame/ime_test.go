package frame

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/x/ansi"
)

func inputField(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	return in
}

func areaField() textarea.Model {
	in := textarea.New()
	in.CharLimit = 2000
	in.ShowLineNumbers = false
	in.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "> "
		}
		return "  "
	})
	return in
}

func TestTextInputCursorMarkerSurvivesBothBlinkPhases(t *testing.T) {
	input := inputField("", 60)
	input.Prompt = "» "
	input.SetValue("甲x")
	input.SetCursor(1)
	input.Focus()

	var position [2]int
	for _, blink := range []bool{false, true} {
		input.Cursor.Blink = blink
		view := TextInputView(input)
		if !strings.Contains(view, CursorMarker) {
			t.Fatalf("blink=%v: input view has no cursor marker", blink)
		}
		clean, col, row, ok := CursorMarkerPosition(view)
		if !ok {
			t.Fatalf("blink=%v: cursor marker was not found", blink)
		}
		if strings.Contains(clean, CursorMarker) {
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
	input := areaField()
	input.Placeholder = ""
	input.SetWidth(6)
	input.SetHeight(2)
	input.SetValue("甲乙ab")
	input.SetCursor(3)
	input.Focus()

	var position [2]int
	for _, blink := range []bool{false, true} {
		input.Cursor.Blink = blink
		view := TextAreaView(input)
		_, col, row, ok := CursorMarkerPosition(view)
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
	frame := "first\n\x1b[31m甲乙\x1b[0m" + CursorMarker + "x"
	clean, col, row, ok := CursorMarkerPosition(frame)
	if !ok || col != 5 || row != 2 {
		t.Fatalf("cursor = (%d, %d, %v), want (5, 2, true)", col, row, ok)
	}
	if strings.Contains(clean, CursorMarker) || ansi.Strip(clean) != "first\n甲乙x" {
		t.Fatalf("clean frame = %q", clean)
	}
}

// enterAltScreen drives a writer through a real alternate-screen entry, which
// is the only way it starts anchoring.
func enterAltScreen(t *testing.T, out *bytes.Buffer, w *CursorOutputWriter) {
	t.Helper()
	if _, err := w.Write([]byte(ansi.SetModeAltScreenSaveCursor)); err != nil {
		t.Fatal(err)
	}
	out.Reset()
}

func TestCursorOutputAnchorsOnlyInsideAlternateScreen(t *testing.T) {
	var out bytes.Buffer
	anchor := &CursorAnchor{}
	anchor.Set(14, 9, true)
	w := NewCursorOutputWriter(&out, anchor)

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
			anchor := &CursorAnchor{}
			anchor.Set(14, 9, true)
			w := NewCursorOutputWriter(&out, anchor)

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
			anchor := &CursorAnchor{}
			anchor.Set(14, 9, true)
			w := NewCursorOutputWriter(&out, anchor)
			enterAltScreen(t, &out, w)

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
	anchor := &CursorAnchor{}
	anchor.Set(14, 9, true)
	w := NewCursorOutputWriter(&out, anchor)
	enterAltScreen(t, &out, w)

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
	anchor := &CursorAnchor{}
	anchor.Set(14, 9, true)
	w := NewCursorOutputWriter(&out, anchor)
	tty := &CursorTTYOutput{writer: w}

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
	anchor := &CursorAnchor{}
	w := NewCursorOutputWriter(&out, anchor)
	enterAltScreen(t, &out, w)
	anchor.Set(4, 3, true)
	if _, err := w.Write([]byte("focused")); err != nil {
		t.Fatal(err)
	}
	anchor.Set(0, 0, false)
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
	anchor := &CursorAnchor{}
	anchor.Set(4, 3, true)
	out := &scriptedWriter{shortOn: 1}
	w := NewCursorOutputWriter(out, anchor)
	w.altScreen = true

	if _, err := w.Write([]byte("frame")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write error = %v, want %v", err, io.ErrShortWrite)
	}
	if w.altScreen || len(w.altScreenPrefix) != 0 {
		t.Fatalf("short write left terminal state active: alt=%v prefix=%q", w.altScreen, w.altScreenPrefix)
	}
}

func TestCursorOutputDisablesAnchoringAfterWriteFailure(t *testing.T) {
	anchor := &CursorAnchor{}
	anchor.Set(4, 3, true)
	out := &scriptedWriter{failOn: 1}
	w := NewCursorOutputWriter(out, anchor)
	w.altScreen = true

	if _, err := w.Write([]byte("frame")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error = %v, want %v", err, io.ErrClosedPipe)
	}
	if w.altScreen || len(w.altScreenPrefix) != 0 {
		t.Fatalf("write failure left terminal state active: alt=%v prefix=%q", w.altScreen, w.altScreenPrefix)
	}
}

func TestCursorOutputReturnsCursorPositionWriteFailure(t *testing.T) {
	anchor := &CursorAnchor{}
	anchor.Set(4, 3, true)
	out := &scriptedWriter{failOn: 2}
	w := NewCursorOutputWriter(out, anchor)
	w.altScreen = true

	if _, err := w.Write([]byte("frame")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("cursor position error = %v, want %v", err, io.ErrClosedPipe)
	}
	if len(out.writes) != 2 {
		t.Fatalf("writes = %d, want initial frame and cursor position", len(out.writes))
	}
	if w.altScreen || len(w.altScreenPrefix) != 0 {
		t.Fatalf("cursor position failure left terminal state active: alt=%v prefix=%q", w.altScreen, w.altScreenPrefix)
	}
}
