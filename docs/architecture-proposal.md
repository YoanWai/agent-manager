# Application architecture proposal

This branch refactors the running application against upstream `d3e9075a745f47e384ee7801681cdb1958dd4017` (2026-09-30). It changes existing production callers and moves their implementations. The earlier additive architecture POC and archive/inbox-only extraction are supporting evidence, not this deliverable.

## Current application and resulting boundaries

| Concern | Upstream main | This proposal |
| --- | --- | --- |
| Background execution | `ui/poller.go` performs delivery, mailbox processing, status writes, heartbeats, conversation capture and sampling | The actual implementation and algorithm tests live in UI-free `execution`; presentation receives typed snapshots |
| Lifecycle effects | CLI/MCP use `sessioncmd`; TUI duplicates launch, kill, revive/restart and archive ordering | `sessioncmd.Lifecycle` owns shared ordered effects and CLI/MCP use it; TUI lifecycle migration is in progress |
| Resource lifetime | UI owns its open store; CLI/MCP command operations construct and close runtimes independently | `sessioncmd.Backend` explicitly opens or borrows one runtime; borrowed command operations do not close the shared store |
| Presentation state | `Model` has a large flat set of unrelated fields | Eight named, non-embedded groups expose state dependencies; existing selectors, constructors and tests use those groups |
| Composition | UI constructor assembles polling/tool runtime | `app` assembles local services; `main` supplies them to the UI and explicitly binds CLI/MCP backends |
| Extensions | Additional consumers would need UI/store/tmux internals | A copied read projection supports observations; mutating extensions call existing canonical command ports |

```mermaid
flowchart TD
    Main[main composition] --> App[app local assembly]
    Main --> CLI[CLI adapter]
    Main --> MCP[MCP adapter]
    App --> UI[UI presentation]
    App --> Execution[execution runner]
    App --> Commands[sessioncmd backend and lifecycle]
    CLI --> Commands
    MCP --> Commands
    UI --> Commands
    Execution --> Commands
    Execution --> Snapshot[typed snapshots]
    Snapshot --> UI
    Snapshot --> View[read-only extension view]
    Commands --> Infra[config / SQLite store / tmux / hooks / git]
    Execution --> Infra
```

This is an application-wide boundary refactor, not a claim that every feature handler has been extracted into a separate object. UI handlers still use `Model`, the root message router remains substantial, and painting still records geometry. Feature-owned behavior from [#646](https://github.com/YoanWai/agent-manager/issues/646) can be extracted incrementally against these boundaries.

## Design alternatives and synthesis

Two structurally different shapes were compared.

1. Keep the existing substantive `sessioncmd` use cases, bind their dependencies, consolidate lifecycle effects, extract execution, and make presentation state explicit.
2. Replace direct application calls with a remote-first command/event bus covering sessions, terminals, coordination, review, execution and lifecycle. Every frontend would become a protocol client of a dedicated owner process.

The first shape is implemented here. The repository already has substantive application code shared by CLI and MCP; introducing another application facade would add pass-through layers. The second shape would require production protocol semantics, deployment, uncertain outcomes and historical-writer fencing at the same time as the refactor. It becomes appropriate when that process cutover is implemented and verified, rather than as a package organization claim.

The design exploration used the available native design lane and source-grounding reviewers. The configured external design providers were unavailable; this is reduced provider diversity, not a completed four-provider consensus.

## Domain modeling and patterns

The useful domain boundaries are session lifecycle, execution/readiness, coordination, and workspace presentation. Existing transactional task claims, inbox claims and reservation batches remain with their established store/use-case implementations. An aggregate hierarchy or repository interface for every table would add abstraction without removing the difficult policies.

The patterns used are explicit dependency composition, application services, frontend adapters and read projections. A `Backend` has one local resource binding. A `Lifecycle` borrows resources and owns ordered effects. An execution runner owns mutable polling state. Presentation groups own their visible state. Extensions receive value summaries or a narrow command interface, not a pointer to the root model.

Human and session actors are deliberately different. Session archive preserves a live pane and refuses self-archive. Human selection archive snapshots before destructive effects and reconciles the visible selection. Shared machinery must not erase that policy distinction.

Launch label failure remains separately represented because the existing human UI reports it while session commands treat it as cosmetic. Hook cleanup and pane rollback are shared effects. This is explicit compatibility policy, not an assertion that all historical frontend outcomes were identical.

## Execution lifetime and local coordination

`Runner.Run(ctx)` produces a latest-result channel. Maintenance continues when the UI stops draining observations, including during tmux attach. Cancellation stops new passes and waits for outstanding conversation capture before closing the result channel. The composition root drains execution before closing its store.

Cancellation is checked between existing driver calls. Existing tmux subprocess calls do not have a context deadline, so there is no claimed fixed wall-clock shutdown bound.

Reflow and fork-key coordination remain honest in-process runtime APIs. Closures used for local resizing are not a remote transport contract. `ClaimPoller` still provides the existing socket-based ownership policy; it is not process-instance fencing.

## Client compatibility and future owner rollout

| Surface | Evidence in this branch | Remaining boundary |
| --- | --- | --- |
| CLI | Existing names, arguments, vocabulary and output shapes remain; real main binds canonical commands to its explicit backend | Historical binary execution across mixed releases is separate evidence |
| MCP | Existing SDK/default tests plus explicit 2024-11-05, 2025-03-26, 2025-06-18 and 2025-11-25 protocol-mode tests; text fallback, malformed mutation refusal and service errors | These use the current pinned SDK in older modes, not historical client binaries |
| Local service failures | A supplied command service reports its failure; callers do not replace it with a newly opened local service | A network adapter with environment/owner-instance binding is not shipped here |
| SQLite and execution ownership | Existing schema and socket ownership behavior are preserved | Old-writer exclusion, owner-instance fencing and safe in-flight cutover still need a production rollout |
| Multiple devices/workspaces | Explicit profile and tmux driver binding can be composed without the UI constructing execution | A connection catalog, remote transport, capability negotiation and stale projection policy remain separate implementation work |

For a remote implementation, negotiation must use protocol versions and capabilities rather than binary version strings. Reads may omit unsupported optional observations; writes must refuse unsupported semantics before dispatch. A client must capture device/profile and owner identity, the owner must validate that binding at the effect boundary, and an uncertain response must not trigger a local fallback or automatic replay. Adding a transport does not prove that an older binary cannot keep writing SQLite directly.

## Runnable extension and headless examples

`examples/extensions` contains a delivery backlog observer and an explicit archive action. The archive example uses the same canonical command layer as current frontends, refuses invalid callers without state changes, and observes successful archival in the next projection. No plugin registry or duplicate domain implementation is introduced.

`examples/headless` runs the production composition and execution loop without importing UI or Bubble Tea. Supply both a disposable profile and a named tmux server:

```sh
go run ./examples/headless --profile /tmp/am-demo-profile --socket am-poc-demo
```

Use an isolated `TMUX_TMPDIR` for experiments. This example proves reusable headless execution, not exclusive daemon cutover or remote-client compatibility.

## Validation

The clean upstream full race suite passes in an isolated shell environment. Use an empty `ZDOTDIR`, unset `TMUX`, and a separate short `TMUX_TMPDIR`; the local user's startup script otherwise prints unrelated errors into fixture panes.

This first draft exposes execution extraction, backend composition and presentation state changes for review. TUI lifecycle wiring and its final integrated validation remain in progress; later commits will complete them. The pull request records validation for each published head.
