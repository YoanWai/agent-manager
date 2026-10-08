# UI feature ownership

The root `internal/ui` package composes the application and the final terminal frame. Help, Review, Focus, and Rail own their interaction policies in child Go packages. Their models have private fields. Root adapters translate concrete store, Git, status, and tmux facts into feature values and execute feature-specific outcomes.

This is an incremental module boundary, not a new plugin protocol or a production execution authority. These in-process values do not introduce a wire schema or change the released-client compatibility policy.

## Keep each feature's contract narrow

| Package | Feature-owned state and policy | Root responsibilities |
| --- | --- | --- |
| [Help](../../internal/ui/help) | Catalog, scope, search, scroll, and body content | Modal navigation, command scheduling, dialog chrome |
| [Review](../../internal/ui/review) | Target, scope, files, request fences, comments, drafts, reviewed marks, navigation, and send policy | Git and filesystem reads, durable review writes, ordered send/rollback effects, highlighted diff painting, editor launch, cross-feature return route |
| [Focus](../../internal/ui/focus) | Key priority, selection, mouse forwarding policy, clipboard generation, scroll coalescing, pane facts, and prepared pane geometry | Watcher lifetime, tmux reads/writes, clipboard and browser operations, status classification, IME publication, mode changes |
| [Rail](../../internal/ui/rail) | Copied inventory, rows, stable selection, filtering, folding, menus, reorder policy, and prepared rail content | Concrete session records, persistence and lifecycle effects, navigation, generic chrome and final frame composition |
| Confirm (`confirmDialog` in root, host `confirmHost`) | Pending lifecycle target, y/n/esc keys, title, tone and wording; confirming hands the lifecycle lane a copied target | Lifecycle adapters fill the target and open the dialog; shared confirm card, mode changes, lifecycle effect lane, quit |
| Rename (`renameDialog`, `renameHost`) | Name, tool, path, worktree and base fields, focus, keys, name validation, the `renameRequest` it submits | Opening from the selected row, configured tools and group choices; shared path completion and base probe; `queueRename` adds the session row or path fallbacks and the dialog generation, runs the effect, applies the completion and mirrors the rename locally |
| Move (`moveDialog`, `moveHost`) | Captured session or group, picker keys, the same-parent no-op, the Rail mutation and its close follow-up | Opening rebuilds and shapes the spawn form's group picker it borrows; picker cursor and paint; `enqueueMove` runs the mutation and closes only the generation it captured |
| Fork (`forkDialog`, `forkHost`) | Source capture and validation on open, the name field, dialog generation, the `forkRequest` it submits | Selected row and tool configuration; `queueFork` mints the child id and pane size and admits one job per generation; effect completion and launch-error routing |
| Launch hint (`launchHintDialog`, `launchHintHost`) | Fix text, command and images, the pending install, keys, view, the `installStartRequest` and when its images move to it | `reportLaunchError` routes refused launches into it; composer images; `startInstall` admits one install with shell, group, pane and generation; settle polling; `syncMouseCapture` keeps `mouse.released` in root and releases the mouse while the dialog is open |

Each feature has concrete context and outcome types. There is no shared generic event bus or effect interpreter. A new request belongs beside the feature that needs it. Do not pass the root model or a bag of application services into a child.

The [production dependency checker](../../tools/architecture/check-ui-boundaries) follows transitive Go imports. It permits only each feature's explicit pure dependencies and rejects root UI, store, tmux, config, execution, and application packages. Review consumes [pure diff models and Git values](review-data.md); the concrete Git runner stays in its root adapter.

## Root feature types

Features that stay in package `ui` own their state, keys and view through a type that reaches the root only through a host interface it declares. `*Model` satisfies each host; the small host methods sit next to the feature. A host that would need the store, tmux or Git instead returns a typed request or exit that a root adapter runs.

| Type | Feature-owned state and policy | Host | Root responsibilities |
| --- | --- | --- | --- |
| Settings (`settingsFeature`) | Dialog state, cache, request generation and pending count; field stepping; CLI picker and its one-CLI rule; key picker capture, validation, reset and the lane-aware table diff; editor row and probe; the `settingsRequest` and `keysRequest` it captures; restoring the dialog from a partial save's read-back; dialog, CLI and key picker views | `settingsHost`: `reportErr`, `clearErr`, `toolConfig`, `keyTables`, `queuedEffects`, `submitEffect`, `syncPaneTheme`, `previewBackground`. `settingsViewHost`: `keyTables`, `release`, `dialogHeight`, `card`, `cardFlex`, `confirmCard` | Mode switch on open and close; `settingsExit` (save and close, in-place update, bug report link); live prefs mirror and reconcile; store and key writes on the effect lane; load completions for the dialog, form and quick bar |
| `noticesPanel` (`notices_state.go`, `notices.go`) | Notice list built from copied `noticeSources` (store presence, release info, key tables), cursor, body scroll, legend hit, keys and modal content | `size`, `currentMode`, `setMode`, `noticeSources`, `refreshNotices`, `startUpdate`, `dismissNotice`, `statusRow` | Release and feed checks, self-update, startup greeting and stored versions, optimistic dismissal on the effect lane, opening over the list or deferring it, backdrop placement |
| `repoPicker` (`review_picker.go`) | Rows snapshotted at open, filter, cursor, kind, source identity, keys and card body | `repoPickerHost`: `setMode`, `clearErr`, `reportErr`, `requestQuit`, `reviewPickerSourceCurrent`, `selectRepo`, `selectBase`; `repoPickerViewHost`: `size`, `cardWidth`, `card` | Branch and base reads as commands, stale-result fences, base save on the effect lane, review retarget |
| Editor values (`editor.go`) | `editorResolution` discovery order, `editorLaunch.takesScreen`, `editorReturnTarget.resumes`; no state of its own | None | PATH and file checks in commands, terminal handoff, status reports, reattach |
| `choice` (inside `form`, `quick`) | Profile, model and effort pick, model list filtering and scroll, restoring and keeping the CLI's last choice, recent models, row values | `choiceHost`: CLI flags, catalog answer, cached choice setting, save choice setting, report error | Catalog reads and refresh (`ensureCatalog`, `handleCatalog`), refitting open choices, ordered persistence (`effect_choice.go`) |
| `formDialog` (`form`) | New Session fields, focus, keys, clicks, view, validation and the `spawnRequest` | `formHost`: `choiceHost`, clear error, path completer, spawn defaults for a group | Opening, settings-load defaults, group picker rebuild and step, worktree probe and toggle, card chrome, `formRequest` execution, spawn effect |
| `groupFormDialog` (`groupForm`) | New Group fields, focus, keys, view, name validation and the `groupRequest` | `groupFormHost`: status bar, path completer, shared parent picker and its view, parent base, directory candidates | Opening, parent picker step, base step through Git, card chrome, `groupFormRequest` execution, group effect |
| `quickBar` (`quick`) | Prompt, tool, choice and pick list, keys, clicks, legend, bar view, validation and the send or spawn request | `quickHost`: `choiceHost`, clear error, selected row, spawn defaults for a group | Opening, settings-load defaults, cursor moves, worktree probe and toggle, footer layout, `quickRequest` execution, send and spawn effects |
| `pathComplete` (`pathSugg`) | Suggestions, highlight, request generation, stale-answer policy and dropdown view | `pathCompleteHost`: the value of the field a read answers, while it is open | Which dialog owns each target, applying a pick to that dialog's field |
| `composer` (inside `form`, `quick`) | Chips, paste reservation, chip keys, clipboard result and text paste policy | `composerHost`: report and clear error | Routing a clipboard message to its box and saying whether that box is still open |

Some `*Model` methods remain as one-line delegates because other features' files call them: `formTool`, `selectedGroupPath`, `viewGroupPicker`, `quickTool`, `quickSpawning`, `refreshChoicePrefs`, `requestPathSuggestions` and `viewPathSuggestions`.

## Preserve the interaction contracts

Review distinguishes load, file, status, probe, highlight, save, and send results. Its target and generation checks prevent stale replies from replacing current feature state. Closing a review invalidates presentation work while preserving process-lifetime draft and review caches. Durable review writes and send rollback retain their original ordering. A request fence does not revoke a mutation already accepted by a concrete adapter.

Focus receives copied watcher facts for the selected session. Its scroll policy coalesces repeated requests and fetches the latest requested region after an older capture finishes. Clipboard completion is bound to the standing selection generation. Selection, links, forwarded mouse cells, and IME coordinates use the prepared pane frame, including its visible text and clipping.

Rail reconciles copied inventory and keeps selection by session or group identity. Its typed persistence requests are applied through root adapters. Reorder policy applies a typed completion after ordered asynchronous persistence, before admitting another dependent move. This preserves completed-result policy without optimistic rollback state. Rendered rows and their hit geometry are produced together; root composition places that content without a second implementation of row layout.

## Prepare frames before reading them

The root prepares a complete frame at construction and after message dispatch. Preparation lays out child content, records hit geometry, clamps the terminal height, removes private cursor markers, and publishes the final IME anchor. `View()` returns the prepared string. Repeating `View()` does not update feature state, recompute layout, or publish cursor coordinates.

Input is interpreted against the preceding prepared frame. The next update then prepares the replacement frame. Tests that directly construct or mutate root fixtures explicitly prepare them before checking rendered output; they do not require production `View()` to mutate the fixture.

Read-only rendering does not make `Update()` nonblocking. The [ordered effect lane](ui-effects.md) now covers confirmed lifecycle, Rail persistence, geometry and attach preparation with durable partial reconciliation and blocked-adapter tests. Remaining synchronous families and wider acceptance stay on the [roadmap](roadmap-and-evidence.md).

## Verify policy separately from process wiring

Child tests construct feature values without creating a root model or invoking runtime adapters. Root integration tests cover input ordering and concrete caller translation. The [committed terminal smoke](../../tools/e2e/README.md) drives the actual binary through Help, Review, Focus, and Rail with disposable profiles and named sockets.

The smoke does not establish the complete supported-tool, terminal, platform, SSH, or historical-client matrix. Keep those release acceptance requirements explicit. Package placement and a passing dependency graph prove a module boundary; they do not establish exclusive writer authority or compatible rollout.

## Refresh against current upstream

The proposal incorporates upstream `cf9ed0d` through the migrated owners. Live directory tracking flows from tmux pane observations through the execution snapshot to root projections; Review captures the directory when its request opens. Coordination remains the upstream store setting, with consistent on-request/proactive launch, CLI and MCP behavior. Antigravity retains the final upstream profile, transcript capture, picker and chrome-free activity rules. No legacy root poller or session lifecycle monolith is recreated to carry these changes.
