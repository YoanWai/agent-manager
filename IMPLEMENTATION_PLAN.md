## Stage 1: Review domain model and contracts
**Goal**: Add a private-state child model with feature-local request/result DTOs.
**Success Criteria**: Child tests prove draft lifetime and independent stale-result fences without root/store/tmux fixtures.
**Tests**: `go test ./internal/ui/review`
**Status**: In Progress

## Stage 2: Root adapters and caller migration
**Goal**: Route Review load, persistence, input, and navigation through the child model.
**Success Criteria**: Root owns concrete I/O and return modes; Review owns result acceptance and policy.
**Tests**: Focused `internal/ui` Review tests.
**Status**: Not Started

## Stage 3: Rendering boundary
**Goal**: Render from immutable Review snapshots and remove render-time state mutation.
**Success Criteria**: Root cannot mutate Review fields and Review layout state is prepared before paint.
**Tests**: Review view/navigation tests.
**Status**: Not Started

## Stage 4: Verification and cleanup
**Goal**: Delete old state/policy, verify, and commit working increments.
**Success Criteria**: Build, vet, focused race tests, and diff review pass; plan file removed.
**Tests**: Focused package/race tests; root full suite deferred to coordinator.
**Status**: Not Started
