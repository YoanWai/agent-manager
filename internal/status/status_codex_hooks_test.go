package status

import "testing"

// Codex 0.163 draws a blocking picker on the composer marker and waits.
// The startup hooks review is the render snapshot rust-v0.163.0 publishes
// (codex-rs/tui/src/snapshots/codex_tui__startup_hooks_review__tests__startup_hooks_review_prompt.snap).
// 0.163.0 is not installed here. The installed 0.164.0 stops at its sign-in
// screen before this dialog; that release's snapshot of the same prompt is
// this text. The footer is "enter confirm · esc skip", not "Press enter to
// confirm", and the selected row is the activity cutoff, so the dialog used
// to read as a finished turn.
func TestCodexBlockingPickersWait(t *testing.T) {
	engine := defaultEngine(t)
	hooks := "" +
		"  Hooks need review\n" +
		"  2 hooks are new or changed.\n" +
		"  Hooks can run outside the sandbox\n" +
		"  after you trust them.\n" +
		"\n" +
		"\n" +
		"› 1. Review hooks\n" +
		"  2. Trust all and continue\n" +
		"  3. Continue without trusting (hooks\n" +
		"     won't run)\n" +
		"\n" +
		"  enter confirm · esc skip\n"
	progress := "" +
		"  Hooks need review\n" +
		"  2 hooks are new or changed.\n" +
		"  Hooks can run outside the sandbox\n" +
		"  after you trust them.\n" +
		"  Trusting hooks...\n" +
		"\n" +
		"\n" +
		"›    Review hooks\n" +
		"     Trust all and continue\n" +
		"     Continue without trusting (hooks\n" +
		"     won't run)\n"
	trustError := "" +
		"  Hooks need review\n" +
		"  2 hooks are new or changed.\n" +
		"  Hooks can run outside the sandbox after you trust them.\n" +
		"  Failed to trust hooks: config/batchWrite failed in TUI:\n" +
		"  Invalid configuration: features.fast_mode=true is not\n" +
		"  supported; allowed set [fast_mode=false]\n" +
		"\n" +
		"\n" +
		"› 1. Review hooks\n" +
		"  2. Trust all and continue\n" +
		"  3. Continue without trusting (hooks won't run)\n" +
		"\n" +
		"  enter confirm · esc skip\n"
	// The same picker, drawn under a turn that already closed. The header
	// sits past the turn-end label and the selected row is the cutoff.
	afterTurn := "" +
		"• PONG\n" +
		"  02:41\n" +
		hooks
	// Other 0.163 startup pickers use the same selected-row marker and an
	// "enter confirm" or "enter continue" footer.
	update := "" +
		"  Update available · 0.0.0 → 9.9.9\n" +
		"  Release notes: https://github.com/openai/codex/releases/latest\n" +
		"\n" +
		"› 1. Update now (runs `npm install -g @openai/codex`)                           \n" +
		"  2. Skip\n" +
		"  3. Skip until next version\n" +
		"\n" +
		"  enter continue · esc skip\n"
	migrate := "" +
		"  Codex just got an upgrade. Introducing\n" +
		"  gpt-5.1-codex-max.\n" +
		"\n" +
		"  Upgrade to gpt-5.2-codex for the latest and greatest\n" +
		"  agentic coding model.\n" +
		"\n" +
		"  You can continue using gpt-5.1-codex-mini if you prefer.\n" +
		"\n" +
		"› 1. Try new model                                          \n" +
		"  2. Use existing model\n" +
		"\n" +
		"  enter/esc confirm · ⌃c quit\n"
	cases := []struct {
		name string
		pane string
		want string
	}{
		{"hooks need review", hooks, Waiting},
		{"hooks review while trusting", progress, Waiting},
		{"hooks review after a trust error", trustError, Waiting},
		{"hooks review under a closed turn", afterTurn, Waiting},
		{"update picker", update, Waiting},
		{"model migration picker", migrate, Waiting},
		{"a finished reply that mentions the footer stays finished",
			"• The picker footer reads enter continue once it is up.\n  02:41\n› Ask Codex to do anything\n  gpt-5.1-codex default · /tmp/work", Finished},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match("codex", tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}
