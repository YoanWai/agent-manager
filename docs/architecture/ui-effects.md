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

An active, queued or unresolved installer refuses clean quit with guidance to
finish or stop its terminal; normal input remains usable. Once no installer is
outstanding, every in-band quit, including self-update, rejects new user work and drains
accepted jobs plus causal Rail follow-ups before emitting `tea.Quit`. Shutdown
then closes command admission, waits for begun effects, cancels and drains the
poller, and closes local resources. Self-exec performs that order explicitly
because `syscall.Exec` bypasses deferred cleanup. Abnormal UI-loop termination
can discard jobs that never began; late command invocation is refused. Begun
work finishes while its borrowed resources remain open.

Runner reflow reports an explicit error if stopped; it cannot silently report
unexecuted work as success. Empty-group work still runs. Tmux commands and control clients now have bounded cancellation and reaping.
Git and filesystem operations do not share one global deadline, so the overall
drain still has no fixed time bound.

## Scope and compatibility

Confirmed human archive, restore, delete, kill, restart and revive, direct revive,
Rail persistence, pane resize/size publication, attach preparation, form and quick
spawn, fork, group creation, rename, move-dialog mutations, and settings/CLI-picker
persistence use this lane. The same lane owns keybinding persistence, focus and
acknowledgment probes, detach markers, and Review mutations. Existing synchronous lifecycle fixture entry points live in test files
and drive the same executor/reconciliation. Confirmation presentation uses the
loaded workspace; workers revalidate membership before mutations. Submission
path/repository validation belongs to the accepted job so a later draft edit or
quit cannot drop it. Picker defaults, path suggestions, worktree probes and
editor path lookup use independent read-only commands with stale-result fences.
Raw keys/mouse/paste and installer start/settle also use the ordered lane.

The queue adds no RPC or plugin protocol. The separate automatic-delivery
contract adds schema receipts and in_flight/uncertain message states; old delivery
writers/readers require its offline cutover. This queue orders
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
wiring gate alongside committed lifecycle, partial-failure, blocked-dialog and
quit-drain scenarios. The wider provider/platform matrix remains separate.

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

## Focus, keys and Review integration

Focus captures the selected session and foreground generation. Later keyboard input or
mouse presses invalidates pending foreground entry, including a return to the same
selection. Detach completion also checks generation, list mode and quit state
before opening Review or an editor. Marker read and clear are serialized within
this process; they are separate tmux calls, not atomic cross-client consumption.

Acknowledgment, focus and attach completions advance the stale-poll fence.
Attach preparation still completes its accepted work, while its foreground
continuation checks the captured mode and generation before taking the terminal.
Review returns to its captured focus origin, never whichever row moved underneath it.
Key saves capture the instance writer and tables. Runtime tables follow the
committed stages even on partial failure. Save admission compares against pending
writes, so restoring the original binding queues behind an earlier change. Errors remain visible; an exited key
picker is reopened explicitly for resubmission.

Review save, handle, send, status and normalization use the root lane. Pure loads
remain concurrent. Normalization re-reads current persisted state when it runs,
so an older load capture cannot overwrite a newer queued save. Accepted save
failures remain visible after Review closes or retargets. Send retains the
persistence/delivery ordering and reports preflight errors. Refused transport
restores drafts; uncertain transport retains the committed round and does not
turn it into resendable drafts. Captured session identity rejects a relaunch
before the send begins. This does not establish durable human send-once receipts
after a crash or revoke another process after the final identity check.

Focused raw keys, mouse reports and paste now share the FIFO effect lane.
Key workers prefer bounded control-client commands and wait for acknowledgment.
They use the driver only when no client exists, and never replay an uncertain
control outcome. Mouse reports also prefer the acknowledged control client; paste uses bounded driver commands.
The captured launch and socket are checked before transport; prompt recording
uses an atomic launch/socket comparison. Tests prove paste cannot be overtaken
by Enter and blocked input preserves Update responsiveness and quit drain.
This orders human input within one frontend, without claiming cross-client pane
authority. LastPrompt remains a conditional display record: a draft is recorded
only after a subsequent working observation, so quitting before that observation
can leave it unrecorded. The input itself is drained; this is not a delivery receipt.

## Remaining-adapter reconciliation

Selected-session quick send uses the FIFO lane, freezes the launch/socket and
composer, and classifies transport as refused, confirmed, or uncertain. Only
confirmed transport records acknowledgment and prompt metadata, together in one
conditional SQLite write. Uncertain transport is never replayed automatically;
newer drafts and attachments remain untouched. This is process-local human input
policy, not a durable send-once protocol across a crash.

Review opening reads preferences in a command while its loading view remains
responsive. Results cannot retarget a reopened or relaunched review. Preparation
can finish behind Review Help without replacing Help. Branch/base picker Git,
SQLite and symlink reads are captured commands; picker Enter validates its source
review. Accepted base saves drain on quit and reload only the originating view.

Notice dismissal is optimistic, deduplicated and persisted by a transactional
merge so another manager's dismissal is retained. Failed persistence restores the
notice without stealing a newer screen. Split saves remain ordered; only the
latest accepted save can publish a failure. Quit captures an active resize before
closing admission. Editor discovery runs alongside existing path/file checks;
foreground and review fences prevent stale results launching an editor.

Startup notices and initial preference reads execute before Bubble Tea starts.
The converted handlers do not establish remote authority or full supported
provider/terminal/platform acceptance; those remain explicit rollout gates.
