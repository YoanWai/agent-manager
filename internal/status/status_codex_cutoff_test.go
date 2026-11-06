package status

import "testing"

const codexComposer = "\n› Ask Codex to do anything\n\n  GPT-5.6-Sol default · ~/src/scheduler"

const codexTurn = "› fix the flaky scheduler test and run the full suite\n\n" +
	"• Running make test-all\n  └ === RUN   TestIntegration\n\n"

func TestCodexCutOffStatusRowIsWorking(t *testing.T) {
	engine := defaultEngine(t)
	cases := map[string]string{
		"24 cols, working": "• Working (13m 05s • es…\n" +
			"  └ Tip: For models with\n" +
			"    adjustable\n" +
			"    reasoning, press…\n",
		"24 cols, reasoning header": "• Checking files and pr…\n",
		"24 cols, background wait": "• Waiting for backgroun…\n" +
			"  └ cargo test -p\n" +
			"    codex-core --…\n",
		"40 cols, background wait": "• Waiting for background terminal (13m …\n" +
			"  └ cargo test -p codex-core -- --exact…\n",
		"40 cols, reasoning header": "• Checking files and preparing a respon…\n" +
			"  └ Tip: For models with adjustable\n" +
			"    reasoning, press alt+. to increase\n" +
			"    reasoning effort or alt+, to…\n",
		"40 cols, inline background terminal": "• Working (13m 05s • esc to interrupt) …\n",
		"60 cols, background wait":            "• Waiting for background terminal (13m 05s • esc to interru…\n",
		"60 cols, reasoning header": "• Checking files and preparing a response (13m 05s • esc to…\n" +
			"  └ Tip: For models with adjustable reasoning, press alt+.\n" +
			"    to increase reasoning effort or alt+, to decrease it.\n",
		"80 cols, inline background terminal": "• Working (13m 05s • esc to interrupt) · 1 background terminal running · /ps to…\n" +
			"  └ checking command policy for deployment commands, production credentials, an…\n",
		"80 cols, long reasoning header": "• Investigating intermittent scheduler deadlock under heavy parallel load (13m …\n" +
			"  └ Tip: For models with adjustable reasoning, press alt+. to increase reasoning\n" +
			"    effort or alt+, to decrease it.\n",
		"animations off, timer kept": "Waiting for background terminal (13m 05s • esc to…\n",
		"blinking bullet off frame":  "◦ Checking files and preparing a respon…\n",
	}
	for name, status := range cases {
		pane := codexTurn + status + "\n" + codexComposer
		if got, ok := engine.Match("codex", pane); !ok || got != Working {
			t.Errorf("%s: Match = %q, %v; want working", name, got, ok)
		}
		if hold := engine.TypingHold("codex", pane); hold != Working {
			t.Errorf("%s: TypingHold = %q; want working", name, hold)
		}
	}
}

func TestCodexStatusRowWithBlocksBelowIsWorking(t *testing.T) {
	engine := defaultEngine(t)
	status := "• Working (13m 05s • esc to interrupt)\n"
	cases := map[string]string{
		"two detail rows": status +
			"  └ checking 日本語 ｶﾞﾊﾟ policy\n" +
			"  └ existing details\n",
		"hook row over a wrapped tip": status +
			"  └ Running PreToolUse hook: checking command policy\n" +
			"  └ Tip: For models with adjustable reasoning, press alt+. to\n" +
			"    increase reasoning effort or alt+, to decrease it.\n",
		"background terminals row": status +
			"  └ Tip: For models with adjustable reasoning, press alt+. to increase reasoning effort or alt+, to decrease it.\n" +
			"\n" +
			"  2 background terminals running · /ps to view · /stop to close\n",
		"queued follow-up": status +
			"\n" +
			"• Queued follow-up inputs\n" +
			"  ↳ Queued follow-up question\n" +
			"    ⇧← edit last queued message\n",
		"details and queued follow-up": status +
			"  └ First detail line\n" +
			"    Second detail line\n" +
			"\n" +
			"• Queued follow-up inputs\n" +
			"  ↳ Queued follow-up question\n" +
			"    ⇧← edit last queued message\n",
		"pending steer": status +
			"\n" +
			"• Messages to be submitted after next tool call\n" +
			"  (press esc to interrupt and send immediately)\n" +
			"  ↳ Please continue.\n",
		"pending steer wrapped on a narrow pane": status +
			"\n" +
			"• Messages to\n" +
			"  be submitted\n" +
			"  after next\n" +
			"  tool call\n" +
			"  ↳ Please\n" +
			"    continue.\n",
		"pending steer above queued follow-up": status +
			"\n" +
			"• Messages to be submitted after next tool call\n" +
			"  (press esc to interrupt and send immediately)\n" +
			"  ↳ Please continue.\n" +
			"\n" +
			"• Queued follow-up inputs\n" +
			"  ↳ Queued follow-up question\n" +
			"    ⇧← edit last queued message\n",
	}
	for name, below := range cases {
		pane := codexTurn + below + "\n" + codexComposer
		if got, ok := engine.Match("codex", pane); !ok || got != Working {
			t.Errorf("%s: Match = %q, %v; want working", name, got, ok)
		}
		if hold := engine.TypingHold("codex", pane); hold != Working {
			t.Errorf("%s: TypingHold = %q; want working", name, hold)
		}
	}
}

func TestCodexRestingTurnWithCutTextIsNotWorking(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name string
		pane string
		want string
	}{
		{"finished on a cut command",
			"• Ran cargo test -p codex-core -- --exact scheduler::tests::flaky_deadl…\n" +
				"  └ ok\n" +
				"\n" +
				"  Worked for 2m 05s • 02:41\n" + codexComposer, Finished},
		{"finished on text ending in an ellipsis",
			"• Done, the suite passes now. Next I would look at the retry loop…\n" +
				"  Worked for 2m 05s • 02:41\n" + codexComposer, Finished},
		{"finished on a quoted cut status row",
			"• The status row read (13m 05s • esc to …\n" +
				"  Worked for 3s • 02:41\n" + codexComposer, Finished},
		{"finished on a quoted status row",
			"• While it runs the row reads • Working (12s • esc to interrupt)\n" +
				"  and then clears.\n" +
				"  Worked for 3s • 02:41\n" + codexComposer, Finished},
		{"quoted status row above the newest output",
			"• Working (12s • esc to interrupt)\n" +
				"• That row is what a running turn shows.\n" +
				"  Worked for 3s • 02:41\n" + codexComposer, Finished},
		{"finished with a background terminals row",
			"• Started the dev server.\n" +
				"  Worked for 3s • 02:41\n" +
				"\n" +
				"  1 background terminal running · /ps to view · /stop to close\n" + codexComposer, Finished},
		{"resting at the composer", "› Ask Codex to do anything\n\n  GPT-5.6-Sol default · ~/src/scheduler", Idle},
	}
	for _, tc := range cases {
		if got, _ := engine.Match("codex", tc.pane); got != tc.want {
			t.Errorf("%s: Match = %q; want %q", tc.name, got, tc.want)
		}
		if hold := engine.TypingHold("codex", tc.pane); hold != "" {
			t.Errorf("%s: TypingHold = %q; want none", tc.name, hold)
		}
	}
}

// A short reply that ends on an ellipsis right above the composer reads the
// same as a reasoning header cut by a narrow pane, since the header's bold
// styling is gone once the pane is stripped. The rule keeps reading it as
// working; the poller's stuck grace settles a static pane within 15s. Codex
// prints a "Worked for" row when a turn ends, so a finished turn normally
// matches turn_end instead.
func TestCodexShortReplyEndingInEllipsisReadsAsWorking(t *testing.T) {
	engine := defaultEngine(t)
	reply := "• Sure, starting with the retry loop…\n"
	cases := map[string]string{
		"bare reply":       reply + codexComposer,
		"reply after turn": codexTurn + reply + codexComposer,
	}
	for name, pane := range cases {
		if got, ok := engine.Match("codex", pane); !ok || got != Working {
			t.Errorf("%s: Match = %q, %v; want working", name, got, ok)
		}
	}
}
