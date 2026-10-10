# Automatic delivery ownership

Automatic inbox and pending-input delivery has one cooperating owner per
profile. The owner holds a nonblocking advisory lock beside the profile DB from
durable admission through the bounded tmux transport and the durable receipt.
SQLite transactions remain short; no database transaction spans process I/O.

This boundary covers the poller's automatic inbox and pending-input paths. Raw
human keyboard, mouse, paste, attach, fork, and review input use their own
ordering boundary and are not made exclusive by this lock.

## Store contract

`Store.WithDeliveryGuard` is the only entry to automatic delivery mutation. It
combines an in-process lock with `flock(LOCK_EX|LOCK_NB)` on
`<state-db>.delivery.lock`. A competing manager skips the tick immediately.
Linux, WSL2 Linux, and Darwin use the same contract.

Every process must address a profile through one canonical existing DB path.
`Store.Open` resolves an existing DB-file symlink before deriving the lock
path. Hard-link aliases, dangling DB-file symlinks, and separately aliased WAL
paths are unsupported because they can name one SQLite database with distinct
advisory lock files.

Inside the callback, a `DeliveryGuard` admits and finishes work with a random
attempt token:

```go
acquired, err := st.WithDeliveryGuard(ctx, func(g *store.DeliveryGuard) error {
    message, queued, err := g.HeadMessage(sessionID)
    if err != nil || !queued {
        return err
    }
    claim, claimed, err := g.ClaimMessage(message.ID, time.Now())
    if err != nil || !claimed {
        return err
    }
    result, sendErr := driver.SendTextContext(ctx, sessionID, message.Body)
    outcome := classify(result, sendErr)
    return g.FinishMessage(message.ID, claim, outcome, time.Now())
})
```

Claims are committed before transport, so they survive a process crash. A
receipt must match the admitted attempt token and update exactly one row. The
four stored outcomes are:

- `in_flight`: admitted while its guard owner is alive.
- `confirmed`: paste and Enter both returned successfully.
- `refused`: transport failed before the paste command started.
- `uncertain`: paste may have started, or a later owner found an ownerless
  claim. Uncertain work is terminal and is never replayed automatically.

Pending input uses the same tokens and outcomes. Its receipt removes only the
claimed head with a compare-and-set, preserving concurrently appended tail
items.

## Transport boundary

`SendTextContext` reports whether load, paste, or submit started. Context
cancellation kills the tmux command and every helper in its process group, then
waits for the direct child before returning. The delivery guard is released
only after that return and the receipt attempt. Ordinary driver commands use a
five-second default deadline. The context-aware in-process and cross-process
tmux attach gates retain the exclusive handshake required by tmux before 3.7.
A persistent control command holds the shared gate from submission through its
acknowledgement. Closing or aborting a control client also acquires that gate,
so it cannot notify the server during another process's exclusive handshake.

Control cleanup is bounded and reports failure explicitly. If close cannot
acquire the gate within its caller and cleanup budgets, it leaves the client
alone for a later close attempt; it does not silently kill through the
handshake. Any close or command cleanup error means the caller has no proof
that the control subprocess was reaped.

If receipt persistence fails after a successful transport, the claim remains
`in_flight`. The next guard owner records it as `uncertain`; it does not paste
again.

## Offline cutover

The schema migration adds attempt, receipt, and outcome columns, then installs
SQLite triggers that reject the old claim and terminal SQL shapes. Existing
delivered rows become `confirmed`; old dropped or admitted-but-unfinished rows
become `uncertain`, because the migration cannot prove whether their paste
started.

This migration requires an offline cutover:

1. Stop every old TUI, poller, CLI, and MCP process using the profile, including
   any already admitted tmux transport, and verify they exited.
2. Back up the profile DB and its WAL files.
3. Open the profile with one new binary so all additive migrations and fences
   install before automatic delivery resumes.
4. Restart only binaries that use `WithDeliveryGuard` and token receipts.

The advisory lock cannot constrain an old binary, and a generation check cannot
revoke a paste that an old process already admitted. The SQL triggers block old
writes only after installation. Running the migration beside active old
processes therefore does not establish exclusive ownership.

Old clients are unsupported after this cutover. Their automatic claim and
terminal statements are refused by the triggers, and their status readers do
not understand `delivery_outcome`: an old reader can mistake the nonzero
terminal timestamp of an `uncertain` row for confirmed delivery. All writers
and receipt/status readers for a profile must move together.

### Released-writer fence probe

The SQL fence can be checked against the actual v0.39.0 store code without
starting a manager or touching a real profile. Run this from a checkout that
contains the new migration. It creates one disposable DB, opens it once with
the new store, then asks the released store implementation to claim its row.

```sh
repo=$PWD
probe=$(mktemp -d)
test ! -e "$repo/internal/store/released_writer_probe_test.go"
cleanup() {
  rm -f "$repo/internal/store/released_writer_probe_test.go"
  rm -rf "$probe"
}
trap cleanup EXIT

mkdir -p "$probe/v039"
git archive v0.39.0 | tar -x -C "$probe/v039"
cat >"$probe/released_writer_probe_test.go" <<'EOF'
package store

import (
	"fmt"
	"os"
	"testing"
	"time"
)

func TestReleasedWriterFenceProbe(t *testing.T) {
	st, err := Open(os.Getenv("AM_PROBE_DB"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	switch os.Getenv("AM_PROBE_PHASE") {
	case "seed":
		id, err := st.Enqueue(InboxMessage{
			SessionID: "target", SenderID: "sender", SenderName: "sender",
			Body: "probe", Fingerprint: "probe", SentAt: time.Now(),
		}, DefaultInboxLimits)
		if err != nil || id != 1 {
			t.Fatalf("seed id=%d err=%v", id, err)
		}
	case "migrate":
		// Open installed the current additive columns and triggers.
	case "claim":
		claimed, err := st.ClaimMessage(1, time.Now())
		fmt.Printf("RELEASE_V039_REFUSED claimed=%v error=%q\n", claimed, err)
		if err == nil || claimed {
			t.Fatalf("released writer was admitted: claimed=%v err=%v", claimed, err)
		}
	default:
		t.Fatal("set AM_PROBE_PHASE")
	}
}
EOF

cp "$probe/released_writer_probe_test.go" "$probe/v039/internal/store/"
cp "$probe/released_writer_probe_test.go" "$repo/internal/store/"
db="$probe/state.db"
(cd "$probe/v039" && AM_PROBE_DB="$db" AM_PROBE_PHASE=seed go test ./internal/store -run '^TestReleasedWriterFenceProbe$')
(cd "$repo" && AM_PROBE_DB="$db" AM_PROBE_PHASE=migrate go test ./internal/store -run '^TestReleasedWriterFenceProbe$')
(cd "$probe/v039" && AM_PROBE_DB="$db" AM_PROBE_PHASE=claim go test -v ./internal/store -run '^TestReleasedWriterFenceProbe$')
```

The final run must print
`RELEASE_V039_REFUSED claimed=false error="constraint failed: delivery claim requires an attempt token (1811)"`.
This proves refusal after migration; it does not make a concurrently running old
process safe, so the stopped offline cutover remains mandatory.

## Deliberate limits

The design gives at-most-once automatic delivery after the offline cutover. It
does not claim exactly-once pane effects, durable response receipts, automatic
retry after a lost response, or authority over human pane input. WAL and the
five-second SQLite busy timeout still allow unrelated short profile writes;
automatic delivery never holds a SQLite lock while tmux runs.
