# Application architecture

PR #2 implements a local application boundary refactor. It is partially conformant to the broader architecture proposed in PR #1 and issue #646. Shared lifecycle and UI-free maintenance are implemented. Help, Review, Focus, and Rail have feature-owned packages, and rendering reads a prepared frame. The ordered lane covers lifecycle, dialog writes, Review mutations and Focus preparation. The follow-up moves picker/path/preflight, raw input and installation work out of Update, and adds scoped automatic-delivery authority. Selected-session quick sends, Review preference/picker reads and base saves, notice/split persistence, and editor discovery now follow the same captured-command contracts. Initialization reads stay before the event loop; the bounded acceptance evidence is tracked separately. Remote workspace coordination, cross-client human pane authority and wider platform/provider acceptance remain incomplete.

These documents retain the broader target. A requirement described here is not evidence that its implementation exists.

## Read the contract and its evidence

| Document | Purpose |
| --- | --- |
| [Boundaries](boundaries.md) | Responsibilities, vocabulary, dependencies, and file organization |
| [Decisions](decisions.md) | Selected patterns, alternatives, and lifecycle policies |
| [Compatibility and rollout](compatibility-and-rollout.md) | Mixed versions, scope, uncertain outcomes, and old-writer cutover |
| [Roadmap and evidence](roadmap-and-evidence.md) | Current conformance, source references, and bounded follow-ups |
| [Production proposal](../architecture-proposal.md) | Summary of the implemented refactor and runnable examples |

The [UI feature package contracts](ui-feature-packages.md) record Help, Review, Focus, and Rail ownership and root adapter responsibilities. The [Help feature package](help-package.md) retains its detailed first-extraction contract. The [review data boundary](review-data.md) separates pure line models and Git values from concrete subprocess adapters. The [real-terminal harness](../../tools/e2e/README.md) makes the local smoke reproducible.

The [status file map](status-file-map.md) records the classification, transcript, region and composer split.

The [session command file map](sessioncmd-file-map.md) records the command split. The [UI concern map](ui-file-map.md) records the UI taxonomy and separates file placement from feature ownership.

## Keep the historical evidence separate

[PR #1](https://github.com/ribeirojose/agent-manager/pull/1) is closed without merging. Its experiments remain available at commit `21294a3d895790e8c1937108b5c900eb43a2c2d3`. PR #2 supersedes it as the production-code proposal, not as proof that all experimental features shipped.

The maintained contract reconciles these original records:

| Historical record | What carries forward |
| --- | --- |
| [Application architecture](https://github.com/ribeirojose/agent-manager/blob/21294a3d895790e8c1937108b5c900eb43a2c2d3/docs/architecture-poc/application-architecture.md) | Workspace versus execution-profile authority, narrow ports, projections, and scope |
| [Scope and migration](https://github.com/ribeirojose/agent-manager/blob/21294a3d895790e8c1937108b5c900eb43a2c2d3/docs/architecture-poc/scope-and-migration.md) | Incremental migration through existing use cases and preservation of UI behavior |
| [Independent review](https://github.com/ribeirojose/agent-manager/blob/21294a3d895790e8c1937108b5c900eb43a2c2d3/docs/architecture-poc/application-review.md) | Five version domains, stale replies, uncertain mutations, and legacy-writer risks |
| [Migration design](https://github.com/ribeirojose/agent-manager/blob/21294a3d895790e8c1937108b5c900eb43a2c2d3/docs/architecture-poc/migration-design.md) | Archive and inbox extraction with explicit actor policies and bounded ownership claims |
| [Coverage](https://github.com/ribeirojose/agent-manager/blob/21294a3d895790e8c1937108b5c900eb43a2c2d3/docs/architecture-poc/coverage.md) | Separation of fixture evidence, real-process evidence, and production acceptance |

File organization follows the maintainability direction of [issue #646](https://github.com/YoanWai/agent-manager/issues/646). Smaller files and feature types help reviewers. Neither establishes authority, compatibility, or nonblocking behavior by itself.

[Ordered UI effects](ui-effects.md) records captured requests, partial reconciliation, FIFO execution, shutdown and remaining synchronous paths.

[In-flight delivery](in-flight-delivery.md) records the reproduced claim-retirement gap and the single-manager pilot boundary.

[Automatic delivery ownership](delivery-ownership.md) records guarded admission, bounded transport, durable uncertainty and the required offline legacy cutover. [Upstream adoption](adoption.md) describes independently reviewable migration stages.
