package status

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"testing"
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
