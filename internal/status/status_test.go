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

// Fixtures below are captured from real claude/opencode panes (2026-07-16).
func defaultEngine(t *testing.T) *Engine {
	t.Helper()
	cfg, err := config.Default()
	if err != nil {
		t.Fatalf("built-in config: %v", err)
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("engine from built-in config: %v", err)
	}
	return engine
}

func TestDefaultRulesRealPanes(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"claude active turn", "claude",
			"✳ Drizzling… (6s · thinking with medium effort)\n❯ ", Working},
		{"claude long turn", "claude",
			"✶ Cooking… (2m14s · esc to interrupt)\n❯ ", Working},
		{"claude done at prompt", "claude",
			"✻ Cogitated for 13s\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Finished},
		{"claude prompt with nbsp (real capture)", "claude",
			"✻ Cooked for 13s\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Finished},
		{"claude trust dialog", "claude",
			" ❯ 1. Yes, I trust this folder\n   2. No, exit\n Enter to confirm · Esc to cancel", Waiting},
		{"claude permission ask", "claude",
			"Do you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently", Waiting},
		{"claude done with ghost suggestion in prompt", "claude",
			"✻ Cogitated for 13s\n❯ count from 1 to 300", Finished},
		{"claude fresh start, typed unsubmitted", "claude",
			"Try \"fix the build\"\n❯ count from 1 to 300", Idle},
		{"opencode running", "opencode",
			"  ┃  write a haiku\n     ▣  Build · DeepSeek V4 Pro\n   /home/dev  ctrl+p commands", Working},
		{"opencode finished turn", "opencode",
			"     hey. what need?\n     ▣  Build · GLM-5.2 · 22.0s\n   /home/dev  ctrl+p commands", Idle},
		{"opencode fresh prompt", "opencode",
			"  ┃  Ask anything... \"What is the tech stack of this project?\"\n  tab agents  ctrl+p commands", Finished},
		{"opencode out of credits", "opencode",
			"  ┃  This request requires more credits, or fewer max_tokens.", Errored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.Derive(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Derive(%s) = %q want %q", tc.name, got, tc.want)
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
