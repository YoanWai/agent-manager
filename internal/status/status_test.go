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
		{"claude done, blank line before separator (real capture)", "claude",
			"✻ Cooked for 10s\n\n────\n❯ \n────\n  ▎ ○ Haiku 4.5", Finished},
		{"claude prompt with nbsp (real capture)", "claude",
			"✻ Cooked for 13s\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Finished},
		{"claude trust dialog", "claude",
			" ❯ 1. Yes, I trust this folder\n   2. No, exit\n Enter to confirm · Esc to cancel", Waiting},
		{"claude permission ask", "claude",
			"Do you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently", Waiting},
		{"claude done with ghost suggestion in prompt", "claude",
			"✻ Cogitated for 13s\n────\n❯ count from 1 to 300", Finished},
		{"claude plain-text question (real capture)", "claude",
			"⏺ What color now, what color want?\n✻ Crunched for 9s\n────\n❯ \n────\n  ▎ ✧ /plan  enter plan mode", Waiting},
		{"claude old question, newer statement turn", "claude",
			"⏺ What color now?\n✻ Crunched for 9s\n  DONE\n✻ Worked for 10s\n────\n❯ \n────", Finished},
		{"claude interrupted turn (real capture)", "claude",
			"  221\n⎿  Interrupted · What should Claude do instead?\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Waiting},
		{"claude streaming without spinner (real capture)", "claude",
			"  183\n  184\n────\n❯ \n────\n  ▎ ● Fable 5 ✦ medium", Idle},
		{"claude fresh start, typed unsubmitted", "claude",
			"Try \"fix the build\"\n❯ count from 1 to 300", Idle},
		{"opencode running", "opencode",
			"  ┃  write a haiku\n     ▣  Build · DeepSeek V4 Pro\n   /home/dev  ctrl+p commands", Working},
		{"opencode turn ended on a question", "opencode",
			"     hey. what need?\n     ▣  Build · GLM-5.2 · 22.0s\n  ┃\n  ╹▀▀▀▀\n   /home/dev  ctrl+p commands", Waiting},
		{"opencode fresh prompt, nothing ran yet", "opencode",
			"  ┃  Ask anything... \"What is the tech stack of this project?\"\n  tab agents  ctrl+p commands", Idle},
		{"opencode finished with duration (real capture)", "opencode",
			"     HELLO\n     ▣  Build · GLM-5.2 · 13.9s\n  ┃\n  ┃  Build · GLM-5.2 Z.AI Coding Plan · high\n  ╹▀▀▀▀", Finished},
		{"opencode plain-text question (real capture)", "opencode",
			"     What color are you thinking?\n     ▣  Build · GLM-5.2 · 9.7s\n  ┃\n  ┃  Build · GLM-5.2 Z.AI Coding Plan · high\n  ╹▀▀▀▀", Waiting},
		{"opencode old question, newer statement turn", "opencode",
			"     What color?\n     ▣  Build · GLM-5.2 · 9.7s\n     DONE\n     ▣  Build · GLM-5.2 · 4.2s\n  ┃\n  ╹▀▀▀▀", Finished},
		{"opencode question with trailing pad from ansi capture (real)", "opencode",
			"     Which fruit do you want to know more about?   \n     ▣  Build · GLM-5.2 · 10.4s   \n     \n  ┃     \n  ┃  Build · GLM-5.2 Z.AI Coding Plan · high   \n  ╹▀▀▀▀", Waiting},
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

func TestLongTurnAndMidLineQuestion(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"claude long duration with hidden-messages suffix (real capture)", "claude",
			"  Done, runtime-proven.\n✻ Crunched for 8m 48s · 6 messages hidden (/focus to show)\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Finished},
		{"claude question mid final line (real capture)", "claude",
			"  Approve commit? Then I'll redeploy to staging so you can feel it there.\n✻ Crunched for 8m 48s · 6 messages hidden (/focus to show)\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Waiting},
		{"claude statement after older mid-line question", "claude",
			"  Approve commit? ok.\n✻ Crunched for 8m 48s\n  Deployed. All done.\n✻ Worked for 12s\n────\n❯ \n────", Finished},
		{"opencode long duration", "opencode",
			"     All finished here.\n     ▣  Build · GLM-5.2 · 1m 22s\n  ┃\n  ╹▀▀▀▀", Finished},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.Derive(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Derive(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRealPaneEdgeCases(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"claude long spinner without esc hint (real capture)", "claude",
			"✽ Zigzagging… (3m 18s · ↓ 1.4k tokens · thought for 1s)\n────\n❯ ", Working},
		{"claude separator carrying hint text (real capture)", "claude",
			"  Approve commit? Then I'll redeploy to staging.\n✻ Crunched for 8m 48s · 6 messages hidden (/focus to show)\n\n──────────────────    /rc · focus\n❯ nice! works! BUT older prompt echo\n\n✻ Crunched for 2m 2s\n\n──────────────────\n❯ ", Finished},
		{"claude question with dec-graphics separator", "claude",
			"  Ship it now?\n✻ Crunched for 2m 2s\nqqqqqqqqqqqqqqqqqq\n❯ ", Waiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.Derive(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Derive(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestRecapBelowSummary(t *testing.T) {
	engine := defaultEngine(t)
	pane := "  All set on the twin box.\n" +
		"✻ Crunched for 1m 1s · 3 messages hidden (/focus to show)\n" +
		"※ recap: Setting up laptop-casting: twin box is done and proven, now deploying\n" +
		"  plus ports. (disable recaps in /config)\n" +
		"────\n❯ done, code is 431652\n────\n  ⏵⏵ bypass permissions on"
	if got := engine.Derive("claude", pane); got != Finished {
		t.Fatalf("recap below summary should still be finished, got %q", got)
	}
}

func TestQuotedSignalsDoNotTrigger(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"claude quoting spinner and esc text in a finished turn", "claude",
			"  The rule matches \"esc to interrupt\" in the pane.\n" +
				"  Example spinner: ✳ Drizzling… (6s · thinking)\n" +
				"  Menu sample:\n ❯ 1. Yes, I trust this folder\n Enter to confirm\n" +
				"✻ Crunched for 2m 2s\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Finished},
		{"claude quoting menu text then real question", "claude",
			"  We match \" ❯ 1.\" for dialogs. Should I apply it?\n" +
				"✻ Crunched for 1m 5s\n────\n❯ \n────", Waiting},
		{"claude real spinner during turn still working", "claude",
			"  old output\n✻ Crunched for 2m 2s\n  streaming new answer\n✳ Drizzling… (6s · thinking)\n────\n❯ ", Working},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.Derive(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Derive(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}
