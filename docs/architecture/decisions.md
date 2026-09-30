# Architecture decisions

## Extend existing application services

The selected implementation binds existing `sessioncmd` use cases, shares lifecycle effects, extracts execution, and composes dependencies outside presentation. It gives CLI, MCP, TUI, and extensions the same substantive command implementation.

A remote-first command bus was considered. It would also require production deployment, compatibility, uncertain-outcome semantics, and old-writer cutover. Adding pass-through layers now would not establish those properties. A future transport must invoke the existing canonical use cases rather than implement another lifecycle.

## Apply DDD where policy differs

Strategic domain-driven design separates execution-profile authority from controller workspace coordination. Presentation features such as help and review are UI concerns, not automatically bounded contexts.

Use explicit dependency composition, application services, frontend adapters, narrow consumer-owned ports, and copied read projections. Avoid an aggregate hierarchy, a generic event bus, a plugin registry, or an interface for every table without a concrete use case.

Task claims, inbox claims, and reservation batches keep their established transactional rules. Moving files does not justify rewriting those policies.

## Preserve actor-specific lifecycle policy

Human archive captures before destructive effects and reconciles selection and subtree results. Session archive changes the archive flag while preserving a live pane and refusing self-archive. Sharing machinery must retain that distinction.

`Lifecycle` owns ordered durable and driver effects. Presentation owns selection, watches, layout, and visible reconciliation. Partial batch results identify completed effects when a later step fails. The UI must reconcile those completed effects even on error. Current delete does this; archive and restore can leave visible archive membership stale until a later poll.

A label failure after relaunch is a warning while status and acknowledgement work finishes. Restore exposes aggregated label warnings after durable success. Failed row creation cleans up hooks across frontends. A failed pane rollback preserves the persistence cause and identifies the surviving pane. Human archive of an already dead row preserves the existing skip of kill and hook cleanup.

These are explicit policy choices, not a claim that this branch consists only of mechanical moves.

## State resource ownership precisely

A `Backend` can own a lazily opened runtime or borrow one. Borrowed command operations do not close the caller's store. Owned operations reload configuration and key tables; a failed initial open can be retried. `Close` is not advertised as a concurrent barrier for in-flight standalone commands.

An execution runner owns mutable polling state. It keeps the latest result unless an unread error is pending. That error result takes priority until observed. Maintenance continues while an observer is suspended. Cancellation stops new passes and drains outstanding capture before the composition root closes the store.

Existing tmux subprocess calls have no context deadline. Cancellation therefore does not promise a fixed shutdown duration. Existing socket-based poller claims do not establish exclusive process authority.

## Separate file taxonomy from package ownership

Concern families group root coordination, observations, rail, review, focus, dialogs, settings, notices, and shared presentation. The [UI concern map](ui-file-map.md) records their files and tests. Existing declarations move mechanically before feature policy changes.

Feature subpackages were considered. They would need exported messages, shared render primitives, and root adapters before review and focus could move without import cycles or broad callback interfaces. The mechanical move therefore kept the flat package. It preserved the existing API and message priority before extracting a real feature boundary.

Help is now the first feature package. It consumes current presentation values and returns content and input outcomes. Root owns mode transitions, command scheduling, and generic dialog chrome. A small shared presentation package contains three reused pure text operations. Moving all dialog rendering, legends, and theme state was rejected for this increment because it would widen the cutover across unrelated features. A broad host exposing the store, driver, or all services would retain the coupling under a new name. See the [Help package contract](help-package.md).

## Keep UI effects and rendering explicit

The target remains the repository invariant that `Update` never blocks. I/O and subprocess work belong in `tea.Cmd`, with typed completion messages and request generations. Current synchronous lifecycle handlers are a documented conformance gap, even though their effects now use shared services.

Layout runs before painting. `View` reads prepared state and does not resize inputs, change scroll, or record geometry. The current implementation still performs some of these mutations while rendering. Moving geometry to a layout step must preserve mouse hit regions and frame identity.

Reject stale presentation replies after retarget or cancellation. Report completed or uncertain effects against their captured dispatch target. Dropping a stale reply does not cancel an effect already accepted by the execution authority.

## Keep test setup proportional to the behavior

Policy tests create the state their use case needs. SQLite-only task, reservation, and group policy tests use real rows and independent command connections without starting tmux panes. Group-to-spawn and pane-survival tests retain real panes, as do spawn, lifecycle, message, and terminal integration tests.

Preserve transactional races and failure-path assertions when optimizing fixtures. Measure uncached before and after runs with the same test selection and race settings. A smaller fixture is useful when it removes irrelevant setup while retaining the behavior under test. Prefer explicit synchronization to arbitrary sleeps when a test needs to observe an asynchronous event.
