package ui

import (
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

// The status bar carries git and filesystem errors, whose text quotes a path
// or a ref that can hold control bytes, on both the failure and the outcome
// branch.
func TestStatusMessageEscapesControlBytes(t *testing.T) {
	payload := "boom \x1b]0;P\x07\x1b[2J"
	const want = "boom ^[]0;P^G^[[2J"

	var m Model
	m.errBar.text = payload
	if got := ansi.Strip(m.statusMessage("✖", "✔", "▲")); !strings.Contains(got, want) {
		t.Errorf("failure branch should read as caret notation, got %q", got)
	}
	if stray := strayControl(m.statusMessage("✖", "✔", "▲")); stray != "" {
		t.Errorf("failure branch leaks a control byte near %q", stray)
	}

	m.errBar.done = payload
	if !m.errBar.worked() {
		t.Fatal("matching text and done should read as an outcome")
	}
	if got := ansi.Strip(m.statusMessage("✖", "✔", "▲")); !strings.Contains(got, want) {
		t.Errorf("outcome branch should read as caret notation, got %q", got)
	}
	if stray := strayControl(m.statusMessage("✖", "✔", "▲")); stray != "" {
		t.Errorf("outcome branch leaks a control byte near %q", stray)
	}
}

// A copy that went through with a caveat reads as neither the green
// outcome nor the red failure: it takes the warning glyph and the tone the
// theme gives work in progress.
func TestStatusMessageWarnsInItsOwnTone(t *testing.T) {
	var m Model
	m.reportWarn("copied the whole pane")

	got := m.statusMessage("✖", "✔", "▲")
	if !strings.Contains(ansi.Strip(got), "▲ copied the whole pane") {
		t.Fatalf("warning should take the warning glyph, got %q", ansi.Strip(got))
	}
	if got != uitheme.WarnStyle().Render("▲ copied the whole pane") {
		t.Fatalf("warning should render in the warning style, got %q", got)
	}
	if got == uitheme.DoneStyle().Render("✔ copied the whole pane") || got == uitheme.ErrStyle().Render("✖ copied the whole pane") {
		t.Fatal("warning must not read as an outcome or a failure")
	}
}

// A notice used to take a row from the body, so the whole layout shrank
// whenever one showed. It floats over the frame now: same rows, same body.
func TestStatusToastKeepsLayoutStill(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		for _, height := range []int{24, 34, 50} {
			quiet := shotModel()
			quiet.layout.width, quiet.layout.height = width, height
			quietRows := strings.Split(quiet.viewListFrame(), "\n")
			quietBody := quiet.listBodyHeight()

			noticed := shotModel()
			noticed.layout.width, noticed.layout.height = width, height
			noticed.errBar.text = "worktree kept (has work): /Users/someone/projects/agent-manager"
			noticedRows := strings.Split(noticed.viewListFrame(), "\n")

			if len(noticedRows) != len(quietRows) {
				t.Errorf("%dx%d: notice changed the frame from %d rows to %d",
					width, height, len(quietRows), len(noticedRows))
			}
			if got := noticed.listBodyHeight(); got != quietBody {
				t.Errorf("%dx%d: notice changed the body from %d rows to %d", width, height, quietBody, got)
			}
			for i, line := range noticedRows {
				if got := ansi.StringWidth(line); got != ansi.StringWidth(quietRows[i]) {
					t.Fatalf("%dx%d: row %d is %d wide with a notice, %d without",
						width, height, i, got, ansi.StringWidth(quietRows[i]))
				}
			}
			if !strings.Contains(ansi.Strip(noticed.viewListFrame()), "worktree kept") {
				t.Errorf("%dx%d: the notice is not on the frame", width, height)
			}
		}
	}
}

// The card sits at the top right, under the header, not over the rail.
func TestStatusToastSitsTopRight(t *testing.T) {
	m := shotModel()
	m.layout.width, m.layout.height = 120, 34
	m.errBar.text = "already up to date"
	rows := strings.Split(ansi.Strip(m.viewListFrame()), "\n")
	row := m.listChromeRows() + 2
	if row >= len(rows) {
		t.Fatalf("frame is only %d rows", len(rows))
	}
	if !strings.Contains(rows[row], "already up to date") {
		t.Fatalf("notice is not on row %d: %q", row, rows[row])
	}
	leftWidth, _ := m.splitWidths()
	if column := strings.Index(rows[row], "already up to date"); column <= leftWidth {
		t.Fatalf("notice starts at column %d, inside the rail (%d wide)", column, leftWidth)
	}
}

// An editor that opened is an outcome, not a failure, all the way from the
// key to the card the toast draws.
func TestOpenedEditorReadsAsAnOutcome(t *testing.T) {
	m := buildModel(t)
	launched := captureEditor(t, "code")
	dir := t.TempDir()
	createSession(t, m, "agent", dir, "")
	m.selectSessionRow(t, "agent")

	_, cmd := m.openEditor()
	applyEditorLookup(t, m, cmd)
	if len(*launched) == 0 {
		t.Fatalf("the editor never launched, status = %q", m.errBar.text)
	}
	if want := uitheme.DoneStyle().Render("● " + m.errBar.text); m.statusLine() != want {
		t.Fatalf("status line is %q, want the outcome styling %q", m.statusLine(), want)
	}
}

// The same for an update check that found the current version installed:
// the check ran, so the toast reports it rather than crossing it out.
func TestUpToDateReadsAsAnOutcome(t *testing.T) {
	m := shotModel()
	updated, _ := m.Update(uirelease.AppliedMsg{UpToDate: true})
	m = updated.(*Model)
	if m.errBar.text != "already up to date" {
		t.Fatalf("status text is %q", m.errBar.text)
	}
	if want := uitheme.DoneStyle().Render("● already up to date"); m.statusLine() != want {
		t.Fatalf("status line is %q, want %q", m.statusLine(), want)
	}
}

// Only the message that says it worked gets the outcome styling: a failure
// written straight to the bar afterwards reads as one, whatever the bar
// carried before.
func TestFailureAfterAnOutcomeReadsAsAFailure(t *testing.T) {
	m := shotModel()
	m.reportDone("opened /tmp/project in code")
	m.errBar.text = "git not found in PATH"
	if want := uitheme.ErrStyle().Render("✕ git not found in PATH"); m.statusLine() != want {
		t.Fatalf("status line is %q, want %q", m.statusLine(), want)
	}

	// Dismissal takes the outcome with it, so the next message starts clean
	// even when it repeats the words of the last one that worked.
	m.reportDone("opened /tmp/project in code")
	for range 3 {
		m.ageError()
	}
	m.errBar.text = "opened /tmp/project in code"
	if want := uitheme.ErrStyle().Render("✕ opened /tmp/project in code"); m.statusLine() != want {
		t.Fatalf("status line is %q, want %q", m.statusLine(), want)
	}
}

// The review footer marks the same two states in its own glyphs.
func TestReviewFooterMarksAnOutcome(t *testing.T) {
	m := shotModel()
	m.layout.width = 80
	m.reportDone("opened /tmp/project in code")
	if got := ansi.Strip(m.viewDiffStatus()); !strings.HasPrefix(got, " ✔ ") {
		t.Fatalf("review footer reads %q, want the outcome glyph", got)
	}
	m.errBar.text = "git not found in PATH"
	if got := ansi.Strip(m.viewDiffStatus()); !strings.HasPrefix(got, " ✖ ") {
		t.Fatalf("review footer reads %q, want the failure glyph", got)
	}
}

// Search is a field over the list it filters, in the rail, not a card that
// floats away from the entries.
func TestSearchFieldSitsInTheRail(t *testing.T) {
	m := shotModel()
	m.layout.width, m.layout.height = 120, 34
	m.rail.SetSearch(m.rail.Search(), true)
	m.rail.SetSearch("rate", m.rail.Searching())

	if status := ansi.Strip(m.statusLine()); strings.Contains(status, "rate") {
		t.Fatalf("the query moved to the rail, the notice should not repeat it: %q", status)
	}
	leftWidth, _ := m.splitWidths()
	rows := strings.Split(ansi.Strip(m.viewListFrame()), "\n")
	found := false
	for _, row := range rows {
		column := strings.Index(row, "⌕ rate")
		if column < 0 {
			continue
		}
		found = true
		if column > leftWidth {
			t.Fatalf("search field starts at column %d, past the rail (%d wide)", column, leftWidth)
		}
	}
	if !found {
		t.Fatalf("no search field on the frame:\n%s", strings.Join(rows, "\n"))
	}
}
