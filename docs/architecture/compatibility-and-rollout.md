# Compatibility and execution-owner rollout

This is a required future production contract. PR #2 implements local command binding and selected protocol-mode checks, not remote negotiation or a completed exclusive-owner rollout.

## Test five independently changing version domains

| Domain | Required upgrade scenario |
| --- | --- |
| Controller build | New controller with an older command endpoint; older controller with a new endpoint |
| Command endpoint on disk | New endpoint communicating with an already running older owner |
| Already running owner | Owner restart or replacement without confusing process identity with binary version |
| Long-lived MCP process | Existing client and server session crossing an upgrade without acquiring broader scope |
| SQLite schema and writers | Old binary encountering a newer schema without corrupting data or bypassing authority |

Wire protocol and storage schema compatibility are separate policies. Support only combinations the project names and tests. Synthetic revisions and current SDK legacy modes do not prove compatibility with released binaries.

Discover versions and capabilities at runtime. A mutation is available only when both the invoked endpoint and running owner implement its mandatory semantics and guards. Refuse missing guards before effects. Never downgrade a guarded write into a legacy mutation.

Compatible reads can omit optional observations. Do not reinterpret a missing required field as a valid destructive target. Keep SQLite migrations append-only and refuse unsupported schemas before writes. Migration success alone does not prove that historical writers are safe.

## Bind scope and authority independently

Construct device-local or controller-workspace scope from trusted process composition. An MCP tool argument cannot broaden a device client into a workspace client.

Capture the selected device, execution profile, session identity, connection generation, and owner instance at dispatch. Distinguish saved connection identity, authoritative environment identity, and process-instance identity. Display labels and version strings are not substitutes for these bindings.

Derive the canonical execution-owner key from the authoritative store identity plus its execution binding, including the tmux socket. A socket claim or an in-process mutex alone cannot prevent a second process from writing the same profile.

Before advertising exclusivity, upgrade, quiesce, reject, or isolate every incompatible legacy writer. Cooperating locks cannot constrain binaries that bypass them. Use a disposable profile to prove that exactly one maintenance path advances and competing writers cannot bypass the selected policy.

## Define uncertainty at the effect boundary

Validate the captured binding at the owner-side effect boundary. A changed connection generation rejects stale UI reconciliation. It does not revoke a command already accepted by the former owner.

If a mutation response is lost, retain its original target and classify the outcome as uncertain. Do not automatically replay against a new endpoint or fall back to a newly opened local service. Operation-specific durable receipts and idempotency need explicit implementation before safe retry is claimed.

Specify freshness and serialization separately for launch, kill, archive, delivery, and raw pane input. A read projection is not input authority. Shared interactive input and resize need explicit writer ownership, takeover, and stale-writer rejection.

## Require real rollout evidence

1. Choose a supported version matrix with pinned released binaries and schemas.
2. Invoke a canonical lifecycle use case through CLI, MCP, and TUI against disposable resources.
3. Race reconnect, retarget, removal, owner replacement, and response loss at documented commit points.
4. Verify missing-capability denial, zero unintended mutation, stable uncertainty, and stale-reply rejection.
5. Verify old-writer exclusion and single maintenance authority before enabling the exclusive-owner guarantee.

Retain the real SSH route test from PR #1 as a replayable experiment. Extend it with supported historical endpoint and owner binaries for production acceptance. Current SSH fixture evidence does not cover that matrix.

## Recovered compatibility acceptance

`tools/compatibility/matrix.py` compares the candidate with a checksummed upstream
v0.39.0 binary on the host platform. The same matrix was also run against
v0.38.0 as an additional older released baseline. The installed native binary
reports `dev`; it was additionally copied into the disposable matrix with SHA256
`1d836a5479dd12e7198e931025876c0e43d54af6a9c8beaa438a8b1da14ddc91`.
Its CLI/MCP and sequential task roundtrips passed without opening the live profile.
This is endpoint compatibility evidence, not a concurrent live-owner cutover. It exercises CLI session/task operations,
five MCP protocol negotiation requests, and sequential profile task-write/read roundtrips
in both version orders. Concurrent task checks additionally create 16 rows and
race six claims through two binaries, preserving every created row and one
claim winner as observed by both readers. These passed against v0.38.0, v0.39.0
and the copied native dev binary. Other mutation families remain unproved.
Protocol requests are not five historical client builds.
No installed client configuration is changed.

`tools/compatibility/two_manager.sh /absolute/path/agent-manager` runs two real
manager processes on one disposable profile with explicit private sockets. It
pauses one process beyond the heartbeat horizon, verifies takeover, resumes it
without stealing a fresh competitor stamp, then verifies reclamation after the
competitor exits. Separate store/runner tests cover stale-claim retirement and
competing claims. These tests do not establish exclusive ownership of in-flight
transport effects, mixed-release concurrent writes beyond the task checks, or real SSH compatibility.

Disposable tests use absolute `tmux -S` paths. A missing `TMUX_TMPDIR` can cause
`tmux -L agentmgr` to resolve to the live server, so named-socket cleanup is not
allowed in these process fixtures. Cleanup tests verify unrelated sentinel
servers and processes survive even when fixture directories are missing.

The [in-flight delivery probe](in-flight-delivery.md) reproduced a paused sender
typing after another Runner retired its claim, while the stored row still
reported a drop. Use one manager process per profile for a bounded pilot;
concurrent execution-owner acceptance remains blocked.

## Automatic-delivery cutover supersedes read/write coexistence

The task matrix still establishes its named endpoint contracts. It does not
permit old automatic delivery writers or receipt readers to share a migrated
profile. [Delivery ownership](delivery-ownership.md) requires all old processes
and their admitted transports to exit before migration. A real v0.39.0 store
claim is refused after the attempt-token fence installs. Older receipt readers
can misread an uncertain terminal timestamp as delivered; upgrade those readers
together. New message_status responses distinguish in_flight and uncertain,
and session responses expose pending_input_outcome as an additive field.

The earlier late-paste log above is historical. Current guarded retirement tests
require the competing owner to skip an in-flight claim. This is cooperating
local automatic-delivery authority, not authority over human or remote input.
