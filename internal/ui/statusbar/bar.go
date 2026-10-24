// Package statusbar paints the transient message the manager floats over the
// frame: what a key, a save or a failed action has to say, and the card that
// carries it, so a notice never costs the body a row.
package statusbar

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The card's measurements: the widest it gets before it wraps, the
// narrowest that still reads as a card, the gap it keeps from the frame's
// right edge, and the rows its chrome costs.
const (
	MaxWidth = 52
	MinWidth = 20
	Margin   = 2
	chromeX  = 4
)

type Tones struct {
	Key    lipgloss.Style
	Subtle lipgloss.Style

	Fail lipgloss.Style
	Done lipgloss.Style
	Warn lipgloss.Style

	Border lipgloss.Style
	Fill   string
}

func TonesFor(t theme.Theme) Tones {
	return Tones{
		Key:    lipgloss.NewStyle().Foreground(lipgloss.Color(t.Accent)).Bold(true),
		Subtle: lipgloss.NewStyle().Foreground(lipgloss.Color(t.Subtle)),
		Fail:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Errored)).Bold(true),
		Done:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Finished)),
		Warn:   lipgloss.NewStyle().Foreground(lipgloss.Color(t.Working)).Bold(true),
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Mix(t.Border, t.Accent, 0.35))),
		Fill:   theme.Mix(t.Bg, t.Surface, 0.35),
	}
}

// Outcome is what the manager last put on the bar: the text, the run of text
// that named it an outcome or a warning, and the poll baseline that ages it
// out. Only Succeeded and Warned set those runs; any later text alone leaves
// them behind, so a message says it worked or reads as a failure.
type Outcome struct {
	text, done, warn, shown string
	age                     int
}

// Failed is an outcome whose text reads as a failure until it says it worked.
func Failed(text string) Outcome { return Outcome{text: text} }

func Succeeded(text string) Outcome { return Outcome{text: text, done: text} }

func Warned(text string) Outcome { return Outcome{text: text, warn: text} }

// Told is an outcome held as four strings and a poll count, for a host that
// keeps those fields itself so the features that read them keep working.
func Told(text, done, warn, shown string, age int) Outcome {
	return Outcome{text: text, done: done, warn: warn, shown: shown, age: age}
}

func (o Outcome) Text() string { return o.text }

// Done, Warn and Shown are the stored runs and the baseline the next poll
// compares against.
func (o Outcome) Done() string  { return o.done }
func (o Outcome) Warn() string  { return o.warn }
func (o Outcome) Shown() string { return o.shown }

func (o Outcome) Age() int { return o.age }

// KindOf is how the text reads, which is the tone the bar paints it in.
func (o Outcome) KindOf() Kind {
	switch {
	case o.text != "" && o.text == o.done:
		return Success
	case o.text != "" && o.text == o.warn:
		return Warning
	}
	return Failure
}

// Ticked counts one poll against the outcome. Text that just changed starts
// over, and text that survived two ticks clears, so a transient message
// self-dismisses without a timer per callsite.
func (o Outcome) Ticked() Outcome {
	if o.text == "" {
		return Outcome{}
	}
	if o.text != o.shown {
		o.shown, o.age = o.text, 0
		return o
	}
	o.age++
	if o.age >= 2 {
		return Outcome{}
	}
	return o
}

type Marks struct {
	Fail string
	Done string
	Warn string
}

// Kind is how a message reads: an action that failed, one that went
// through, or one that went through with a caveat.
type Kind uint8

const (
	// Failure is what a message reads as unless it says it worked: a path or
	// a ref quoting a symptom is not an outcome.
	Failure Kind = iota
	Success
	Warning
)

// Paint styles a message in the tone its kind earns, tagged with that kind's
// glyph. The text is escaped first: it carries paths and refs that can hold
// control bytes.
func (k Kind) Paint(text string, marks Marks, tones Tones) string {
	text = presentation.EscapeControlsInline(text)
	switch k {
	case Success:
		return tones.Done.Render(marks.Done + " " + text)
	case Warning:
		return tones.Warn.Render(marks.Warn + " " + text)
	}
	return tones.Fail.Render(marks.Fail + " " + text)
}

// Message is what the bar has to say, reduced to the facts the paint reads.
// Report is the status bar's own text; the rest are the conditions that
// outrank it, most urgent first.
type Message struct {
	Report string
	Kind   Kind

	Focused         bool
	ScrolledBack    bool
	ScrollOffset    int
	PagesScrollback bool
	Copied          int

	ResizeMode bool
	Dragging   bool

	Notice string
}

// Line is the bar's one line: prompts, search, and self-dismissing errors
// and outcomes. It returns "" when there is nothing to say.
func Line(msg Message, glyphs presentation.GlyphSet, tones Tones) string {
	marks := Marks{Fail: glyphs.ErroredGlyph, Done: glyphs.FinishedGlyph, Warn: glyphs.WarningGlyph}
	switch {
	// Errors outrank the focus notices: a scrolled or focused pane must not
	// hide a failure report.
	case msg.Focused && msg.Report != "":
		return msg.Kind.Paint(msg.Report, marks, tones)
	case msg.ScrolledBack:
		catchUp := "wheel down or type"
		if msg.PagesScrollback {
			catchUp = "wheel down, pgdn or type"
		}
		return tones.Key.Render("scrolled ") +
			tones.Subtle.Render(fmt.Sprintf("%d lines back · %s to catch up", msg.ScrollOffset, catchUp))
	case msg.Focused && msg.Copied > 0:
		return tones.Key.Render("copied ") +
			tones.Subtle.Render(fmt.Sprintf("%d chars to clipboard", msg.Copied))
	case msg.ResizeMode || msg.Dragging:
		hint := "←→ resize · drag divider · enter set · esc cancel"
		if msg.Dragging {
			hint = "release to set · esc cancels"
		}
		return tones.Key.Render("resize ") + tones.Subtle.Render(hint)
	case msg.Report != "":
		return msg.Kind.Paint(msg.Report, marks, tones)
	case msg.Notice != "":
		return tones.Done.Render(glyphs.FinishedGlyph + " " + presentation.EscapeControlsInline(msg.Notice))
	default:
		return ""
	}
}

// Toast is the card for a message in a content column room columns wide:
// nil when there is nothing to say, or when the column is too narrow to hold
// a card.
func Toast(text string, room int, tones Tones) []string {
	if text == "" {
		return nil
	}
	width := ansi.StringWidth(text) + chromeX
	for _, limit := range []int{MaxWidth, room - Margin} {
		if width > limit {
			width = limit
		}
	}
	if width < MinWidth {
		return nil
	}
	inner := width - chromeX
	edge := tones.Border.Render("│")
	rows := []string{paint(tones.Border.Render("╭"+strings.Repeat("─", width-2)+"╮"), width, tones.Fill)}
	for _, line := range strings.Split(ansi.Wordwrap(text, inner, ""), "\n") {
		rows = append(rows, paint(edge+" "+presentation.PadRight(line, inner)+" "+edge, width, tones.Fill))
	}
	return append(rows, paint(tones.Border.Render("╰"+strings.Repeat("─", width-2)+"╯"), width, tones.Fill))
}

// Splice overpaints a run of cells inside a styled row, keeping the escape
// state on both sides of the patch intact.
func Splice(row, patch string, left int) string {
	head := ansi.Truncate(row, left, "")
	if pad := left - ansi.StringWidth(head); pad > 0 {
		head += strings.Repeat(" ", pad)
	}
	tail := ansi.TruncateLeft(row, left+ansi.StringWidth(patch), "")
	return head + patch + tail
}

// paint pads a possibly-styled line to an exact width and fills every cell
// with bg. Inner SGR resets emitted by per-segment renders would drop the
// fill partway across, so each reset re-applies it.
func paint(s string, width int, bg string) string {
	fill := "\x1b[" + lipgloss.ColorProfile().Color(bg).Sequence(true) + "m"
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+fill)
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	return fill + s + "\x1b[0m"
}
