package execution

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// A hooks review dialog blocks on the user. The selected row is the
// activity cutoff and no waiting rule used to match, so a session that
// was working settled to finished once the pane stopped changing.
func TestQuietCodexHooksReviewStaysWaiting(t *testing.T) {
	disableQuietEndGrace(t)
	m := buildModel(t)
	defaultEngine(t, m)
	sess := store.Session{ID: "hooks-review", Tool: "codex", Status: status.Working}
	pane := "" +
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
	seedRegionHash(t, m, sess, pane)
	if got := deriveStatus(t, m, sess, pane, true); got != status.Waiting {
		t.Fatalf("quiet hooks review = %q, want waiting", got)
	}
}

// A failed turn fires StopFailure instead of Stop and still draws its
// turn summary under the error, so the pane reads finished. The hook's
// errored stands until the next prompt or Stop replaces it.
func TestHookErroredHoldsOverTheFailedTurnSummary(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	m.poller.statusSources["claude"] = hooks.StatusSourceClaude
	sess := store.Session{ID: "hooked-failed", Tool: "claude", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Errored)

	// real captures, Claude Code 2.1.319: signed out, and an API error from a local fixture
	for name, pane := range map[string]string{
		"signed out": "❯ Say hello.\n  ⎿ \u00a0Not logged in · Please run /login\n\n✻ Crunched for 0s · done 10:36 PM\n\n" +
			"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)                    Not logged in · Run /login",
		"api error": "❯ Say hello.\n\n● API Error: 400 Review fixture: invalid request\n\n✻ Cooked for 0s · done 6:30 PM\n\n────\n❯ \n────",
		"under a status row": "❯ Say hello.\n  ⎿  Not logged in · Please run /login\n✻ Cogitated for 0s · done 10:40 PM\n" +
			"● full  $0.00  last $0.00  0s                         Review  Audit  Board  Details [-]\n" +
			"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)                      Not logged in · Run /login",
	} {
		for _, acked := range []bool{false, true} {
			sess.Acked = acked
			if got := deriveStatus(t, m, sess, pane, true); got != status.Errored {
				t.Fatalf("%s, acked=%v: failed turn = %q, want errored", name, acked, got)
			}
		}
	}

	sess.Acked = false
	writeHookStatus(t, m, sess.ID, status.Working)
	starting := "❯ Say hello.\n\n✻ Actioning… (running UserPromptSubmit hook · 0s)\n\n" +
		"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt\n                    Not logged in · Run /login"
	if got := deriveStatus(t, m, sess, starting, true); got != status.Working {
		t.Fatalf("next prompt after the failure = %q, want working", got)
	}
	writeHookStatus(t, m, sess.ID, status.Finished)
	done := "❯ Say hello.\n⏺ Hello.\n\n✻ Worked for 2s\n\n────\n❯ \n────"
	if got := deriveStatus(t, m, sess, done, true); got != status.Finished {
		t.Fatalf("next turn's Stop after the failure = %q, want finished", got)
	}
}

// Text that only reads like a failure, in a tool result, a quote or a
// submitted prompt, must not turn a successful turn errored.
func TestHookFinishedHoldsOverQuotedSignedOutText(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	m.poller.statusSources["claude"] = hooks.StatusSourceClaude
	sess := store.Session{ID: "hooked-quoted", Tool: "claude", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Finished)

	for name, pane := range map[string]string{
		"tool result":      "❯ Print the login message.\n⏺ Bash(printf ...)\n  ⎿  Not logged in · Please run /login\n✻ Worked for 2s\n────\n❯ \n────",
		"indented quote":   "❯ What does the footer say?\n⏺ It reads:\n  Not logged in · Run /login\n✻ Worked for 2s\n────\n❯ \n────",
		"multiline prompt": "❯ Explain this footer:\n    Not logged in · Run /login\n⏺ It means the CLI has no account.\n✻ Worked for 2s\n────\n❯ \n────",
	} {
		if got := deriveStatus(t, m, sess, pane, true); got != status.Finished {
			t.Fatalf("%s: successful turn = %q, want finished", name, got)
		}
	}
}
