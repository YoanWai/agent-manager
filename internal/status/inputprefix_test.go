package status

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

func TestInputPrefix(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"claude":     {ActivityCutoff: `(?m)^❯`},
		"gemini":     {ActivityCutoff: `(?m)^\s*[>!*] `},
		"unmarked":   {},
		"degenerate": {ActivityCutoff: `(?m)^`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	cases := []struct {
		name   string
		tool   string
		row    string
		prefix string
		ok     bool
	}{
		{"bare marker", "claude", "❯", "❯", true},
		{"marker with input", "claude", "❯ write a test", "❯", true},
		// The marker has to open the row: quoted mid-line it is content.
		{"quoted marker", "claude", "we use ❯ here", "", false},
		{"content row", "claude", "some output", "", false},
		// gemini's composer marker carries its own indent and trailing space.
		{"indented marker", "gemini", "  > hi", "  > ", true},
		{"shell mode marker", "gemini", "  ! ls", "  ! ", true},
		{"tool without a cutoff", "unmarked", "❯", "", false},
		{"unknown tool", "nosuch", "❯", "", false},
		// A cutoff that matches zero width marks no prompt: it would
		// otherwise stamp every row as one.
		{"zero-width cutoff", "degenerate", "any row at all", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			prefix, ok := engine.InputPrefix(c.tool, c.row)
			if prefix != c.prefix || ok != c.ok {
				t.Fatalf("InputPrefix = (%q, %v), want (%q, %v)", prefix, ok, c.prefix, c.ok)
			}
		})
	}
}
