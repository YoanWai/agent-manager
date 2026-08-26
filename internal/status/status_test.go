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

// 2026-08-23 real capture: a numbered message the user already sent stays
// on screen above the composer wearing the same ❯ marker a dialog puts on
// its selected option, while the turn answering it is still running.
func TestClaudeSentNumberedMessageDoesNotLookLikeDialog(t *testing.T) {
	engine := defaultEngine(t)
	pane := "⏺ Say go on 1 and 2 and I will build the project.\n" +
		"✻ Brewed for 6m 49s · 9 messages hidden (/focus to show)\n\n" +
		"❯ 1. I think we should make a space for marketing & sales right? 2. lets\n" +
		"  create a local git? the other pane showed:\n\n" +
		"   ❯ 1. Yes\n     2. No\n   Enter to confirm · Esc to cancel\n\n" +
		"⏺ User message cut off mid-sentence; awaiting clarification\n" +
		"  ⎿  $ source ~/.profile 2>/dev/null\n\n" +
		"· Razzle-dazzling… (12m 25s · ↓ 30.1k tokens)\n\n" +
		"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)"
	if got, matched := engine.Match("claude", pane); got != Working || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Working)
	}
	if hold := engine.TypingHold("claude", pane); hold != Working {
		t.Fatalf("TypingHold() = %q want %q", hold, Working)
	}
}

// 2026-08-23 real capture: codex replays a sent message under the same ›
// marker its composer carries, and indents what wrapped, so a numbered
// message reads as its approval dialog the way claude's does.
func TestCodexSentNumberedMessageDoesNotLookLikeDialog(t *testing.T) {
	engine := defaultEngine(t)
	pane := "  This deserves a dedicated CV, not the generic version.\n\n" +
		"─ Worked for 2m 39s ──────────────────────────────────────────\n\n" +
		"› 1. should I use my regular cv or 2. match it to them?\n" +
		"  keep the tone hands-on rather than managerial\n\n" +
		"• Working (12s • esc to interrupt)\n\n" +
		"› Ask Codex to do anything\n"
	if got, matched := engine.Match("codex", pane); got != Working || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Working)
	}
}

// 2026-08-23 real capture: claude's question dialog draws its selected
// option on the composer's own row, so the option sits at the cutoff and
// only the footer under it separates a dialog from a numbered draft.
func TestClaudeQuestionDialogWaits(t *testing.T) {
	engine := defaultEngine(t)
	pane := "✻ Churned for 38s\n\n" +
		"❯ use the AskUserQuestion tool to ask me tabs vs spaces\n\n" +
		"⏺ Tabs or spaces for indentation?\n" +
		"────\n ☐ Indent\n\nTabs or spaces for indentation?\n\n" +
		"❯ 1. Spaces\n     Fixed-width indent. Renders identical everywhere.\n" +
		"  2. Tabs\n     One tab per level.\n  3. Type something.\n" +
		"────\n  4. Chat about this\n\n" +
		"Enter to select · ↑/↓ to navigate · Esc to cancel\n"
	if got, matched := engine.Match("claude", pane); got != Waiting || !matched {
		t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Waiting)
	}
	if hold := engine.TypingHold("claude", pane); hold != Waiting {
		t.Fatalf("TypingHold() = %q want %q", hold, Waiting)
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
		{"claude old limit, later turn-end with no other content", "claude",
			"  ⎿  You've hit your weekly limit · resets 1am (Asia/Jerusalem)\n" +
				"✻ Churned for 2h 0m 54s\n✻ Worked for 5s\n────\n❯ \n────", Finished},
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

// Command Code v1.32.1 fixtures are captures of the real TUI: the resting
// composer, the trust dialog, a live streamed turn (wide and narrow), the
// finished turn, the ⚠ error banner, and the insufficient-credits banner.
// The composer stays visible during a turn, with the busy footer sitting
// above it.
func TestCommandCodePanes(t *testing.T) {
	engine := defaultEngine(t)
	border := "────────────────────────────────────────────────────────────────────────────────────"
	footer := border + "\n❯ Ask your question...\n" + border + "\n  ? for shortcuts · taste on"
	header := "# Command Code v1.32.1\n# models: deepseek-v4-flash-(latest) with max effort · taste-1\n# /tmp/amcmd-proj\n"
	streaming := "⠶ Paragraph 0 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n ◇ Ready...  esc to interrupt • 4s • ↓ 0\n"
	cases := []struct {
		name string
		pane string
		want string
	}{
		{"resting composer", header + footer, Idle},
		{"trust dialog", "Do you trust the files in this folder?\n/tmp/amcmd-proj2\n\nCommand Code may read files in this folder. Reading untrusted files may lead Command Code to behave in unexpected ways.\n\nWith your permission Command Code may execute files in this folder. Executing untrusted code is unsafe.\n\n❯ 1. Yes, proceed\n  2. No, exit\n\n↑/↓ to navigate · enter to select · esc to exit", Waiting},
		{"approval dialog", "Execute Shell Command\nCommand Code needs to execute echo \"hi\" > hello.txt.\n❯ 1. Yes\n  2. Yes, don't ask again for this exact command in this project\n  3. No, tell Command Code what to do differently\n\n↑/↓ navigate · enter select", Waiting},
		{"streaming turn", header + streaming + footer, Working},
		{"streaming turn narrow", "⠶ Paragraph 0 adds a little more of the streaming story\n   so the reply keeps growing past the viewport.\n\n ◇ Ready...  0\n" + footer, Working},
		{"streaming footer with long duration and tokens", header + "⠶ Paragraph 0 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n ○ Channeling…  esc to interrupt • 116m 57s • ↓ 41.1k\n" + footer, Working},
		{"streaming footer with permission note", header + "⠶ Paragraph 0 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n ⌘ Shell command allowed  esc to interrupt • 35m 8s • ↓ 22.0k\n" + footer, Working},
		{"streaming footer, tick counter only", header + "⠶ Paragraph 0 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n ○ Channeling…  116m 57s\n" + footer, Working},
		{"finished turn", header + "⠶ Paragraph 0 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n  Paragraph 1 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n ✻ Worked for 3s\n" + footer, Finished},
		{"thought-for turn end with expand hint", header + "⠶ Paragraph 0 adds a little more of the streaming story so the reply keeps growing past the viewport.\n\n✻ Thought for 2 seconds [ctrl+o to expand]\n" + footer, Finished},
		{"thought-for turn end, plural second", header + "❯ hi\n✻ Thought for 1 second [ctrl+o to expand]\n⠶ Sure.\n✻ Thought for 7 seconds [ctrl+o to expand]\n" + footer, Finished},
		{"thought-for end above a trailing recap", header + "❯ hi\n✻ Thought for 1 second [ctrl+o to expand]\n⠶ Sure.\n✻ Thought for 7 seconds [ctrl+o to expand]\n\nTASTE  Learned\n└ Keep the composer clean.\n" + footer, Finished},
		// the » hint row is chrome, so a turn-end marker above it still
		// reads as the newest summary rather than hiding behind the hint
		{"accept-edits hint under a thought-for end", header + "❯ hi\n✻ Thought for 2 seconds [ctrl+o to expand]\n» accept edits on [shift+tab]\n" + footer, Finished},
		{"fast turn with no worked line", "⠶ Sure, which file should I edit?\n" + footer, Idle},
		{"resumed conversation", "❯ hi\n✻ Thought for 1 second [ctrl+o to expand]\n⠶ Hey! What are we working on today? I can dig into code, build something, debug issues, or explore the repo.\n" + footer, Idle},
		{"current error", "⚠ Error: request failed\n" + footer, Errored},
		{"failed shell command", "❯ run this shell command and report its output: sh -c \"exit 1\"\n✻ Thought for 1 second [ctrl+o to expand]\n SHELL  [sh -c \"exit 1\"]\n └ Exit code: 1\n✻ Thought for 1 second [ctrl+o to expand]\n⠶ The command exited with code 1, as expected. No stdout or stderr output was produced.\n ✻ Worked for 2s\n" + footer, Finished},
		{"insufficient credits", "⚠ You have insufficient credits to make this request. Please purchase more credits to continue using Command Code here: https://example.com\n" + footer, Errored},
		{"question turn", "❯ hi\nWhich file should I edit, A or B?\n ✻ Worked for 3s\n" + footer, Waiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match("command-code", tc.pane); got != tc.want {
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

	cfg = config.Config{
		Tools: map[string]config.Tool{
			"bad": {LimitLine: "("},
		},
	}
	if _, err := NewEngine(cfg); err == nil {
		t.Fatal("expected error for invalid limit_line regex")
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

// The inbox gate asks RuleMatch rather than Match so it can tell a dialog
// drawn over the input line from a session resting on a question. Both of
// the fallbacks Match layers on top would pin that gate shut: a question
// left on screen reads as waiting, and a background wait as working, so a
// resting session would never be handed the message queued for it.
func TestRuleMatchLeavesTheFallbacksToMatch(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name  string
		tool  string
		pane  string
		match string
		rule  string
	}{
		{"a question left at a resting prompt", "claude",
			"⏺ What color now, what color want?\n✻ Crunched for 9s\n────\n❯ \n────\n  ▎ ✧ /plan  enter plan mode",
			Waiting, ""},
		{"a background wait outliving its turn", "claude",
			"⏺ Security agent done. 2 left (logic, backend/API).\n✻ Waiting for 2 background agents to finish\n────\n❯ \n────\n  ⏵⏵ bypass permissions on",
			Working, ""},
		{"a tool nobody configured", "ghost", "anything", Idle, ""},
		{"an approval dialog, which is what a rule is for", "claude",
			"Do you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently",
			Waiting, Waiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match(tc.tool, tc.pane); got != tc.match {
				t.Fatalf("Match = %q, want %q", got, tc.match)
			}
			got, matched := engine.RuleMatch(tc.tool, tc.pane)
			if got != tc.rule || matched != (tc.rule != "") {
				t.Fatalf("RuleMatch = (%q, %t), want (%q, %t)", got, matched, tc.rule, tc.rule != "")
			}
		})
	}
}

// TypingHold is what the poller reads before typing a queued message in,
// so each branch is pinned here where the rules live: no readable input
// region holds, a working or dialog rule holds, and a resting prompt takes
// the text.
func TestTypingHold(t *testing.T) {
	cfg := config.Config{
		Tools: map[string]config.Tool{
			"claude": {
				Command:        "claude",
				DefaultStatus:  "idle",
				ActivityCutoff: `(?m)^> `,
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
	cases := []struct {
		name string
		pane string
		want string
	}{
		{"no input line drawn yet", "starting up...", Working},
		{"mid-turn spinner", "thinking... (esc to interrupt)\n> ", Working},
		{"dialog on screen", "Do you want to proceed?\n ❯ 1. Yes\n   2. No\n> ", Waiting},
		{"resting prompt takes the text", "all done here\n> ", ""},
		{"errored is not a hold", "Error: something broke\n> ", ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := engine.TypingHold("claude", testCase.pane); got != testCase.want {
				t.Fatalf("TypingHold(%q) = %q, want %q", testCase.pane, got, testCase.want)
			}
		})
	}
}

// LastMessage quotes the agent's last message from its beginning, not its
// frame or its tail: the message_start marker finds where the reply began,
// its lines flatten into one, and the input box, shortcut hints, spinner
// rows and turn summaries are all stepped over. A pane that is nothing but
// frame yields an empty quote, and a tool without box rules reports it
// cannot tell at all.
func TestLastMessage(t *testing.T) {
	engine := defaultEngine(t)
	pane := "● Ran the suite.\n" +
		"\n" +
		"● Done. The fix is in auth.go.\n" +
		"  Two tests were touched.\n" +
		"\n" +
		"✻ Cerebrating… (4s · esc to interrupt)\n" +
		"\n" +
		"❯ \n" +
		"  ? for shortcuts"
	line, anchored, ok := engine.LastMessage("claude", pane)
	if !ok || !anchored {
		t.Fatal("claude has an activity cutoff, ok should be true")
	}
	if line != "Done. The fix is in auth.go. Two tests were touched." {
		t.Fatalf("LastMessage = %q, want the last message from its start", line)
	}

	// Current Claude Code bullets replies with ⏺, and prints notices (a
	// plugin banner) after the turn summary; the quote starts at the
	// bullet and stops at the summary, from a real v2.1.240 pane shape.
	realPane := "❯ Reply with exactly this sentence and nothing else: The quick banana ate seventeen kayaks today.\n" +
		"\n" +
		"⏺ The quick banana ate seventeen kayaks today.\n" +
		"\n" +
		"✻ Crunched for 3s\n" +
		"──────────────────────────────\n" +
		"Plugins updated: 7 plugins · Run /reload-plugins to apply\n" +
		"❯ \n" +
		"──────────────────────────────"
	line, anchored, ok = engine.LastMessage("claude", realPane)
	if !ok || !anchored || line != "The quick banana ate seventeen kayaks today." {
		t.Fatalf("real pane quote = %q ok=%v, want the reply alone", line, ok)
	}

	if line, _, ok = engine.LastMessage("claude", "✻ Musing… (2s · esc to interrupt)\n\n❯ "); !ok || line != "" {
		t.Fatalf("frame-only pane: line=%q ok=%v, want empty and true", line, ok)
	}

	// opencode has no message_start, so its newest content line is the quote.
	line, anchored, ok = engine.LastMessage("opencode",
		"     hey. what need?\n     ▣  Build · GLM-5.2 · 22.0s\n  ┃\n  ╹▀▀▀▀")
	if !ok || anchored || line != "hey. what need?" {
		t.Fatalf("opencode fallback quote = %q ok=%v", line, ok)
	}

	if _, _, ok = engine.LastMessage("no-such-tool", pane); ok {
		t.Fatal("unknown tool should report it cannot tell")
	}
	if _, _, ok = engine.LastMessage("claude", "just text, no input box"); ok {
		t.Fatal("pane without the cutoff should report it cannot tell")
	}
}

// InputDraft reads what the user has typed after the composer marker, and
// refuses the placeholder wording a composer paints on its empty row.
func TestInputDraft(t *testing.T) {
	engine := defaultEngine(t)
	if draft, ok := engine.InputDraft("claude", "● Done.\n\n❯ fix the flaky test"); !ok || draft != "fix the flaky test" {
		t.Fatalf("claude draft = %q ok=%v", draft, ok)
	}
	if _, ok := engine.InputDraft("claude", "● Done.\n\n❯ "); ok {
		t.Fatal("empty composer should carry no draft")
	}
	if _, ok := engine.InputDraft("codex", "› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev"); ok {
		t.Fatal("codex placeholder should not read as a draft")
	}
	if draft, ok := engine.InputDraft("codex", "› rename the flag\n  gpt-5.6-terra medium · /home/dev"); !ok || draft != "rename the flag" {
		t.Fatalf("codex draft = %q ok=%v", draft, ok)
	}
	// A gutter composer sits above its cutoff, so the text after the
	// cutoff match is the box's border fill, not what was typed — even
	// when a typed line is sitting right there in the gutter.
	opencode := "┃\n" +
		"┃ fix the flaky test\n" +
		"╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		" ■⬝⬝⬝⬝⬝⬝  esc interrupt"
	if _, ok := engine.InputDraft("opencode", opencode); ok {
		t.Fatal("a gutter composer's border fill must not read as a draft")
	}
}

// LastUserEcho reads the newest prompt the transcript echoes; a pane whose
// reply scrolled the bullet away reports an unanchored LastMessage, which
// is the caller's cue to capture deeper.
func TestLastUserEchoAndScrolledMarker(t *testing.T) {
	engine := defaultEngine(t)
	pane := "❯ first prompt\n" +
		"⏺ First reply.\n" +
		"❯ second prompt goes here\n" +
		"⏺ Second reply.\n" +
		"❯ "
	echoed, ok := engine.LastUserEcho("claude", pane)
	if !ok || echoed != "second prompt goes here" {
		t.Fatalf("LastUserEcho = %q ok=%v, want the newest echoed prompt", echoed, ok)
	}

	scrolled := "tail of a reply that scrolled its bullet away.\n\n❯ "
	line, anchored, ok := engine.LastMessage("claude", scrolled)
	if !ok || anchored {
		t.Fatalf("scrolled pane: anchored=%v ok=%v, want unanchored", anchored, ok)
	}
	if line != "tail of a reply that scrolled its bullet away." {
		t.Fatalf("scrolled fallback = %q", line)
	}
	if !engine.HasMessageStart("claude") || engine.HasMessageStart("opencode") {
		t.Fatal("HasMessageStart should be true for claude, false for opencode")
	}
	if echoed, ok := engine.LastUserEcho("claude", "⏺ Only replies here.\n❯ "); !ok || echoed != "" {
		t.Fatalf("echoless pane: echo=%q ok=%v, want empty and true", echoed, ok)
	}
}

// Echo shapes verified live on 2026-08-23: codex v0.56 (trust dialog and
// composer share the › marker), gemini v0.53 (> echo, ✦ reply), opencode
// v1.18.21 (┃ gutter echo above the reply, composer block on the cutoff).
func TestLastUserEchoPerTool(t *testing.T) {
	engine := defaultEngine(t)

	codexPane := "> You are in /private/tmp/work\n" +
		"  Do you trust the contents of this directory?\n" +
		"› 1. Yes, continue\n" +
		"  2. No, quit\n" +
		"  Press enter to continue\n" +
		"› Reply with exactly: CODEX ECHO TEST DONE.\n" +
		"• CODEX ECHO TEST DONE.\n" +
		"› Ask Codex to do anything\n" +
		"  gpt-5.6-luna medium · /private/tmp/work"
	if echoed, ok := engine.LastUserEcho("codex", codexPane); !ok || echoed != "Reply with exactly: CODEX ECHO TEST DONE." {
		t.Fatalf("codex echo = %q ok=%v", echoed, ok)
	}
	if line, anchored, ok := engine.LastMessage("codex", codexPane); !ok || !anchored || line != "CODEX ECHO TEST DONE." {
		t.Fatalf("codex reply = %q anchored=%v ok=%v", line, anchored, ok)
	}

	geminiPane := " > Reply with exactly: GEMINI ECHO TEST DONE.\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		"✦ GEMINI ECHO TEST DONE.\n" +
		"                  ? for shortcuts\n" +
		"────────────\n" +
		" Shift+Tab to accept edits\n" +
		"▄▄▄▄▄▄▄▄▄▄▄▄\n" +
		" >   Type your message or @path/to/file\n" +
		"▀▀▀▀▀▀▀▀▀▀▀▀"
	if echoed, ok := engine.LastUserEcho("gemini", geminiPane); !ok || echoed != "Reply with exactly: GEMINI ECHO TEST DONE." {
		t.Fatalf("gemini echo = %q ok=%v", echoed, ok)
	}
	if line, anchored, ok := engine.LastMessage("gemini", geminiPane); !ok || !anchored || line != "GEMINI ECHO TEST DONE." {
		t.Fatalf("gemini reply = %q anchored=%v ok=%v", line, anchored, ok)
	}

	opencodePane := "  ┃\n" +
		"  ┃  Reply with exactly: OPENCODE ECHO TEST DONE.\n" +
		"  ┃\n" +
		"     OPENCODE ECHO TEST DONE.\n" +
		"     ▣  Build · Gemini 3.6 Flash · 2.6s\n" +
		"  ┃\n" +
		"  ┃\n" +
		"  ┃  Build · Gemini 3.6 Flash Google\n" +
		"  ╹▀▀▀▀▀▀▀▀▀▀▀▀"
	if echoed, ok := engine.LastUserEcho("opencode", opencodePane); !ok || echoed != "Reply with exactly: OPENCODE ECHO TEST DONE." {
		t.Fatalf("opencode echo = %q ok=%v", echoed, ok)
	}
	if line, _, ok := engine.LastMessage("opencode", opencodePane); !ok || line != "OPENCODE ECHO TEST DONE." {
		t.Fatalf("opencode reply = %q ok=%v", line, ok)
	}
}

// Command Code shapes, verified accountless on v1.32.1 with the inject
// stream: replies open on a static ⠶ row, prompts echo on ❯ like claude,
// and the composer paints "Ask your question..." on its empty row.
func TestCommandCodeRowShapes(t *testing.T) {
	engine := defaultEngine(t)
	pane := "# Command Code v1.32.1\n" +
		"❯ Reply with exactly: CMD ECHO TEST DONE.\n" +
		"⠶ CMD ECHO TEST DONE.\n" +
		"  And a second line of the reply.\n" +
		"────────────────────────\n" +
		"❯ Ask your question...\n" +
		"────────────────────────\n" +
		"  ? for shortcuts · taste on"
	if echoed, ok := engine.LastUserEcho("command-code", pane); !ok || echoed != "Reply with exactly: CMD ECHO TEST DONE." {
		t.Fatalf("command-code echo = %q ok=%v", echoed, ok)
	}
	line, anchored, ok := engine.LastMessage("command-code", pane)
	if !ok || !anchored || line != "CMD ECHO TEST DONE. And a second line of the reply." {
		t.Fatalf("command-code reply = %q anchored=%v ok=%v", line, anchored, ok)
	}
	if _, ok := engine.InputDraft("command-code", "⠶ Done.\n❯ Ask your question..."); ok {
		t.Fatal("the composer placeholder should not read as a draft")
	}
}

// A degenerate cutoff like ^ matches every row at zero width. InputPrefix
// refuses it for tools that did not declare a prefix, and the row-matcher
// behind MatchesActivityCutoff refuses it just the same, so neither door
// can stamp arbitrary rows as composer rows.
func TestDegenerateCutoffStampsNothing(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"degenerate": {Command: "x", ActivityCutoff: "^"},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, ok := engine.InputPrefix("degenerate", "any row at all"); ok {
		t.Fatal("a zero-width cutoff read as an input prefix")
	}
	if engine.MatchesActivityCutoff("degenerate", "any row at all") {
		t.Fatal("a zero-width cutoff read as a composer boundary")
	}
}

// An empty composer is the pristine placeholder or a bare marker, and the
// placeholder closes the row, so a draft that merely quotes it mid-text
// stays a draft. Row shapes measured live on command-code v1.33.0: the
// placeholder shows until the first prompt is typed, and a composer cleared
// afterwards paints "❯" with nothing after it for the rest of the session.
func TestComposerIsEmpty(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"command-code": {
			ActivityCutoff:      `(?m)^❯`,
			ComposerPlaceholder: "Ask your question...",
		},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if !engine.ComposerIsEmpty("command-code", "❯ Ask your question...") {
		t.Fatal("the pristine composer's placeholder was not recognised")
	}
	for _, row := range []string{"❯", "❯ ", "❯   "} {
		if !engine.ComposerIsEmpty("command-code", row) {
			t.Fatalf("a cleared composer %q did not read as empty", row)
		}
	}
	if engine.ComposerIsEmpty("command-code", "❯ fix the Ask your question... bug") {
		t.Fatal("a draft quoting the placeholder read as empty")
	}
	if engine.ComposerIsEmpty("command-code", "❯ retry Ask your question...") {
		t.Fatal("a draft ending with the placeholder read as empty")
	}
	// A tool that declares no placeholder never takes the parked-caret
	// path, so a bare marker of its own is not empty for this purpose.
	plain, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"claude": {ActivityCutoff: `(?m)^❯`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if plain.ComposerIsEmpty("claude", "❯ ") {
		t.Fatal("a tool without a declared placeholder took the parked-caret path")
	}
}
