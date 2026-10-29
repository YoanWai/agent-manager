package execution

import (
	"testing"

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
