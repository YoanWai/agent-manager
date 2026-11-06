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

// A signed-out turn fires StopFailure, not Stop. A plugin's status row
// under the turn summary keeps the pane from settling, so the hook's
// errored has to stand on its own.
func TestHookErroredHoldsUnderClaudeStatusRow(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	m.poller.statusSources["claude"] = hooks.StatusSourceClaude
	sess := store.Session{ID: "hooked-signed-out", Tool: "claude", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Errored)

	pane := "❯ Say hello.\n  ⎿  Not logged in · Please run /login\n✻ Cogitated for 0s · done 10:40 PM\n" +
		"● full  $0.00  last $0.00  0s                         Review  Audit  Board  Details [-]\n" +
		"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)                      Not logged in · Run /login"
	if got := deriveStatus(t, m, sess, pane, true); got != status.Errored {
		t.Fatalf("signed-out turn under a status row = %q, want errored", got)
	}
}

// The signed-out footer is on screen for the whole session, so a turn
// that is still running keeps reading as working.
func TestHookWorkingHoldsBesideSignedOutFooter(t *testing.T) {
	m := buildModel(t)
	defaultEngine(t, m)
	m.poller.statusSources["claude"] = hooks.StatusSourceClaude
	sess := store.Session{ID: "hooked-starting", Tool: "claude", Status: status.Working}
	writeHookStatus(t, m, sess.ID, status.Working)

	for name, pane := range map[string]string{
		"spinner": "❯ Say hello.\n\n✻ Actioning… (running UserPromptSubmit hook · 0s)\n\n" +
			"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt\n                    Not logged in · Run /login",
		"status row": "❯ Say hello.\n⏺ Reading the file\n" +
			"● full  $0.00  last $0.00  0s                         Review  Audit  Board  Details [-]\n" +
			"────\n❯ \n────\n  ⏵⏵ auto mode on (shift+tab to cycle)                      Not logged in · Run /login",
	} {
		if got := deriveStatus(t, m, sess, pane, true); got != status.Working {
			t.Fatalf("%s: running turn = %q, want working", name, got)
		}
	}
}
