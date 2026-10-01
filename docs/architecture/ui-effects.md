# Ordered UI effects

Root `ui` owns one Bubble Tea effect queue. Feature packages continue to own
interaction policy; concrete services execute captured requests outside `Update`.
This is an application adapter boundary, not a new execution authority or RPC
protocol.

## Execution and reconciliation

1. Confirmation copies its selected sessions, action, group, pane size and prior
   watched ID. Rail copies collapse paths and sibling lists. Geometry captures
   target dimensions. Dispatch captures the store, driver, runner, configuration
   and snapshot writer; worker closures do not read `Model`.
2. One command runs at a time. A `sync.Once` guard prevents duplicate execution.
   The active job ID rejects duplicate completions. Navigation can continue while
   work runs; later selection cannot change an accepted target.
3. Completion applies durable results before reporting errors. Per-row archive
   flags and `GroupChanged` describe committed membership, separately from pane
   creation or destruction. Restoring one row and its ancestor groups is one
   SQLite transaction. A failed group restore can leave revived panes archived.
4. Rail calls its feature's `ApplyMutation` only after persistence. Dependent
   mutation input waits for the previous move; collapse saves can queue. Required
   collapse follow-ups execute immediately after their parent and supersede older
   pending collapse snapshots with the combined current preferences. An error discards
   only the failed chain's remaining requests. Label failures remain warnings
   after committed placement.
   Prioritized collapse saves from group reveal, rename and deletion reconciliation
   also supersede older pending collapse snapshots, retaining active work and
   unrelated queued jobs.
5. Lifecycle, Rail, spawn, fork, group creation and rename completion times fence older poll listings. Fresh observations
   reconcile the next snapshot; geometry-only completion does not advance that
   fence. Geometry coalesces only adjacent requests and avoids already accepted
   identical sizes, preserving lifecycle/move order and scrollback rules.

Launch retries retain value requests and failed targets, including unattempted
rows for stop-first actions. They capture updated services/configuration when
redispatched after installation. They do not reread the current confirmation.
A completion does not replace a newly opened dialog or take its attachments.
Watcher recovery uses worker-observed pane survival and current UI selection.

## Lifetime

Every in-band quit, including self-update, rejects new user work and drains
accepted jobs plus causal Rail follow-ups before emitting `tea.Quit`. Shutdown
then closes command admission, waits for begun effects, cancels and drains the
poller, and closes local resources. Self-exec performs that order explicitly
because `syscall.Exec` bypasses deferred cleanup. Abnormal UI-loop termination
can discard jobs that never began; late command invocation is refused. Begun
work finishes while its borrowed resources remain open.

Runner reflow reports an explicit error if stopped; it cannot silently report
unexecuted work as success. Empty-group work still runs. Existing subprocesses
have no shared cancellation/deadline contract, so drain has no fixed time bound.

## Scope and compatibility

Confirmed human archive, restore, delete, kill, restart and revive, direct revive,
Rail persistence, pane resize/size publication, attach preparation, form and quick
spawn, fork, group creation, rename, move-dialog mutations, and settings/CLI-picker
persistence use this lane. Existing synchronous lifecycle fixture entry points live in test files
and drive the same executor/reconciliation. Pre-confirmation reads, keybinding persistence, acknowledgement/focus probes,
review effects and detach request-marker reads remain separate follow-ups. Cached
read-only `View` plus this lane does not make every `Update` path nonblocking.

The schema, CLI/MCP messages and supported tool commands are unchanged. SQLite
transactional restoration is compatible with existing readers. This queue orders
only this frontend process; it does not fence older binaries or other clients
writing the same profile. Released-binary, cross-platform and SSH acceptance,
and exclusive-writer cutover remain in the [rollout plan](compatibility-and-rollout.md).

## Evidence

Fast behavior tests cover captured targets/writers, deferred I/O, copied Rail
requests, FIFO, duplicate commands/completions, closed admission and quit drain.
A blocked snapshot writer proves a window update returns while lifecycle work
is running. Store and lifecycle regressions cover partial durable results and
ancestor rollback. Root integration fixtures explicitly drive completions rather
than assume dispatch already wrote to SQLite/tmux. Pure feature tests still
avoid root's tmux fixture. The existing committed TUI smoke remains the process
wiring gate; lifecycle/failure/drain process scenarios remain roadmap work.

## Dialog effect reconciliation

Spawn requests transfer prompt images into the accepted job. Failed jobs return
images by appending to the original composer when it remains open; they preserve
attachments added while work ran. Successful quick spawns clear only an unchanged
draft. Repeated spawn or fork submissions from the same pending dialog are
refused. Spawn captures its manager ID on admission and retains the assembled
launch plan, including the conversation ID, across installation retries. A
recorded-fork retry retains the child manager ID as well. Reopened forms and group/fork dialogs retain their generation and foreground
state while durable results still update the workspace.

Fork workers validate the captured source conversation and worktree identity
against the current row before typing keys or launching. A recorded ForkKeys
conversation survives child-launch failure in the retry request, so installation
retry reuses that conversation without typing the fork keys again. Delivery or
recording failure reports an uncertain outcome for manual inspection. This is
process-local retry evidence, not a durable crash-recovery protocol.

Rename results carry committed stage flags. Partial failures update rendered rows
before reporting the error. Move dialog closure follows the successful Rail
mutation within its chain and cannot close a reopened or resubmitted dialog.

Settings persist captured writes in order. Partial saves read back each key with
its own error. Runtime preferences reconcile even when the submitting dialog is
stale; dialog fields remain generation-fenced. Each successful later full save
reapplies its captured preferences, so an earlier failure cannot leave the runtime
behind the committed state. Hidden-only saves do not apply unrelated preferences.
An empty committed hidden-tool set clears the dialog map.
