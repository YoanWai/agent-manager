package statusbar

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func testTones() Tones { return TonesFor(theme.Palettes[0]) }

func testGlyphs() presentation.GlyphSet { return presentation.GlyphsFor("") }

var testMarks = Marks{Fail: "✖", Done: "✔", Warn: "▲"}

// The bar carries git and filesystem errors, whose text quotes a path or a
// ref that can hold control bytes, on both the failure and the outcome
// branch.
func TestPaintEscapesControlBytes(t *testing.T) {
	payload := "boom \x1b]0;P\x07\x1b[2J"
	const want = "boom ^[]0;P^G^[[2J"

	for _, kind := range []Kind{Failure, Success, Warning} {
		got := ansi.Strip(kind.Paint(payload, testMarks, testTones()))
		if !strings.Contains(got, want) {
			t.Errorf("kind %d should read as caret notation, got %q", kind, got)
		}
	}
}

// An action that went through with a caveat reads as neither the green
// outcome nor the red failure: it takes the warning glyph and the tone the
// theme gives work in progress.
func TestPaintWarnsInItsOwnTone(t *testing.T) {
	tones := testTones()
	got := Warning.Paint("copied the whole pane", testMarks, tones)
	if got != tones.Warn.Render("▲ copied the whole pane") {
		t.Fatalf("warning should render in the warning style, got %q", ansi.Strip(got))
	}
	if got == tones.Done.Render("✔ copied the whole pane") || got == tones.Fail.Render("✖ copied the whole pane") {
		t.Fatal("warning must not read as an outcome or a failure")
	}
}

// An error outranks a scrolled pane: the reader must not lose a failure
// report to a view notice.
func TestLineOutranksFocusNotices(t *testing.T) {
	tones := testTones()
	got := Line(Message{Report: "git not found in PATH", Focused: true, ScrolledBack: true, ScrollOffset: 12}, testGlyphs(), tones)
	if !strings.Contains(ansi.Strip(got), "git not found in PATH") {
		t.Fatalf("error was hidden behind the scroll notice: %q", ansi.Strip(got))
	}
	if strings.Contains(ansi.Strip(got), "lines back") {
		t.Fatalf("the scroll notice should not sit beside the error: %q", ansi.Strip(got))
	}
}

// The scroll notice names the catch-up keys the session actually has: a
// session whose scrollback tmux holds also answers a page key.
func TestLineNamesTheCatchUpKeys(t *testing.T) {
	tones := testTones()
	plain := ansi.Strip(Line(Message{ScrolledBack: true, ScrollOffset: 12}, testGlyphs(), tones))
	if !strings.Contains(plain, "12 lines back · wheel down or type to catch up") {
		t.Fatalf("scroll notice = %q", plain)
	}
	paged := ansi.Strip(Line(Message{ScrolledBack: true, ScrollOffset: 12, PagesScrollback: true}, testGlyphs(), tones))
	if !strings.Contains(paged, "wheel down, pgdn or type to catch up") {
		t.Fatalf("scroll notice = %q", paged)
	}
}

// The three states a message can be in read in three tones, and a failure is
// what a message reads as by default.
func TestLinePaintsWhatItSays(t *testing.T) {
	tones := testTones()
	cases := []struct {
		msg  Message
		want string
	}{
		{Message{Report: "opened /tmp in code", Kind: Success}, tones.Done.Render(testGlyphs().FinishedGlyph + " opened /tmp in code")},
		{Message{Report: "copied the pane", Kind: Warning}, tones.Warn.Render(testGlyphs().WarningGlyph + " copied the pane")},
		{Message{Report: "git not found in PATH"}, tones.Fail.Render(testGlyphs().ErroredGlyph + " git not found in PATH")},
	}
	for _, tc := range cases {
		if got := Line(tc.msg, testGlyphs(), tones); got != tc.want {
			t.Errorf("line for %q = %q, want %q", tc.msg.Report, got, tc.want)
		}
	}
}

// A card splices into the frame's rows without moving the cells around it.
func TestSpliceKeepsWidth(t *testing.T) {
	row := strings.Repeat("session running ", 3)
	spliced := Splice(row, strings.Repeat("boom  ", 1), 20)
	if got := ansi.StringWidth(spliced); got != ansi.StringWidth(row) {
		t.Fatalf("spliced row is %d wide, want %d: %q", got, ansi.StringWidth(row), spliced)
	}
	if plain := ansi.Strip(spliced); !strings.Contains(plain, "boom") || !strings.HasPrefix(plain, "session running ") {
		t.Fatalf("splice lost content: %q", plain)
	}
}

// A card shrinks to its message and gives up rather than painting a sliver
// when the column has no room for one.
func TestToastFitsItsColumnAndGivesUpWhenItCannot(t *testing.T) {
	tones := testTones()
	rows := Toast("already up to date", 200, tones)
	if len(rows) == 0 {
		t.Fatal("a wide column should paint the card")
	}
	width := ansi.StringWidth(rows[0])
	if width != ansi.StringWidth("already up to date")+chromeX {
		t.Fatalf("card is %d wide, want it shrunk to its message", width)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got != width {
			t.Fatalf("card row is %d wide, want %d", got, width)
		}
	}
	if got := Toast("already up to date", MinWidth+Margin-1, tones); len(got) != 0 {
		t.Fatalf("a column too narrow for a card painted %d rows", len(got))
	}
	if got := Toast("", 200, tones); got != nil {
		t.Fatalf("nothing to say painted %d rows", len(got))
	}
}

// A long message wraps inside the card's widest width instead of running
// past the frame's right edge.
func TestToastWrapsAtItsWidest(t *testing.T) {
	rows := Toast(strings.Repeat("worktree kept (has work) ", 4), 200, testTones())
	if len(rows) < 3 {
		t.Fatalf("a long message stayed on one row: %q", rows)
	}
	for _, row := range rows {
		if got := ansi.StringWidth(row); got != MaxWidth {
			t.Fatalf("card row is %d wide, want the %d-column cap", got, MaxWidth)
		}
	}
}

// Only a run that matched when it was written keeps reading as what it was:
// text written on its own is a failure, and the report that follows must not
// inherit an earlier outcome or warning.
func TestOutcomeKindFollowsTheRunThatNamedIt(t *testing.T) {
	if got := Failed("git not found in PATH").KindOf(); got != Failure {
		t.Fatalf("lone text reads as %d, want a failure", got)
	}
	if got := Succeeded("opened /tmp/a in code").KindOf(); got != Success {
		t.Fatalf("an action that went through reads as %d", got)
	}
	if got := Warned("copied the whole pane").KindOf(); got != Warning {
		t.Fatalf("a caveat reads as %d", got)
	}
	if got := Succeeded("opened /tmp/a in code").Done(); got != "opened /tmp/a in code" {
		t.Fatalf("Succeeded kept %q as its outcome run", got)
	}
	if got := Warned("copied").Warn(); got != "copied" {
		t.Fatalf("Warned kept %q as its warning run", got)
	}
	// A message written over an outcome, holding the same fields a host keeps.
	if got := Told("something else", "opened", "", "opened", 0).KindOf(); got != Failure {
		t.Fatalf("text replaced after an outcome reads as %d, want a failure", got)
	}
}

// A message's first poll records it, and it clears two polls later, so a
// transient error needs no timer of its own.
func TestOutcomeClearsAfterTwoPolls(t *testing.T) {
	if got := (Outcome{}).Ticked(); got.Text() != "" {
		t.Fatalf("an empty bar grew %q", got.Text())
	}
	once := Failed("boom").Ticked()
	if once.Text() != "boom" || once.Shown() != "boom" {
		t.Fatalf("the first poll should only record the baseline: %+v", once)
	}
	if ticked := Failed("").Ticked(); ticked.Text() != "" {
		t.Fatalf("a cleared bar came back with %q", ticked.Text())
	}
	twice := once.Ticked()
	if twice.Text() != "boom" || twice.Age() != 1 {
		t.Fatalf("the second poll should age the bar, got %q at %d", twice.Text(), twice.Age())
	}
	if thrice := twice.Ticked(); thrice.Text() != "" {
		t.Fatalf("the third poll should clear the bar, kept %q", thrice.Text())
	}
	if fresh := once.Ticked().Ticked().Ticked(); fresh.Text() != "" {
		t.Fatalf("a cleared bar came back with %q", fresh.Text())
	}
	changed := Failed("newer").Ticked()
	if changed.Text() != "newer" || changed.Age() != 0 {
		t.Fatalf("new text should restart the count: %+v", changed)
	}
}
