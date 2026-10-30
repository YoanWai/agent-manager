package status

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

// tmux reports the cursor in display cells, so a wide rune ahead of it
// must not shift the drawn cursor off its cell.
func TestRuneAtColumn(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		column int
		index  int
	}{
		{"ascii", "abc", 1, 1},
		{"after wide rune", "世界x", 4, 2},
		{"inside wide rune", "世界x", 1, 0},
		{"past end", "ab", 4, 2},
		{"empty line", "", 0, 0},
	}
	for _, c := range cases {
		if index := RuneAtColumn([]rune(c.line), c.column); index != c.index {
			t.Errorf("%s: RuneAtColumn(%q,%d) = %d, want %d",
				c.name, c.line, c.column, index, c.index)
		}
	}
}

func TestHasTextBeforeCaretProtectsDraftAndIgnoresPlaceholder(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{"agent": {InputPrefix: "^❯\\s"}}})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, row string
		caret     int
		want      bool
	}{
		{"empty prompt", "❯ ", 2, false},
		{"nonbreaking space", "❯ ", 2, false},
		{"draft before caret", "❯ q", 3, true},
		{"wide draft", "❯ 界", 4, true},
		{"placeholder after caret", "❯ Ask anything", 2, false},
		{"trimmed blanks", "❯ ", 5, false},
		{"output row", "final answer", 5, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := HasTextBeforeCaret(engine, "agent", test.row, test.caret); got != test.want {
				t.Fatalf("draft before caret = %v, want %v", got, test.want)
			}
		})
	}
}

// omp draws its ╰─ marker on the first composer row only, so a draft that
// wraps leaves the caret on a markerless row; delivery must still hold.
func TestDraftBeforeCaretReadsAWrappedOmpDraft(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	wrapped := []string{
		" ⬢ model",
		"╰─ this is a long draft typed while the turn runs, long enough to wrap",
		"   the composer below the gutter",
	}
	if !HasDraftBeforeCaret(engine, "omp", wrapped, 33, 2) {
		t.Fatal("a draft wrapped onto a second composer row was read as empty")
	}
	if !HasDraftBeforeCaret(engine, "omp", []string{" ⬢ model", "╰─ short draft"}, 14, 1) {
		t.Fatal("a one-row draft was read as empty")
	}
	if HasDraftBeforeCaret(engine, "omp", []string{" ⬢ model", "╰─ "}, 3, 1) {
		t.Fatal("an empty composer was read as a draft")
	}
	if HasDraftBeforeCaret(engine, "omp", []string{"", "   some output"}, 14, 1) {
		t.Fatal("a row with no marker above it was read as a draft")
	}
}

// Rows captured from each CLI: a line break leaves the caret on a blank composer row, a parked caret holds no draft.
func TestDraftBeforeCaretReadsADraftThatEndsInALineBreak(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rule := strings.Repeat("─", 80)
	band, lowerBand := strings.Repeat("▄", 80), strings.Repeat("▀", 80)
	voice := "── Voice input (alt(option)+v to start) " + strings.Repeat("─", 40)
	cases := []struct {
		name, tool     string
		rows           []string
		caretX, caretY int
		want           bool
	}{
		{"gemini wrapped draft", "gemini", []string{
			" Shift+Tab to accept edits                                         1 MCP server",
			band,
			" > Run this exact shell command once, replacing <name> with a short 2-4 word",
			"   kebab-case name for the broad feature or theme of this whole session (not",
			"   name from context, not a narrow step. Then continue.",
			"",
			lowerBand,
		}, 3, 5, true},
		{"gemini draft opening on a blank line", "gemini", []string{band, " > ", "   second line", "", lowerBand}, 3, 3, true},
		{"gemini blank line inside a draft", "gemini", []string{band, " > first line", "", "   third line", lowerBand}, 13, 3, true},
		{"gemini empty composer", "gemini", []string{band, " > ", "", lowerBand}, 3, 2, false},
		{"hermes wrapped draft", "hermes", []string{
			rule,
			"❯ Run this exact shell command once, replacing <name> with a short 2-4 word keba",
			"en continue.",
			"",
			rule,
		}, 0, 3, true},
		{"hermes collapsed paste", "hermes", []string{
			rule,
			"❯ [Pasted text #1: 9 lines → /tmp/home/.hermes/pastes/paste_1_224143.txt]",
			"",
			rule,
		}, 0, 2, true},
		{"muse draft under its marker row", "muse", []string{voice, "❯", "  hello world", rule}, 13, 2, true},
		{"muse empty composer under its marker row", "muse", []string{voice, "❯", "  ", rule}, 2, 2, false},
		{"cursor caret parked under its footer", "cursor", []string{
			"  → Plan, search, build anything",
			"",
			"  Auto",
			"  /tmp/w · main",
			"",
			"",
		}, 0, 5, false},
		{"command-code caret parked under its footer", "command-code", []string{
			rule,
			"❯ Ask your question...",
			rule,
			"  ? for shortcuts · taste on",
			"",
			"",
		}, 0, 5, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := HasDraftBeforeCaret(engine, test.tool, test.rows, test.caretX, test.caretY); got != test.want {
				t.Fatalf("draft before caret = %v, want %v", got, test.want)
			}
		})
	}
}
