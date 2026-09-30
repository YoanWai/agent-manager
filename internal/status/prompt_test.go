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
