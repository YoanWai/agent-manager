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
5. Lifecycle/Rail completion time fences older poll listings. Fresh observations
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
Rail persistence, pane resize/size publication and attach preparation use this
lane. Existing synchronous lifecycle fixture entry points live in test files
and drive the same executor/reconciliation. Pre-confirmation reads, form/spawn/
fork, move-dialog and rename/settings persistence, acknowledgement/focus probes,
review effects and detach request-marker reads are separate follow-ups. Cached
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
