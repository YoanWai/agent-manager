## Stage 1: Ground and design the application-wide architecture
**Goal**: Trace execution and presentation against upstream d3e9075, compare two module designs, and select explicit ownership boundaries.
**Success Criteria**: Existing policy differences and client contracts are recorded; target covers the actual application rather than an additive POC.
**Tests**: Source and caller tracing; baseline build and suite evidence.
**Status**: Complete

## Stage 2: Extract execution and shared application behavior
**Goal**: Move authoritative polling out of UI and share lifecycle behavior across frontends with explicit composition.
**Success Criteria**: Runtime has no Bubble Tea dependency; actual callers use shared behavior; cancellation stops new work and drains in-flight captures before store closure.
**Tests**: Delivery, readiness, mailbox, heartbeat, lifecycle rollback, cancellation and blocked subscriber tests.
**Status**: In Progress

## Stage 3: Refactor presentation and compatibility boundaries
**Goal**: Introduce explicit feature state and bound local application services using the production composition root; preserve multiple MCP protocol contracts.
**Success Criteria**: Existing UI code is reorganized; extension examples exercise real ports; malformed legacy client mutations are refused before dispatch and bound service errors are returned directly.
**Tests**: UI reconciliation and focus tests; negotiated MCP protocol and mutation-refusal matrix; extension examples.
**Status**: Not Started

## Stage 4: Verify the integrated application and review the diff
**Goal**: Validate preserved contracts and document implemented architecture separately from future rollout gates.
**Success Criteria**: Build, vet, formatting and meaningful tests pass; any suite failures have baseline evidence; disposable profile process proof is recorded.
**Tests**: CLI/MCP/UI behavior, focused race checks, full suite, real isolated tmux/TUI exercise.
**Status**: Not Started

## Stage 5: Publish a clean upstream-main comparison
**Goal**: Create a fork PR and cross-fork comparison maintainers can review.
**Success Criteria**: Frozen base matches upstream snapshot; diff shows existing code changes, moves and deletions; remote PR base/head verified.
**Tests**: Remote file list and commit comparison.
**Status**: Not Started
