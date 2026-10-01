# Conformance, evidence, and next increments

## PR #2 is partially conformant

The initial application-boundary audit used implementation commit `c7a7e8247190f99a4813e6ad92078180ae0fdd3b` against upstream base `d3e9075a745f47e384ee7801681cdb1958dd4017`. Subsequent [sessioncmd](sessioncmd-file-map.md) and [UI](ui-file-map.md) mechanical splits preserve declaration bodies while updating file placement. Help, Review, Focus, and Rail ownership are separate behavioral-boundary increments. Source links below follow the proposal branch. The UI mechanical comparison uses the frozen refs recorded below.

| Requirement | Status | Evidence or remaining gap |
| --- | --- | --- |
| UI-free maintenance and reusable observations | Implemented in this slice | [execution](../../internal/execution), [projection](../../internal/execution/view.go), [headless example](../../examples/headless) |
| Explicit local composition and borrowed resource lifetime | Implemented in this slice | [app](../../internal/app), [backend](../../internal/sessioncmd/backend.go), [main](../../main.go) |
| Shared lifecycle with explicit actor and failure policies | Implemented in this slice | [lifecycle](../../internal/sessioncmd/lifecycle.go), [contract tests](../../internal/sessioncmd/backend_lifecycle_test.go), [UI executor](../../internal/ui/effect_lifecycle.go) |
| Immediate reconciliation of partial lifecycle effects | Implemented for human lifecycle | [Typed completion reconciliation](../../internal/ui/effect_lifecycle.go) applies durable rows/groups before errors; row restoration and ancestor flags commit atomically. Pane effects remain distinct from membership |
| Feature-owned UI behavior and narrow contracts | Partial across the whole UI | [Help, Review, Focus, and Rail](ui-feature-packages.md) own policy behind private models and typed value contracts. Other dialogs remain root methods; [services](../../internal/ui/model_services.go) stays in root composition |
| UI feature packages and directory boundaries | Implemented for these four features | [Feature contracts](ui-feature-packages.md), [pure review data](review-data.md), and a [transitive production dependency check](../../tools/architecture/check-ui-boundaries) reject root and runtime imports |
| Repeatable process and TUI end-to-end coverage | Partial | The [committed harness](../../tools/e2e/README.md) and CI exercise Help, Review, Focus, Rail, terminal creation, and clean exit. CLI/MCP/headless/extension flows and the full product matrix remain follow-ups |
| Nonblocking Update and read-only View | Partial | [Frame preparation](../../internal/ui/model_view.go) runs after dispatch; View only reads the prepared string. [Captured typed commands](ui-effects.md) run lifecycle, Rail, geometry, attach, spawn/fork/group, rename/move and settings persistence outside Update; preflight, keybinding saves, review and focus/acknowledgement paths remain follow-ups |
| Files organized by concern and source-adjacent tests throughout | Partial | Execution, sessioncmd, and UI concern families are implemented. Store, status, and tmux taxonomy remain future work; file moves do not establish feature ownership |
| Production controller workspace and remote adapter | Deferred | Saved connections and SSH remain historical PR #1 experiments |
| Exclusive authority and historical writer cutover | Deferred | [ClaimPoller](../../internal/execution/poller.go) retains socket-based coordination without process-instance fencing |
| Released-client compatibility | Deferred | Current pinned SDK checks older MCP protocol modes; synthetic PR #1 revisions are not released-client evidence |
| Extensions using canonical production commands | Implemented for representative examples | [extensions](../../examples/extensions) demonstrate observations and explicit archive, not a general plugin platform |

Nonblocking Update and read-only View are existing requirements, not optional polish. Prepared-frame rendering now resolves the View mutation requirement. Preserved synchronous UI paths still need conversion without hiding I/O behind a differently named method.

## Separate measured evidence from release acceptance

[Hosted CI for implementation commit c7a7e82](https://github.com/ribeirojose/agent-manager/actions/runs/36738420273) passed Linux race and coverage tests, build, formatting, vet, vulnerability checks, and secret scanning. [CodeQL](https://github.com/ribeirojose/agent-manager/actions/runs/36738420518) passed separately.

The local isolated full race suite passed. Disposable-profile TUI and headless process checks proved rendering, observations, and clean exits. Those checks do not demonstrate every lifecycle action through every supported tool, terminal, platform, SSH route, and input method.

The UI taxonomy commit `7773665` preserves 2,371 declarations and 4,350 comment tokens against `560a463`. The subsequent Help increment preserves seven byte-identical captured terminal frames for opening, entering search, typing, committing, clearing, mouse consumption, and scrolling. A disposable profile also opens a real zsh terminal and exits cleanly. The full race suite uses an explicit `/bin/sh` environment and a private tmux socket; clean-zsh UI and tmux package runs pass separately. Those historical runs exposed a shell-readiness race in the tmux environment test. The current proposal waits for the actual relaunched prompt before sending input; no test was disabled or timeout increased.

The older MCP-mode tests use the current SDK. The PR #1 owner, workspace, SSH, and generation tests prove their bounded fixture contracts. Neither evidence class substitutes for the released-binary matrix or exclusive-owner cutover.

The [repository guidelines](../../AGENTS.md) retain the full product matrix. PR #2 is a scoped draft proposal, not a declaration that every release acceptance condition is met.

### Current four-feature integration evidence

The current proposal incorporates upstream `dc471a9`. Local macOS verification on 2026-09-30 passed build, vet, formatting, the production dependency guard, all seven harness contract tests, and `go test -race -p 1 ./...` under a precreated disposable tmux directory with `TMUX` unset and a clean `/bin/sh` environment. The full UI package reported 275.771 seconds; execution reported 34.975 seconds and sessioncmd 132.124 seconds. These are measured package times from this run, not an overall speedup claim.

The actual binary smoke passed in 5.76 seconds and covered Help, real Git Review, Focus input/detach, Rail filter/fold, shell command execution and clean exit. Focused UI/execution race gates passed in 4.631/3.615 seconds. Pure feature tests and import guards avoid root's tmux fixture; the compiled Rail race tests also passed with `PATH=/nonexistent`. The grouped-session rename regression found during independent review is covered by a retained render test.

Hosted checks must be inspected for the published head separately. None of these local gates establishes the wider release matrix or safe live-profile cutover.

## Deliver follow-ups as independently verifiable units

| Unit | Deliverable | Acceptance evidence |
| --- | --- | --- |
| 1. Documentation | Reconcile historical rationale, current code, and future contracts | Source trace, local links, independent claim review; no behavior change |
| 2. Mechanical file splits | Sessioncmd and UI now use concern families; store, status, and tmux remain future work | Sessioncmd preserves 318 declarations; UI preserves 2,371 declarations. The UI comparator checks comments, exported names, build constraints, and init function order; initializer-order notices require source review |
| 3. UI feature packages | Help, Review, Focus, and Rail own interaction policy in child packages; root retains concrete adapters, navigation, and final frame composition | Pure feature tests, copied-data alias tests, dependency checks, root dispatch contracts, and committed TUI smoke |
| 4. Async effects and layout | Prepared geometry, read-only View and ordered lifecycle/Rail/geometry/attach, spawn/fork/group, rename/move and settings commands are implemented; migrate remaining synchronous families in bounded units | Blocked-adapter tests prove Update returns; generation tests prove stale rejection; partial-failure tests prove completed archive and restore effects reconcile immediately; prepared-frame and real geometry checks remain regression gates |
| 5. Workspace and authority rollout | Ship one saved connection/read use case, then one guarded canonical mutation and writer cutover | Supported historical binaries, real SSH, failure races, single maintenance proof, and explicit old-writer policy |

Each unit needs its own implementation plan. Split unit 4 by feature and unit 5 by contract rather than landing one large rewrite. Concern-based moves and behavior changes remain separately reviewable commits.

## Extract UI feature packages incrementally

The original flat `internal/ui` arrangement was an intermediate step. Help, Review, Focus, and Rail now have child packages with private models and source-adjacent policy tests. Root `internal/ui` retains application composition, Bubble Tea dispatch, concrete effect adapters, cross-feature navigation, and final frame composition. The [feature contracts](ui-feature-packages.md) record the exact ownership cut.

The production import checker rejects root UI and concrete runtime dependencies transitively. Review consumes pure line models and Git values. Data ownership tests reject aliases that would let a caller mutate private Review state through load results or copied read views. Focus and Rail derive displayed content and hit geometry together. Root prepares and publishes the complete frame after dispatch; View reads its cached text.

Other dialogs remain candidates for extraction when their context and outcomes form a narrow contract. Do not expose Model, all services, or a callback for every root method to make a move compile. Keep fast feature tests independent of root runtime fixtures, preserve dispatch and adapter contracts in root tests, extend the dependency check for each new package, and update the file map. The [ordered effect lane](ui-effects.md) implements confirmed lifecycle, Rail persistence, geometry, attach preparation,
spawn/fork/group creation, rename/move and settings persistence. Remaining synchronous families retain the acceptance conditions above.

## Turn session checks into repeatable end-to-end tests

The [committed real-terminal harness](../../tools/e2e/README.md) now covers Help, a real Git review, focused shell input and detach, rail filtering and folding, terminal creation, and clean manager exit. It uses disposable profiles and socket directories, waits for observable readiness, and retains diagnostic artifacts. CI runs it as a separate job; the wider process and platform matrix below remains acceptance work. Keep fast policy and rendering tests as the default feedback loop; use end-to-end tests for the process, terminal, and adapter wiring those tests cannot prove.

Deliver the harness in bounded increments:

1. Maintain the implemented deterministic Help TUI smoke test that builds the actual binary and drives a PTY or private tmux server. Cover opening, entering and committing search, clearing, scrolling, consumed mouse events, closing, creating a real terminal, and clean exit. Retain normalized layout goldens as unit tests. Define terminal size and color settings explicitly for process-level assertions.
2. Add CLI, stdio MCP, headless, and representative extension flows against disposable profiles. Assert durable command results, observation delivery, resource lifetime, and clean shutdown. Keep released-client and real SSH matrices as separate acceptance work; current-SDK MCP modes do not substitute for historical binaries.
3. Keep the implemented small pull-request smoke independent of fast policy tests, then add a broader supported-platform matrix on scheduled or release jobs. Record per-test duration and a runtime budget. Share binary builds where practical, retain isolated mutable fixtures, and keep the fast suite independent of the slower process matrix.

Every test must use its own HOME, profile, and socket scope and clean up only resources it created. Wait for observable readiness with bounded deadlines rather than arbitrary sleeps. The existing shell-readiness assertion now waits for the actual interactive shell prompt; the harness does the same before verifying command execution. On failure, retain terminal captures, subprocess logs, exit status, and the relevant profile state for diagnosis. The suite must run from one documented repository command on a fresh checkout without private session files.

## Maintain the audit alongside code

When a follow-up ships, update its row with concrete source and behavioral evidence. Keep deferred requirements until their acceptance conditions pass. Do not make the branch appear conformant by rewriting the target to match its current limitations.

### Async lifecycle and Rail increment

The [effect contract](ui-effects.md) records frozen targets, one-at-a-time execution, partial durable reconciliation, retries and normal versus abnormal shutdown. Regression tests include a deliberately blocked snapshot writer while WindowSize Update returns, FIFO and duplicate/late commands, transactional ancestor rollback and explicit stopped-runner errors. This changes no schema or external protocol and does not establish cross-process writer authority. Add lifecycle failure and quit-drain scenarios to the committed process harness in a follow-up; current smoke continues to prove the existing terminal and feature wiring.

Final local validation of the async increment on 2026-09-30 passed `go test -race -p 1 ./...` with isolated tmux state and `/bin/sh`; UI reported 321.841 seconds and unchanged packages used the valid Go test cache. The focused async/failure/order regression selection passed uncached in 5.952 seconds. Build, vet, gofmt, whitespace and production dependency checks passed, as did all seven harness contract tests. The final binary smoke passed in 7.72 seconds against disposable state. These timings are evidence for those selections, not a suite-wide speedup claim. Hosted checks must be read at the published head separately.

### Dialog effects integration

The four worker slices share the existing root effect lane and canonical lifecycle
service. Integration regressions cover successful settings saves after earlier
failures, preserved newer prompt drafts and attachments, same-target reopened
dialogs, captured fork-source identity, and recorded-fork retries without repeated
keys. Inventory mutations also advance the existing stale-poll fence.

Remaining acceptance includes real-terminal failure/quit-drain scenarios for these
dialogs, keybinding persistence and the other synchronous paths listed in the
effect contract. Historical-client and cross-process authority evidence remains
required. A long runner reflow can delay its heartbeat; reproduce that separately
with two disposable managers before changing leader-election semantics.

Final coordinator validation on 2026-10-01 passed `go test -race -p 1 ./...`
with isolated tmux state and `/bin/sh` (UI 235.000 seconds). Unchanged pure
packages used the valid test cache. Build, vet, formatting, whitespace, six
production dependency guards and seven harness contract tests passed. The final
actual-binary TUI smoke passed in 3.46 seconds against disposable state. The four
effect test files contain 60 source-adjacent regressions. These results prove the
local integration gates, not hosted CI or the deferred release matrix.

### Recovered Qwen effects and compatibility work

The coordinator recovered all four worktrees after the tmux cleanup incident.
Review normalization now dispatches with an explicit operation, re-reads current
state at execution, and propagates send preflight errors. Keys/Focus use captured
instance writers and foreground generations. Raw keys and paste retain their
synchronous ordering until a unified bounded input design is proved. Marker
consumption is serialized within one frontend, not atomic across processes.

The committed scenario suite adds actual-binary spawn/group/move/rename/settings/
fork behavior and accepted-write quit drain under a SQLite lock. CI shares one
binary build with smoke and retains both sets of artifacts. Compatibility
cleanup contracts use unrelated sentinel resources to catch scope escapes.
The standalone release and two-manager gates remain separate from the fast
suite; their runtime and exact coverage are documented in tools/compatibility.

Remaining acceptance includes real-terminal partial settings/file failure,
installation retries, newer/reopened dialog races while a worker is blocked,
real provider conversation capture, installed client extensions, mixed-version
concurrent mutations and in-flight delivery authority, and real SSH/platform
coverage. Passing heartbeat takeover is not proof of exclusive effect ownership.

Final recovered integration passed `go test -race -p 1 ./...` with isolated
tmux state and `/bin/sh` (UI 271.585 seconds; unchanged packages used valid
cached results). Build, vet, formatting, whitespace and six architecture guards
passed. Seven E2E and four cleanup contract tests passed. The final binary
smoke passed in 2.04 seconds and dialog/quit-drain scenarios in 7.25 seconds.
Checksummed v0.38.0/v0.39.0 release matrices and a copied native dev binary
passed disposable CLI/MCP and sequential task roundtrips. Two actual manager
processes passed heartbeat takeover and reclamation. Hosted CI must be checked
at the published head separately.
