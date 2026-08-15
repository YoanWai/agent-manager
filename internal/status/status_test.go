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
					{State: "waiting", Pattern: `(?m)^ ❯ 1\.`},
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

func TestMatch(t *testing.T) {
	engine := testEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"working spinner", "claude", "thinking... (esc to interrupt)", Working},
		{"persisted working-first rules still prefer waiting", "claude",
			"✶ Cooking… (2m 14s · esc to interrupt)\nDo you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently", Waiting},
		{"errored", "claude", "Error: something broke", Errored},
		{"idle fallback", "claude", "> ", Idle},
		{"first rule wins", "claude", "Error: x\nesc to interrupt", Working},
		{"unknown tool", "ghost", "anything", Idle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%q)=%q want %q", tc.pane, got, tc.want)
			}
		})
	}
}

func TestMatchWaitingOnlyOverridesWorkingFirstMatch(t *testing.T) {
	cfg := config.Config{Tools: map[string]config.Tool{
		"custom": {Rules: []config.Rule{
			{State: "errored", Pattern: "error signal"},
			{State: "working", Pattern: "working signal"},
			{State: "waiting", Pattern: "waiting signal"},
		}},
	}}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	if got, _ := engine.Match("custom", "error signal\nworking signal\nwaiting signal"); got != Errored {
		t.Fatalf("Match() = %q want %q", got, Errored)
	}

	cfg.Tools["custom"] = config.Tool{Rules: []config.Rule{
		{State: "working", Pattern: "working signal"},
		{State: "errored", Pattern: "error signal"},
		{State: "waiting", Pattern: "waiting signal"},
	}}
	engine, err = NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if got, _ := engine.Match("custom", "working signal\nerror signal\nwaiting signal"); got != Waiting {
		t.Fatalf("Match() = %q want %q", got, Waiting)
	}
}

func TestMatchLegacyClaudeRuleOrderStillResolvesWaiting(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	claude := cfg.Tools["claude"]
	claude.Rules = []config.Rule{
		{State: Working, Pattern: `(?m)^[✻✳✶✽✢·✦✧+*] \S+… \(`},
		{State: Working, Pattern: "esc to interrupt"},
		{State: Waiting, Pattern: "Enter to confirm"},
		{State: Waiting, Pattern: `(?m)^[ \x{A0}]*❯[ \x{A0}]+\d+\.`},
		{State: Errored, Pattern: `(?im)^\s*error:`},
	}
	cfg.Tools["claude"] = claude
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Include a prior turn and the input cutoff so the real Claude scope
	// settings narrow matching to the current mixed-signal turn.
	pane := "⏺ Previous turn\n✻ Worked for 1s\n" +
		"✶ Cooking… (2m 14s · esc to interrupt)\nDo you want to proceed?\n" +
		" ❯ 1. Yes\n   2. No, and tell Claude what to do differently\n❯ "
	if got, matched := engine.Match("claude", pane); got != Waiting || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Waiting)
	}
}

// Reconstructs the mixed signals reported in issue #112: an unanswered
// approval dialog while Claude's active-turn hint remains visible.
func TestClaudeMixedApprovalPane(t *testing.T) {
	engine := defaultEngine(t)
	pane := "✶ Cooking… (2m 14s · esc to interrupt)\nDo you want to proceed?\n" +
		" ❯ 1. Yes\n   2. Yes, and don't ask again\n" +
		"   3. No, and tell Claude what to do differently"
	if got, matched := engine.Match("claude", pane); got != Waiting || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Waiting)
	}
}

func TestClaudeEnterToConfirmOverridesWorking(t *testing.T) {
	engine := defaultEngine(t)
	pane := "✶ Cooking… (2m 14s · esc to interrupt)\n" +
		"Review the selected choice\nEnter to confirm · Esc to cancel"
	if got, matched := engine.Match("claude", pane); got != Waiting || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Waiting)
	}
}

func TestClaudeNumberedInputDoesNotLookLikeDialog(t *testing.T) {
	engine := defaultEngine(t)
	pane := "✳ Drizzling… (6s · esc to interrupt)\n❯ 1. refactor the parser"
	if got, matched := engine.Match("claude", pane); got != Working || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Working)
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
		// 2026-07-26 real capture: background agents outlive the turn that
		// spawned them, and the wait line has the same shape as a turn-end
		// summary ("glyph word for digit"), so it must not read as finished.
		{"claude waiting on background agents (real capture)", "claude",
			"⏺ Security agent done. 2 left (logic, backend/API).\n✻ Waiting for 2 background agents to finish\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Working},
		{"claude waiting on background agents with hidden-message note (real capture)", "claude",
			"✻ Waiting for 1 background agent to finish · 13 messages hidden (/focus to show)\n────\n❯ \n────\n  ⏵⏵ bypass permissions on", Working},
		{"claude background wait after a completed turn (real capture)", "claude",
			"⏺ done\n✻ Worked for 8m 12s\n  Ran 5 agents\n✻ Waiting for 5 background agents to finish\n────\n❯ \n────", Working},
		{"claude background wait under a recap block", "claude",
			"✻ Waiting for 2 background agents to finish\n※ recap: goal was X; next is Y.\n────\n❯ \n────", Working},
		{"claude background wait superseded by a newer turn", "claude",
			"✻ Waiting for 2 background agents to finish\n⏺ all agents reported\n✻ Worked for 5s\n────\n❯ \n────", Finished},
		// 2026-08-14 real capture: a turn that leaves background shells
		// running says so in its own summary line, the same way the agent
		// wait line does.
		{"claude turn end with one background shell (real capture)", "claude",
			"⏺ ok\n✻ Worked for 3s · 1 shell still running\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 1 shell", Working},
		{"claude turn end with two background shells (real capture)", "claude",
			"  Ran 2 shell commands\n⏺ ok\n✻ Cooked for 4s · 2 shells still running\n────\n❯ \n────\n  ⏵⏵ bypass permissions on · 2 shells", Working},
		{"claude background shells drained by a newer turn", "claude",
			"⏺ ok\n✻ Worked for 3s · 1 shell still running\n⏺ done\n✻ Cooked for 1s\n────\n❯ \n────", Finished},
		// 2026-08-14 real capture: transient banners render under the busy
		// line and say nothing about whether the work drained.
		{"claude background wait under a plugin banner (real capture)", "claude",
			"⏺ ok\n✻ Waiting for 1 background agent to finish · 1 message hidden (/focus to show)\n  Plugins updated: 7 plugins · Run /reload-plugins to apply\n────\n❯ \n────", Working},
		// 2026-08-15 real capture: a weekly/session limit lands above the
		// turn-end summary, so matchScope (text after that summary) never
		// sees it and the quiet turn would otherwise read as finished.
		{"claude weekly usage limit (real capture)", "claude",
			"  ⎿  You've hit your weekly limit · resets 1am (Asia/Jerusalem)\n" +
				"     /usage-credits to finish what you’re working on.\n\n" +
				"✻ Churned for 2h 0m 54s\n────\n❯ \n────", Errored},
		{"claude session limit (real capture)", "claude",
			"You've hit your session limit · resets 9pm (Asia/Jerusalem)\n" +
				"✻ Crunched for 9s\n────\n❯ \n────", Errored},
		{"claude old limit, newer finished turn", "claude",
			"  ⎿  You've hit your weekly limit · resets 1am (Asia/Jerusalem)\n" +
				"✻ Churned for 2h 0m 54s\n  All done now.\n✻ Worked for 5s\n────\n❯ \n────", Finished},
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
		{"opencode usage limit reached", "opencode",
			"  ┃  Usage limit reached. It will reset in 4 hours.\n  ╹▀▀▀▀", Errored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Fixtures below are captured from real grok Build panes (2026-07-18).
func TestGrokRealPanes(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"grok idle at prompt", "grok",
			"  Tip: Press Ctrl+O to toggle auto-approve mode.\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Idle},
		{"grok active turn (braille spinner)", "grok",
			"     Deleting victim.txt.\n    ⠹ Delete victim.txt with rm… 2.5s                6.0s ⇣32.4k [↓][stop]\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Working},
		{"grok waiting-for-response spinner", "grok",
			"    ⠴ Waiting for response… 1.8s                            1.8s ⇣15.4k [stop]\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Working},
		{"grok finished turn", "grok",
			"     ❯ count from 1 to 5\n     1\n     2\n     done\n     Worked for 5.0s.               stop  [hooks: 2]\n\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Finished},
		{"grok finished, whole-second duration", "grok",
			"     Deleted victim.txt.\n     Worked for 25s.               stop  [hooks: 2]\n\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Finished},
		// 2026-07-21: finished lines often drop the trailing period; "stop" still marks end.
		{"grok finished, no trailing period", "grok",
			"     Twin switch fired.\n     Worked for 4m1s               stop  [hooks: 2]\n\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Finished},
		// Live subagent timers print "Worked for 1m20s" without stop; must not end the turn.
		{"grok live subagent timer is not turn end", "grok",
			"     Worked for 1m20s\n     Worked for 1m21s\n    ⠼ Thinking… 3.0s                            1m32s ⇣15.4k [↓][stop]\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Working},
		{"grok finished with scrollbar chrome", "grok",
			"     Worked for 9.5s.               stop  [hooks: 2]   █\n                                                                                          █\n\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Finished},
		{"grok plain-text question ends the turn", "grok",
			"     Which feature do you want, A or B?\n     Worked for 3.2s.            stop  [hooks: 2]\n\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Waiting},
		{"grok old question, newer statement turn", "grok",
			"     Which one?\n     Worked for 4s.               stop  [hooks: 2]\n     All done now.\n     Worked for 2s.               stop  [hooks: 2]\n\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯\n  Shift+Tab:mode  │  Ctrl+x:shortcuts", Finished},
		{"grok first-run trust dialog", "grok",
			"                  Do you trust the contents of this directory?\n                         /Users/someone/projects\n\n            Grok Build may run or modify contents in this directory,\n                             posing security risks.\n\n                         Yes, proceed                 y\n                         No, quit                     n", Waiting},
		{"grok approval dialog (input box replaced)", "grok",
			"  ┃  Remove victim2.txt file\n  ┃  rm victim2.txt\n  ┃\n  ┃  1 (●) Yes, and don't ask again for anything (always-approve mode)\n  ┃  2 (○) Yes, proceed\n  ┃  3 (○) No, reject (type to add feedback)\n  ┃\n\n  1/3:select  │  Ctrl+o:always-approve  │  Ctrl+c:cancel", Waiting},
		{"grok errored", "grok",
			"  error: request failed\n  │ ❯                    │", Errored},
		{"grok rate limit", "grok",
			"     You've hit the rate limit for your plan. Upgrade your account or try again later.\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯", Errored},
		{"grok free usage limit", "grok",
			"     You hit your free usage limit.\n  ╭────────────────────────────╮\n  │ ❯                        │\n  ╰──────────── Grok 4.5 (high) ─╯", Errored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Fixtures below mirror real Codex TUI frames, drawn from Codex's own render
// snapshot tests (openai/codex, codex-rs/tui) and a live-captured session
// (2026-07-18). Working/finished frames come from the snapshots; idle, the
// first-run trust dialog, and the usage-limit error were captured live.
func TestCodexRealPanes(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"codex idle at prompt", "codex",
			"› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Idle},
		{"codex active turn", "codex",
			"• Working (0s • esc to interrupt)\n\n› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Working},
		{"codex active turn, other status verb", "codex",
			"• Analyzing (12s • esc to interrupt)\n\n› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Working},
		{"codex active turn with animations disabled", "codex",
			"Working (12s • esc to interrupt)\n\n› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Working},
		{"codex reconnecting turn with details", "codex",
			"• Reconnecting... 3/5 (1m 04s • esc to interrupt)\n" +
				"  └ Stream disconnected before completion\n\n" +
				"› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Working},
		{"codex numbered draft is not a dialog", "codex",
			"• Working (0s • esc to interrupt)\n\n› 1. keep this as ordinary input\n  gpt-5.6-terra medium · /home/dev", Working},
		{"codex option-shaped draft without footer is not a dialog", "codex",
			"• Working (0s • esc to interrupt)\n\n› 1. Yes, proceed (y)", Working},
		{"codex finished work turn", "codex",
			"• Ran echo preparing\n  └ preparing\n\n────────────────────────────────\n\n• Final response.\n\n─ Worked for 2m 05s ─────────────\n\n› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Finished},
		{"codex finished turn ending on a question", "codex",
			"• Which file should I edit, A or B?\n\n─ Worked for 3s ─────────────────\n\n› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev", Waiting},
		{"codex command-approval modal", "codex",
			"  $ echo hello world\n\n› 1. Yes, proceed (y)\n  2. Yes, and don't ask again for commands that start with `echo hello world` (p)\n  3. No, and tell Codex what to do differently (esc)\n\n  Press enter to confirm or esc to cancel", Waiting},
		{"codex command-approval modal overrides stale working signal", "codex",
			"• Working (0s • esc to interrupt)\n\n  $ echo hello world\n\n› 1. Yes, proceed (y)\n  2. No, and tell Codex what to do differently (esc)\n\n  Press enter to confirm or esc to cancel", Waiting},
		{"codex command-approval modal after completed turn", "codex",
			"• Previous response.\n\n─ Worked for 1s ─────────────\n\n• Working (0s • esc to interrupt)\n\n  $ echo hello world\n\n› 1. Yes, proceed (y)\n  2. No, and tell Codex what to do differently (esc)\n\n  Press enter to confirm or esc to cancel", Waiting},
		{"codex first-run trust dialog", "codex",
			"Do you trust the contents of this directory? Working with untrusted contents comes with higher risk of prompt injection.\n\n› 1. Yes, continue\n  2. No, quit\n\n  Press enter to continue", Waiting},
		{"codex request-user-input selection", "codex",
			"  Choose an option.\n\n  › 1. Option 1  First choice.\n    2. Option 2  Second choice.\n\n  tab to add notes | enter to submit answer | esc to interrupt", Waiting},
		{"codex usage limit", "codex",
			"■ You've hit your usage limit. Upgrade to Plus to continue using Codex, or try again at Jul 22nd, 2026 10:42 AM.\n\n› Ask Codex to do anything", Errored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Gemini fixtures: idle, the auth dialog, the usage-limit dialog and the
// API-error frame are captured from real gemini v0.53.0 panes (2026-07-31);
// the working spinner and tool-confirmation frames reconstruct that
// version's rendering source ("(esc to cancel, Ns)" loading suffix,
// "Waiting for user confirmation..." tip, RadioButtonSelect's "● N."
// selected row).
func TestGeminiPanes(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		tool string
		pane string
		want string
	}{
		{"gemini idle at prompt", "gemini",
			" Gemini CLI v0.53.0\nTips for getting started:\n1. Create GEMINI.md files to customize your interactions\n──────────────────────────────\n Shift+Tab to accept edits\n▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n >   Type your message or @path/to/file\n▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n workspace (/directory)   branch   sandbox   /model\n /Users/dev/proj          main     no sandbox     gemini-2.5-flash", Idle},
		{"gemini active turn", "gemini",
			" Press Ctrl+O to show more lines of the last response\n ⠧ Thinking... (esc to cancel, 4s)                        ? for shortcuts\n──────────────────────────────\n Shift+Tab to accept edits\n▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n >   Type your message or @path/to/file\n▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀", Working},
		{"gemini tool confirmation dialog", "gemini",
			"╭──────────────────────────────────────╮\n│ Edit example.txt                     │\n│ Apply this change?                   │\n│ ● 1. Allow once                      │\n│   2. Allow always                    │\n│   3. No, suggest changes (esc)       │\n╰──────────────────────────────────────╯\n⡏ Waiting for user confirmation...", Waiting},
		{"gemini confirmation tip without dialog rows", "gemini",
			"⡏ Waiting for user confirmation...\n\n >   Type your message or @path/to/file", Waiting},
		{"gemini errored", "gemini",
			" > Count from 1 to 5, one number per line, then stop.\n▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n✕ [API Error: An unknown error occurred.]\nℹ This request failed. Press F12 for diagnostics, or run /settings and change \"Error Verbosity\" to full for\n  full details.\n▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n >   Type your message or @path/to/file", Errored},
		{"gemini first-run auth dialog", "gemini",
			"╭──────────────────────────────╮\n│ ? Get started                │\n│                              │\n│   How would you like to authenticate for this project?  │\n│                              │\n│   ● 1. Sign in with Google   │\n│     2. Use Gemini API Key    │\n│     3. Vertex AI             │\n│                              │\n│   (Use Enter to select)      │\n╰──────────────────────────────╯", Waiting},
		{"gemini usage-limit dialog", "gemini",
			"╭──────────────────────────────────────╮\n│                                      │\n│ Usage limit reached for gemini-3.5-flash.  │\n│ /stats model for usage details       │\n│ /model to switch models.             │\n│                                      │\n│ ● 1. Keep trying                     │\n│   2. Stop                            │\n│                                      │\n╰──────────────────────────────────────╯", Errored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Hermes fixtures follow the classic prompt_toolkit interface in Hermes Agent
// v0.20.0. Agent Manager launches --cli explicitly so user TUI preferences do
// not change these status surfaces underneath the detector.
func TestHermesPanes(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		pane string
		want string
	}{
		{"idle at prompt",
			"Welcome to Hermes Agent\n  ⚕ hermes-4 │ ctx -- │ ⏲ 0s\n────────────────────────\n❯ ", Idle},
		{"profile-prefixed prompt",
			"  ⚕ hermes-4 │ ctx -- │ ⏲ 0s\n────────────────────────\ncoder ❯ ", Idle},
		{"active turn",
			"  ◇ cogitating...  (  4.2s)\n  ⚕ hermes-4 │ ctx -- │ ⏱ 4s\n────────────────────────\n⚕ ❯ msg=interrupt · /queue · /bg · /steer · Ctrl+C cancel", Working},
		{"approval dialog",
			"╭────────────────────────╮\n│ Run rm build.tmp?      │\n│ Allow once             │\n│ Deny                   │\n╰────────────────────────╯\n  ↑/↓ to select, Enter to confirm  (299s)\n⚠ ❯ ", Waiting},
		{"clarify free text",
			"╭────────────────────────╮\n│ Which target?          │\n╰────────────────────────╯\n  type your answer and press Enter\n✎ ❯ ", Waiting},
		{"first-run setup",
			"It looks like Hermes isn't configured yet -- no API keys or providers found.\nRun setup now? [Y/n] ", Waiting},
		{"first-run provider setup",
			"⚕ No inference provider is configured yet — let's fix that.\n  Set up a provider now? [Y/n]: ", Waiting},
		{"background work",
			"Started a background delegation.\n  ⚕ hermes-4 │ ctx -- │ ⛓ 2 │ ⏲ 8s\n────────────────────────\n❯ ", Working},
		{"rate limited",
			"❌ Rate limited after 3 retries — too many requests\n  ⚕ hermes-4 │ ctx -- │ ⏲ 0s\n────────────────────────\n❯ ", Errored},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match("hermes", tc.pane); got != tc.want {
				t.Fatalf("Match() = %q want %q", got, tc.want)
			}
		})
	}

	if got := engine.TurnEndedState("hermes", "╭────╮\n│ All done. │\n╰────╯\n  ⚕ hermes-4 │ ctx -- │ ⏲ 4s\n────────"); got != Finished {
		t.Fatalf("completed turn = %q want finished", got)
	}
	if got := engine.TurnEndedState("hermes", "╭────╮\n│ Which target? │\n╰────╯\n  ⚕ hermes-4 │ ctx -- │ ⏲ 4s\n────────"); got != Waiting {
		t.Fatalf("question turn = %q want waiting", got)
	}
}

// Pi 0.83.0 fixtures cover its resting editor, active spinner, project-trust
// selector, and final question or error directly above the editor.
func TestPiPanes(t *testing.T) {
	engine := defaultEngine(t)
	editor := "\n\n──────────────────────────────\n\n──────────────────────────────\n~/dev/project (main)\nanthropic/claude-sonnet-4"
	trust := "──────────────────────────────\n\nProject trust\n~/dev/project\n\nSaved decision: none\nCurrent session: untrusted\n\n→ Trust this project\n  Keep it untrusted\n\n↑↓ navigate  enter save  esc cancel\n\n──────────────────────────────"
	cases := []struct {
		name string
		pane string
		want string
	}{
		{"resting turn", "Implementation complete." + editor, Finished},
		{"resumed session", "Resumed session" + editor, Idle},
		{"resumed session with trailing blanks", "Resumed session" + editor + "\n\n", Idle},
		{"historical resumed frame", "Resumed session" + editor + "\n\nImplementation complete." + editor, Finished},
		{"active turn", "⠋ Working on the request" + editor, Working},
		{"active turn with trailing blanks", "⠋ Working on the request" + editor + "\n\n", Working},
		{"shell command", "⠙ Running command" + editor, Working},
		{"project trust", trust, Waiting},
		{"historical project trust", "Project trust\n\nTrust accepted.\n\nImplementation complete." + editor, Finished},
		{"historical spinner", "⠋ Working on the request\n\nImplementation complete." + editor, Finished},
		{"historical spinner frame", "⠋ Working on the request" + editor + "\n\nImplementation complete." + editor, Finished},
		{"final question", "Which option do you prefer?" + editor, Waiting},
		{"question with trailing blanks", "Which option do you prefer?" + editor + "\n\n", Waiting},
		{"old question", "Which option do you prefer?\n\nI used option A." + editor, Finished},
		{"historical question frame", "Which option do you prefer?" + editor + "\n\nImplementation complete." + editor, Finished},
		{"current error", "Error: request failed" + editor, Errored},
		{"rate limit reached", "Hugging Face rate limit reached" + editor, Errored},
		{"question-mark error", "Error: request failed?" + editor, Errored},
		{"error with trailing blanks", "Error: request failed" + editor + "\n\n", Errored},
		{"old error", "Error: first attempt failed\n\nRetry completed." + editor, Finished},
		{"historical error frame", "Error: request failed" + editor + "\n\nImplementation complete." + editor, Finished},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match("pi", tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Gemini closes turns without a summary line, so resting status comes from
// TurnEndedState over the quiet region. The "? for shortcuts" hint and the
// approval-mode banner sit above the composer; both must count as chrome
// or the hint's "?" would read every finished turn as a question.
func TestGeminiTurnEndedState(t *testing.T) {
	engine := defaultEngine(t)
	finishedRegion := "✦ Dark screen glows with text\n  Commands flow, stories unfold\n  Prompt waits, ready now\n Press Ctrl+O to show more lines of the last response\n                    ? for shortcuts\n──────────────────────────────\n Shift+Tab to accept edits\n▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n"
	if got := engine.TurnEndedState("gemini", finishedRegion); got != Finished {
		t.Fatalf("TurnEndedState(finished region) = %q want %q", got, Finished)
	}
	questionRegion := "✦ Should I refactor module A or module B?\n\n                    ? for shortcuts\n Shift+Tab to accept edits\n"
	if got := engine.TurnEndedState("gemini", questionRegion); got != Waiting {
		t.Fatalf("TurnEndedState(question region) = %q want %q", got, Waiting)
	}
}

func TestTurnEndedState(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{"plain response", "• Final response.\n\n", Finished},
		{"question response", "• Which file should I edit, A or B?\n\n", Waiting},
		{"question above trailing separator", "• Which file?\n\n────────────\n", Waiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.TurnEndedState("codex", tc.region); got != tc.want {
				t.Fatalf("TurnEndedState = %q want %q", got, tc.want)
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
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
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
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
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
	if got, _ := engine.Match("claude", pane); got != Finished {
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
		{"codex marker-less turn quoting interrupt hint", "codex",
			"  Output:\n\n" +
				"  tool:       mytool\n" +
				"  result:     working\n" +
				"  pattern:    esc to interrupt\n" +
				"  default:    idle\n\n" +
				"› Summarize recent commits\n" +
				"  gpt-5.6-sol medium · /home/dev", Idle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}
