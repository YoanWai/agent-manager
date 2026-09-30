# Conformance, evidence, and next increments

## PR #2 is partially conformant

This audit describes implementation commit `c7a7e8247190f99a4813e6ad92078180ae0fdd3b` against upstream base `d3e9075a745f47e384ee7801681cdb1958dd4017`. The documentation update does not change executable behavior. Source links below follow the proposal branch; use the audited commit for a frozen comparison.

| Requirement | Status | Evidence or remaining gap |
| --- | --- | --- |
| UI-free maintenance and reusable observations | Implemented in this slice | [execution](../../internal/execution), [projection](../../internal/execution/view.go), [headless example](../../examples/headless) |
| Explicit local composition and borrowed resource lifetime | Implemented in this slice | [app](../../internal/app), [backend](../../internal/sessioncmd/backend.go), [main](../../main.go) |
| Shared lifecycle with explicit actor and failure policies | Implemented in this slice | [lifecycle](../../internal/sessioncmd/lifecycle.go), [contract tests](../../internal/sessioncmd/backend_lifecycle_test.go), [UI calls](../../internal/ui/lifecycle.go) |
| Immediate reconciliation of partial lifecycle effects | Partial | [UI archive and restore](../../internal/ui/lifecycle.go) return on error before membership reconciliation; completed durable changes become visible after a later poll. Delete reconciles partial removals |
| Feature-owned UI behavior and narrow hosts | Partial | Named state groups exist, but [root model](../../internal/ui/model.go) still owns substantial behavior and [services](../../internal/ui/services.go) exposes broad dependencies |
| Nonblocking Update and read-only View | Not conformant yet | [confirm handler](../../internal/ui/lifecycle.go) calls archive, restore, and delete synchronously; [list rendering](../../internal/ui/listview.go) records geometry during View |
| Files organized by concern and source-adjacent tests throughout | Partial | Execution extraction is real; large UI, store, status, tmux, and command files remain; the full #646 cleanup is not completed |
| Production controller workspace and remote adapter | Deferred | Saved connections and SSH remain historical PR #1 experiments |
| Exclusive authority and historical writer cutover | Deferred | [ClaimPoller](../../internal/execution/poller.go) retains socket-based coordination without process-instance fencing |
| Released-client compatibility | Deferred | Current pinned SDK checks older MCP protocol modes; synthetic PR #1 revisions are not released-client evidence |
| Extensions using canonical production commands | Implemented for representative examples | [extensions](../../examples/extensions) demonstrate observations and explicit archive, not a general plugin platform |

Nonblocking Update and read-only View are existing requirements, not optional polish. Shared lifecycle extraction did not resolve them. The preserved synchronous UI paths must be converted without hiding I/O behind a differently named method.

## Separate measured evidence from release acceptance

[Hosted CI for implementation commit c7a7e82](https://github.com/ribeirojose/agent-manager/actions/runs/36738420273) passed Linux race and coverage tests, build, formatting, vet, vulnerability checks, and secret scanning. [CodeQL](https://github.com/ribeirojose/agent-manager/actions/runs/36738420518) passed separately.

The local isolated full race suite passed. Disposable-profile TUI and headless process checks proved rendering, observations, and clean exits. Those checks do not demonstrate every lifecycle action through every supported tool, terminal, platform, SSH route, and input method.

The older MCP-mode tests use the current SDK. The PR #1 owner, workspace, SSH, and generation tests prove their bounded fixture contracts. Neither evidence class substitutes for the released-binary matrix or exclusive-owner cutover.

The [repository guidelines](../../AGENTS.md) retain the full product matrix. PR #2 is a scoped draft proposal, not a declaration that every release acceptance condition is met.

## Deliver follow-ups as independently verifiable units

| Unit | Deliverable | Acceptance evidence |
| --- | --- | --- |
| 1. Documentation | Reconcile historical rationale, current code, and future contracts | Source trace, local links, independent claim review; no behavior change |
| 2. Mechanical file splits | Split one package by concern, starting with sessioncmd | Identical declaration inventory, formatting, full race suite; no policy changes |
| 3. Small UI feature | Give help or a small dialog its own behavior and narrow host | Deterministic input and message tests, keyboard and mouse parity, real TUI frames |
| 4. Async effects and layout | Move lifecycle I/O to commands and prepare geometry before View | Blocked-adapter tests prove Update returns; generation tests prove stale rejection; partial-failure tests prove completed archive and restore effects reconcile immediately; real geometry and focus checks |
| 5. Workspace and authority rollout | Ship one saved connection/read use case, then one guarded canonical mutation and writer cutover | Supported historical binaries, real SSH, failure races, single maintenance proof, and explicit old-writer policy |

Each unit needs its own implementation plan. Split unit 4 by feature and unit 5 by contract rather than landing one large rewrite. Concern-based moves and behavior changes remain separately reviewable commits.

## Maintain the audit alongside code

When a follow-up ships, update its row with concrete source and behavioral evidence. Keep deferred requirements until their acceptance conditions pass. Do not make the branch appear conformant by rewriting the target to match its current limitations.
