package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func runsText(line []textRun) string {
	var text strings.Builder
	for _, run := range line {
		text.WriteString(run.text)
	}
	return text.String()
}

func accented(lines [][]textRun) []string {
	var found []string
	for _, line := range lines {
		for _, run := range line {
			if run.accent {
				found = append(found, run.text)
			}
		}
	}
	return found
}

func TestMarkedRunsAccentTheBacktickSpans(t *testing.T) {
	got := markedRuns("press `ctrl+x` or `ctrl+l` now")
	want := []textRun{{text: "press "}, {text: "ctrl+x", accent: true}, {text: " or "}, {text: "ctrl+l", accent: true}, {text: " now"}}
	if !slices.Equal(got, want) {
		t.Fatalf("markedRuns() = %+v, want %+v", got, want)
	}
	if runs := markedRuns("no marks here"); len(runs) != 1 || runs[0].accent {
		t.Fatalf("plain text is one plain run: %+v", runs)
	}
}

func TestPhraseRunsAccentEveryOccurrence(t *testing.T) {
	got := phraseRuns("ctrl+l opens it and ctrl+l closes it", []string{"ctrl+l"})
	want := []textRun{{text: "ctrl+l", accent: true}, {text: " opens it and "}, {text: "ctrl+l", accent: true}, {text: " closes it"}}
	if !slices.Equal(got, want) {
		t.Fatalf("phraseRuns() = %+v, want %+v", got, want)
	}
}

func TestPhraseRunsPreferTheLongerPhraseAtOnePlace(t *testing.T) {
	got := phraseRuns("press ctrl+l then ctrl", []string{"ctrl", "ctrl+l"})
	if text := runsText(got); text != "press ctrl+l then ctrl" {
		t.Fatalf("text must come through once and whole: %q", text)
	}
	if found := accented([][]textRun{got}); !slices.Equal(found, []string{"ctrl+l", "ctrl"}) {
		t.Fatalf("accented %q, want the longer phrase where both start", found)
	}
}

func TestPhraseRunsWithoutPhrasesIsOnePlainRun(t *testing.T) {
	if got := phraseRuns("plain line", nil); !slices.Equal(got, []textRun{{text: "plain line"}}) {
		t.Fatalf("phraseRuns() = %+v", got)
	}
	if got := phraseRuns("", nil); len(got) != 0 {
		t.Fatalf("an empty line has no runs: %+v", got)
	}
}

func TestWrapRunsKeepsEveryRowInsideTheWidth(t *testing.T) {
	runs := markedRuns("Pick the `model and reasoning effort` for every session, in the New Session modal and in quick prompt mode.")
	for _, width := range []int{20, 34, 60} {
		lines := wrapRuns(runs, width)
		var words []string
		for _, line := range lines {
			text := runsText(line)
			if got := ansi.StringWidth(text); got > width {
				t.Fatalf("width %d: row is %d cells: %q", width, got, text)
			}
			if text != strings.TrimSpace(text) {
				t.Fatalf("width %d: a wrapped row must not start or end with a space: %q", width, text)
			}
			words = append(words, strings.Fields(text)...)
		}
		if got, want := strings.Join(words, " "), "Pick the model and reasoning effort for every session, in the New Session modal and in quick prompt mode."; got != want {
			t.Fatalf("width %d: words changed: %q", width, got)
		}
	}
}

func TestWrapRunsAccentsBothHalvesOfAPhraseThatWraps(t *testing.T) {
	lines := wrapRuns(markedRuns("x `model and reasoning effort` y"), 12)
	if len(lines) < 2 {
		t.Fatalf("want the phrase to wrap, got %d row", len(lines))
	}
	joined := strings.Join(accented(lines), " ")
	if joined != "model and reasoning effort" {
		t.Fatalf("accented words = %q, want the whole phrase across rows", joined)
	}
}

func TestWrapRunsKeepsSpacingInsideARow(t *testing.T) {
	row := "n      new session           space  quick prompt mode"
	lines := wrapRuns([]textRun{{text: row}}, 80)
	if len(lines) != 1 || runsText(lines[0]) != row {
		t.Fatalf("columns inside a row must survive: %q", runsText(lines[0]))
	}
	indented := "       right column only"
	if got := runsText(wrapRuns([]textRun{{text: indented}}, 80)[0]); got != indented {
		t.Fatalf("leading spaces must survive: %q", got)
	}
}

func TestWrapRunsBreaksAfterAHyphen(t *testing.T) {
	lines := wrapRuns([]textRun{{text: "github.com/YoanWai/agent-manager"}}, 28)
	if len(lines) != 2 || runsText(lines[0]) != "github.com/YoanWai/agent-" || runsText(lines[1]) != "manager" {
		t.Fatalf("rows = %+v", lines)
	}
}

func TestWrapRunsOfNothingIsNoRows(t *testing.T) {
	if lines := wrapRuns(nil, 40); len(lines) != 0 {
		t.Fatalf("got %d rows", len(lines))
	}
}

func TestRenderRunsStylesTheAccent(t *testing.T) {
	useTrueColor(t)
	line := []textRun{{text: "press "}, {text: "ctrl+x", accent: true}}
	rendered := renderRuns(line, valueStyle)
	if !strings.Contains(rendered, keyStyle.Render("ctrl+x")) {
		t.Fatalf("accent run is not in the accent style: %q", rendered)
	}
	if !strings.Contains(rendered, valueStyle.Render("press ")) {
		t.Fatalf("plain run is not in the base style: %q", rendered)
	}
	if ansi.Strip(rendered) != "press ctrl+x" {
		t.Fatalf("text changed: %q", ansi.Strip(rendered))
	}
}

func TestWrapRunsBreaksOnlyAtSpacesAndInsideHyphenatedWords(t *testing.T) {
	text := "Press `ctrl+x`, then (`space`) to pick the `--model` flag with `agent-manager` now."
	plain := strings.ReplaceAll(text, "`", "")
	for width := 8; width <= 60; width++ {
		lines := wrapRuns(markedRuns(text), width)
		var joined strings.Builder
		for index, line := range lines {
			row := runsText(line)
			if strings.HasPrefix(row, ",") || strings.HasPrefix(row, ")") || strings.HasSuffix(row, "(") || strings.HasSuffix(row, "--") || row == "--" {
				t.Fatalf("width %d: row %d %q breaks where no space or inner hyphen is", width, index, row)
			}
			if index > 0 && !strings.HasSuffix(joined.String(), "-") {
				joined.WriteString(" ")
			}
			joined.WriteString(row)
		}
		if joined.String() != plain {
			t.Fatalf("width %d: rows re-join to %q, want %q", width, joined.String(), plain)
		}
		if got := strings.Join(accented(lines), ""); got != "ctrl+xspace--modelagent-manager" {
			t.Fatalf("width %d: accented %q, want every marked word", width, got)
		}
	}
}

func TestWrapRunsBreaksAfterTheInnerHyphenOfAFlag(t *testing.T) {
	lines := wrapRuns([]textRun{{text: "aaaa --no-leader"}}, 9)
	var rows []string
	for _, line := range lines {
		rows = append(rows, runsText(line))
	}
	if !slices.Equal(rows, []string{"aaaa", "--no-", "leader"}) {
		t.Fatalf("rows = %q, want the flag split only after no-", rows)
	}
}

func TestWrapRunsAccentsTheSpaceBetweenAccentedWords(t *testing.T) {
	lines := wrapRuns(markedRuns("x `model effort` y"), 80)
	if len(lines) != 1 || !slices.Contains(lines[0], textRun{text: "model effort", accent: true}) {
		t.Fatalf("rows = %+v, want one accented run with its inner space", lines)
	}
}
