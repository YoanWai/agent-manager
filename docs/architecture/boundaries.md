# Responsibilities and file organization

## Use domain terms consistently

| Term | Meaning |
| --- | --- |
| Execution profile | The authoritative session data and execution binding, including the tmux driver socket |
| Controller workspace | Saved connections, selected targets, and observations across execution profiles; a future application concern |
| UI workspace observations | The current `ui.workspace` snapshot state; not a saved connection catalog |
| Command service | Existing canonical `sessioncmd` use cases over explicitly bound dependencies |
| Execution authority | A future exclusive writer with verified cutover and fencing; not implied by a type named `SessionOwner` |

Session identity is distinct from its display label. A read projection is distinct from mutable store records. A connection generation rejects stale presentation results; it does not revoke an effect already accepted by an owner.

## Keep the current package boundaries visible

| Package | Responsibility | Current limits |
| --- | --- | --- |
| `internal/app` | Assemble local services and their resource lifetime | Local composition only |
| `internal/sessioncmd` | Canonical commands, actor policy, ordered lifecycle effects, and explicit runtime binding | Some legacy constructors remain; no production RPC boundary |
| `internal/execution` | Maintenance, delivery, status, capture, and copied observations | Existing socket claims do not fence competing process instances |
| `internal/ui` | Input, presentation, feature state, and local reconciliation | Root composes an ordered captured-effect lane; some preflight, dialog and focus families remain synchronous |
| `internal/cli` and `internal/mcpserver` | Translate frontend requests into canonical commands | Current MCP protocol modes are not historical-binary compatibility |

`config`, `store`, `tmux`, `hooks`, and `git` retain concrete infrastructure responsibilities. Introduce an interface where a consumer needs a narrow seam, not one repository abstraction per SQLite table.

Execution must not import UI or Bubble Tea. Frontend adapters must reuse canonical lifecycle behavior. Extensions consume copied observations or narrow command interfaces, never the root UI model. Wire and storage DTOs belong at their respective adapter boundaries.

The existing UI service group is a root composition convenience. Child features receive copied contexts and typed results rather than this service group. Root adapters execute their narrow requests. Introduce a consumer-declared effect interface only when a feature needs an actual runtime seam.

## Keep future workspace coordination separate

A controller workspace owns saved connections, local and remote inventory projections, hierarchy, and offline recovery. Namespace projected session identities by device and execution profile. Renaming a connection changes its label, not its target identity. A projected remote session is not a locally writable store entity.

Keep cached offline observations visibly stale. Separate inventory and preview reads from maintenance scheduling and mutations. Reconnect and refresh results carry connection generations and request sequences so an older result cannot replace a newer projection. A remote device MCP remains device-local; controller aggregation needs an explicitly workspace-scoped client.

These are future application rules. Current UI observations and PR #1 fixture workspace code do not constitute their production implementation.

## Extract feature packages from concern families

Keep root composition and adapters in `ui`. Move a feature into a child package when its context and outcomes are narrow enough to enforce ownership. Help, Review, Focus, and Rail now have private child models and [explicit package contracts](ui-feature-packages.md). Root adapters retain concrete effects and cross-feature navigation. Features that stay in `ui` are types with narrow hosts ([root feature types](ui-feature-packages.md#root-feature-types)); one moves to a child package when its dependencies meet the same conditions.

| Concern | File family | Acceptance condition |
| --- | --- | --- |
| Root coordination | `model`, `refresh`, `preview`, `startup`, `updates`, `sizing`, `rows` | Root routes messages; feature policy lives with its feature |
| Small dialogs | `help`, `fork`, `move`, `confirm` with keys and view files as needed | Each feature owns state and declares a narrow host |
| Review | Review state, diff keys, diff view, diff rendering, repo picker, editor | Preserve drafts, request generations, scroll, and input priority |
| Focus | Focus state, keys, selection, scroll, links, watches, and IME | Preserve keyboard, mouse, pane geometry, and attach behavior |
| Observations and rail | Observation state and rail state, keys, view | Name UI observations distinctly from future controller workspace coordination |

The [UI concern map](ui-file-map.md) records implemented file families and adjacent tests. The four child features own interaction policy through explicit value contracts. Root methods retain adapters, shared chrome, and unrelated dialogs. A file under 1,000 lines or a model with fewer fields can still hide broad dependencies. Size caps support review; they do not prove a boundary.

## Split other packages without adding layers

| Package | Proposed concerns |
| --- | --- |
| `sessioncmd` | Runtime, groups, spawn, messages, lifecycle, and terminal commands |
| `execution` | Runner, capture, delivery, mailbox application, status, notifications, and projections |
| `store` | Schema, sessions, groups, order, settings, and time encoding |
| `status` | Engine, matching, activity region, input, and turn state |
| `tmux` and `config` | Driver, launch, chrome, input, attach, pane queries; built-in config separate from loading |

The sessioncmd split now follows the [implemented file map](sessioncmd-file-map.md). The other package splits remain proposed.

Use mechanical declaration moves and compare declaration inventories before and after. Keep behavior changes in a separate commit. Keep tests beside sources and retain explicit exceptions for shared helpers and cross-package integration tests.

Keep the thin-wrapper product boundary and runtime discovery rules from [AGENTS.md](../../AGENTS.md). A saved-connection feature needs a UI-managed setting and an immediate application effect; a hand-edited per-user file is not the production configuration design.

Do not create empty domain, application, infrastructure, or workspace directories. Introduce a controller workspace package when a real saved-connection use case needs it.
