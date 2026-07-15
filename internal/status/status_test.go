package status

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

func testEngine(t *testing.T) *Engine {
	t.Helper()
	cfg := config.Config{
		Tools: map[string]config.Tool{
			"claude": {
				Command:       "claude",
				DefaultStatus: "idle",
				Rules: []config.Rule{
					{State: "working", Pattern: "esc to interrupt"},
					{State: "errored", Pattern: "(?i)^error:"},
				},
			},
		},
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

func TestDerive(t *testing.T) {
	engine := testEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"working spinner", "claude", "thinking... (esc to interrupt)", Working},
		{"errored", "claude", "Error: something broke", Errored},
		{"idle fallback", "claude", "> ", Idle},
		{"first rule wins", "claude", "Error: x\nesc to interrupt", Working},
		{"unknown tool", "ghost", "anything", Idle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.Derive(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Derive(%q)=%q want %q", tc.pane, got, tc.want)
			}
		})
	}
}

func TestNewEngineBadPattern(t *testing.T) {
	cfg := config.Config{
		Tools: map[string]config.Tool{
			"bad": {Rules: []config.Rule{{State: "working", Pattern: "("}}},
		},
	}
	if _, err := NewEngine(cfg); err == nil {
		t.Fatal("expected error for invalid regex")
	}
}
