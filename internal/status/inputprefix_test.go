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

// A tool whose composer row carries no marker can declare its own input
// line with input_prefix. Pi composes on a bare blank row and opencode on
// one of its blank gutter rows, so their declared prefixes are zero-width
// or blank-row wide on purpose: the guard that rejects a degenerate cutoff
// does not apply to a prefix written deliberately.
func TestInputPrefixOverride(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"pi":       {InputPrefix: `^$`},
		"opencode": {InputPrefix: `(?m)^\s*┃`},
		"marker":   {ActivityCutoff: `(?m)^❯`, InputPrefix: `^\s*›`},
		"zerocut":  {ActivityCutoff: `(?m)^`, InputPrefix: `^$`},
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
		{"blank input row", "pi", "", "", true},
		{"typed input row", "pi", "xy", "", false},
		{"content row", "pi", "some output", "", false},
		{"blank gutter row", "opencode", "  ┃", "  ┃", true},
		// The match stops at the bar: a captured row keeps its trailing
		// blanks, and a prefix that swallowed them would sit to the right
		// of every caret. A draft on the row still matches the bar, so the
		// blanks-between-marker-and-caret check is what excludes it.
		{"gutter row holding a draft", "opencode", "  ┃  zz", "  ┃", true},
		{"gutter row holding output", "opencode", "  ┃  Build · Ox Alpha", "  ┃", true},
		{"override wins over cutoff", "marker", "› hi", "›", true},
		{"cutoff row without override match", "marker", "❯ hi", "", false},
		{"explicit prefix beats a degenerate cutoff", "zerocut", "", "", true},
		{"explicit zero-width stamps no content row", "zerocut", "text", "", false},
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
