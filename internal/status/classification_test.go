package status

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"testing"
)

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

func TestOpenCodeDialogRulesDoNotReadOldTranscript(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		old  string
	}{
		{"permission title", "  ┃  △ Permission required was shown earlier\n"},
		{"question footer", "  ┃  ⇆ tab  enter submit  esc dismiss\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := tc.old +
				"     ▣  Build · GLM-5.2 · 4.2s\n" +
				"  ┃\n" +
				"  ╹▀▀▀▀\n" +
				"  /home/dev  ctrl+p commands"
			if got, matched := engine.Match("opencode", pane); got != Finished || !matched {
				t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Finished)
			}
			if got, matched := engine.RuleMatch("opencode", pane); matched {
				t.Fatalf("RuleMatch() = (%q, %t) want no dialog rule", got, matched)
			}
			if hold := engine.TypingHold("opencode", pane); hold != "" {
				t.Fatalf("TypingHold() = %q want no hold", hold)
			}
		})
	}
}

func TestOpenCodeDialogRulesDoNotReadWorkingToolOutput(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name   string
		output string
	}{
		{"permission title", "  ┃  2:\"  ┃  △ Permission required\\n\" +\n"},
		{"question footer", "  ┃  1:\"  ┃  ↑↓ select  enter submit  esc dismiss\\n\" +\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pane := "  ┃  ⠼ grep dialog text\n" +
				tc.output +
				"  ┃\n\n" +
				"     ▣  Build · Big Pickle\n\n" +
				"  ┃\n" +
				"  ┃  Build auto · Big Pickle OpenCode Zen\n" +
				"  ╹▀▀▀▀\n" +
				"   ⬝⬝⬝⬝■■■■  esc interrupt"
			if got, matched := engine.Match("opencode", pane); got != Working || !matched {
				t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Working)
			}
			if got, matched := engine.RuleMatch("opencode", pane); got != Working || !matched {
				t.Fatalf("RuleMatch() = (%q, %t) want (%q, true)", got, matched, Working)
			}
			if hold := engine.TypingHold("opencode", pane); hold != Working {
				t.Fatalf("TypingHold() = %q want %q", hold, Working)
			}
		})
	}
}

func TestOpenCodeDialogsClassifyAsWaiting(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		pane string
	}{
		{"permission request", "     ▣  Build · Big Pickle\n" +
			"  ┃  △ Permission required\n" +
			"  ┃    # Shell command\n" +
			"  ┃  $ ls -la .\n" +
			"  ┃   Allow once   Allow always   Reject          ctrl+f fullscreen  ⇆ select  enter confirm"},
		{"always allow confirmation", "     ▣  Build · Big Pickle\n" +
			"  ┃  △ Always allow\n" +
			"  ┃  This will allow bash until OpenCode is restarted.\n" +
			"  ┃   Confirm   Cancel                         ⇆ select  enter confirm"},
		{"permission rejection explanation", "     ▣  Build · Big Pickle\n" +
			"  ┃  △ Reject permission\n" +
			"  ┃  Tell OpenCode what to do differently\n" +
			"  ┃                                             enter confirm  esc cancel"},
		{"permission title clipped in a narrow pane", "     ▣  Build · Big Pickle\n" +
			"  ┃\n" +
			"  ┃  △Permissio\n" +
			"  ┃   n require\n" +
			"  ┃   d"},
		{"single-select question", "     → Asked 1 question\n" +
			"  ┃  What should the new line be?\n" +
			"  ┃  1. Race-enabled test command\n" +
			"  ┃  2. Type your own answer\n" +
			"  ┃  ↑↓ select  enter submit  esc dismiss\n" +
			"  ┃"},
		{"multi-select question", "     → Asked 2 questions\n" +
			"  ┃  Scope\n" +
			"  ┃  Which checks should run? (select all that apply)\n" +
			"  ┃  1. [ ] Race tests\n" +
			"  ┃  ⇆ tab  ↑↓ select  enter toggle  esc dismiss\n" +
			"  ┃"},
		{"multi-question confirmation", "     → Asked 2 questions\n" +
			"  ┃  First: Race tests\n" +
			"  ┃  Second: Linux\n" +
			"  ┃  ⇆ tab  enter submit  esc dismiss\n" +
			"  ┃"},
		{"question footer wrapped in a narrow pane", "     → Asked 2 questions\n" +
			"  ┃  Review\n" +
			"  ┃  ⇆   enter    esc\n" +
			"  ┃  tab submit   dismiss\n" +
			"  ┃"},
		{"custom answer editor", "     → Asked 1 question\n" +
			"  ┃  What should change?\n" +
			"  ┃  3. Type your own answer\n" +
			"  ┃     Cover every dialog state\n" +
			"  ┃  ↑↓ select  enter submit  esc dismiss\n" +
			"  ┃"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, matched := engine.Match("opencode", tc.pane); got != Waiting || !matched {
				t.Fatalf("Match() = (%q, %t) want (%q, true)", got, matched, Waiting)
			}
			if got, matched := engine.RuleMatch("opencode", tc.pane); got != Waiting || !matched {
				t.Fatalf("RuleMatch() = (%q, %t) want (%q, true)", got, matched, Waiting)
			}
			if hold := engine.TypingHold("opencode", tc.pane); hold != Waiting {
				t.Fatalf("TypingHold() = %q want %q", hold, Waiting)
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
		{"dialog replaced input line", "Do you want to proceed?\n ❯ 1. Yes\n   2. No", Waiting},
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

// A turn that died on a provider error leaves opencode's working marker on
// screen while its footer rests; the guard must agree with Match that the
// turn stopped, while a live retry under an animated footer still holds.
func TestTypingHoldOpencodeDiedTurn(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name  string
		pane  string
		state string
		hold  string
	}{
		{"provider error at a resting prompt",
			"  ┃  Reply with just the word hi.\n  ┃\n  ┃\n  ┃  API key not valid. Please pass a valid API key.\n  ┃\n     ▣  Build · Gemini 3.6 Flash\n  ┃\n  ┃\n  ┃\n  ┃  Build · Gemini 3.6 Flash Google\n  ╹▀▀▀▀\n   /home/dev                    tab agents  ctrl+p commands",
			Errored, ""},
		{"provider retry still running",
			"  ┃  Run the shell command ls -la and tell me what files exist.\n  ┃\n     ▣  Build · Gemini 3.6 Flash\n  ┃\n  ┃\n  ┃\n  ┃  Build · Gemini 3.6 Flash Google\n  ╹▀▀▀▀\n   ⬝⬝⬝⬝⬝■■■ This model is currently experiencing high demand. Spikes in demand are usually t… [retrying in 5s attempt #3",
			Working, Working},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if state, _ := engine.Match("opencode", testCase.pane); state != testCase.state {
				t.Fatalf("Match() = %q, want %q", state, testCase.state)
			}
			if hold := engine.TypingHold("opencode", testCase.pane); hold != testCase.hold {
				t.Fatalf("TypingHold() = %q, want %q", hold, testCase.hold)
			}
		})
	}
}

func TestMusePromptAndReply(t *testing.T) {
	engine := defaultEngine(t)
	for _, divider := range []string{"────────────────", "── Voice input (⌥ + v to start) ────────────"} {
		pane := "❯ earlier prompt\n◆ earlier reply\n❯ hello muse\n\n◆ echo: hello muse\n\n" + divider + "\n❯\n────────────────\n  echo · /work · Auto-review\n"
		if _, ok := engine.ActivityRegion("muse", pane); !ok {
			t.Fatal("composer did not bound the activity region")
		}
		if got := engine.TypingHold("muse", pane); got != "" {
			t.Fatalf("resting prompt held as %q", got)
		}
		if got, ok := engine.LastUserEcho("muse", pane); !ok || got != "hello muse" {
			t.Fatalf("LastUserEcho = %q, %v", got, ok)
		}
		if got, anchored, ok := engine.LastMessage("muse", pane); !ok || !anchored || got != "echo: hello muse" {
			t.Fatalf("LastMessage = %q, %v, %v", got, anchored, ok)
		}
		if got, bounded, ok := engine.FullTurnText("muse", pane); !ok || !bounded || got != "◆ echo: hello muse" {
			t.Fatalf("FullTurnText = %q, %v, %v", got, bounded, ok)
		}
	}
	picker := "  Resume a previous session\n❯ just now    blush-polaris · hello\n  1 / 3 · 34%  enter resume  esc exit"
	if got := engine.TypingHold("muse", picker); got != Waiting {
		t.Fatalf("picker TypingHold = %q", got)
	}
}

func TestAntigravityPanes(t *testing.T) {
	engine := defaultEngine(t)
	sleepTurn := "> Run the shell command sleep 6 and then reply with one short sentence saying it finished.\n\n" +
		"▸ Thought for 3s, 362 tokens\n  Considering available tools, the command execution tool seems appropriate for running `sleep 6`.\n\n" +
		"● Bash(sleep 6) (ctrl+o to expand)\n\n"
	cases := []struct {
		name, pane, match, hold string
	}{
		{"fresh composer", agyLogo + agyRule + "\n>\n" + agyRule + "\n" + agyFooter("? for shortcuts") + agyTail, Idle, ""},
		{"generating", agyPane("> Reply with exactly two short lines: alpha, then beta. No tools.\n⣻  Working...\n", ">", "esc to cancel"), Working, Working},
		{"running a command", agyPane(sleepTurn+"⣾  Running command...\n", ">", "esc to cancel"), Working, Working},
		{"queued message swaps the footer", agyPane("> Count slowly from 1 to 30, one number per line.\n⣷  Generating...\n▸ Then say done.\n", ">", "  Press up to edit queued messages"), Working, Working},
		{"bash mode footer is no turn", agyPane("> hi\n\n  Hello! How can I help you today?\n\n", "!", " activated bash mode · esc to cancel"), Idle, ""},
		{"finished reply", agyPane(sleepTurn+"  The sleep 6 command has finished executing.\n\n", ">", "? for shortcuts"), Idle, ""},
		{"reply quoting the footers", agyPane("> what do the footers say?\n\n  ↑/↓ Navigate · enter Select\n  esc to cancel\n\n", ">", "? for shortcuts"), Idle, ""},
		{"command permission", agyLogo + agyTurnRule + "\n" + sleepTurn + "Command\n" + agyRule + "\n\nRequesting permission for:\n   sleep 6\n\nRun this command?\n" +
			"> 1. Yes, run command\n  2. Yes, and always allow in this conversation for commands that start with 'sleep'\n" +
			"  3. Yes, and always allow for commands that start with 'sleep' (Persist to settings.json)\n  4. No, cancel\n\n" +
			"  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command\n" + agyFooter("esc to cancel") + agyTail, Waiting, Waiting},
		{"file access", agyLogo + agyTurnRule + "\n> /resume\n\n● Read(~/.gemini/antigravity-cli/builtin/skills/antigravity_guide/SKILL.md) (ctrl+o to expand)\n\nFile access\n" + agyRule +
			"\n\nRead: /home/dev/.gemini/antigravity-cli/builtin/skills/antigravity_guide/SKILL.md\nReason: outside workspace\n\nAllow access to this file?\n" +
			"> 1. Yes, allow access\n  2. Yes, and always allow non-workspace access\n  3. No, deny access\n\n  ↑/↓ Navigate\n" + agyFooter("esc to cancel") + agyTail, Waiting, Waiting},
		{"trust prompt", "Accessing workspace:\n\n/tmp/agy/proj2\n\nDo you trust the contents of this project?\n\nAntigravity CLI requires permission to read, edit, and execute files here.\n\n" +
			"> Yes, I trust this folder\n  No, exit\n\n  ↑/↓ Navigate · enter Confirm\n" + agyFooter("") + agyTail, Waiting, Waiting},
		{"slash menu", agyLogo + agyRule + "\n> /resume\n" + agyRule +
			"\n> /resume  Browse and resume past conversations\n\n  ↑/↓ Navigate · enter Select · tab Complete\n" + agyFooter("esc to cancel") + agyTail, Waiting, Waiting},
		{"resume picker", agyLogo + agyRule + "\n>\n" + agyRule + "\n   CLI    Other   (tab to cycle)\n\n  Conversations\n  Type to search conversations...\n" +
			"> Working On My Resume                                                                  proj       3 steps        1m ago\n" +
			"  Run Shell Command Sleep                                                               proj       6 steps        5m ago\n\n" +
			"Keyboard: ↑/↓ Navigate  ←/→ Page  enter Select  f2 Rename  f4 Delete  tab Switch Tab  esc Go back / Clear search\n\n" + agyFooter("") + agyTail, Waiting, Waiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match("antigravity", tc.pane); got != tc.match {
				t.Errorf("Match = %q, want %q", got, tc.match)
			}
			if got := engine.TypingHold("antigravity", tc.pane); got != tc.hold {
				t.Errorf("TypingHold = %q, want %q", got, tc.hold)
			}
		})
	}
}
