# UI concern map

The root `ui` package composes the Bubble Tea application. `help`, `review`, `focus`, and `rail` are separate Go packages with private feature models; `presentation` contains shared pure text operations. These are behavior boundaries, not filename-only categories. The [feature contracts](ui-feature-packages.md) describe copied inputs, typed outcomes, and root effects.

## Ownership

| Package | Feature responsibility | Root integration |
| --- | --- | --- |
| Help | Catalog, scope, search, scroll, body content | Dialog frame, navigation, final IME anchor |
| Review | Targets, generations, file selection, drafts, annotations, navigation, scroll and send policy | Git loading, ordered persistence/send/rollback, highlighted painting, cross-feature return routes |
| Focus | Prepared pane geometry, selection, scrolling and input decisions | tmux capture/input/resize, watches, clipboard, URLs, paste and final IME anchor |
| Rail | Tree inventory, filtering, search, selection, drag, reorder, menus, row rendering and prepared hit geometry | Store mutations, session actions, quick prompt composition, preview scheduling and footer/detail composition |
| Presentation | Shared text, chrome and terminal operations | Application composition |
| Settings (root type `settingsFeature`) | Dialog, CLI and key pickers, editor row, cache, generations, captured `settingsRequest`/`keysRequest`, partial-save dialog restore and the three views (`settings*.go`) | Mode switch, `settingsExit` (save and close, in-place update, bug report), live prefs mirror and reconcile, store and key writes on the effect lane (`settings_effect.go`, `settings_keys_effect.go`) |
| Confirm (`confirmDialog`; `confirm.go`, `confirm_keys.go`) | Pending lifecycle target, y/n keys, title, tone and wording, the copied target it confirms | Lifecycle adapters fill and open it, shared confirm card, lifecycle effect lane, quit |
| Rename (`renameDialog`; `rename.go`, `rename_effect.go`) | Fields, focus, keys, tool cycling, name validation, the rename request | Opening from the row, config and group choices; path completion and base probe; inventory facts and generation on the request; effect lane, completion, local mirror |
| Move (`moveDialog`; `move.go`, `move_view.go`) | Captured row, picker keys, same-parent no-op, the Rail mutation and close follow-up | Opening and shaping the spawn form's group picker, picker cursor and paint, effect lane, fenced close |
| Fork (`forkDialog`; `fork.go`, `fork_effect.go`) | Source capture and validation, name field, dialog generation, the fork request | Selected row and tool config; child id, pane and duplicate admission; effect lane, completion, launch-error routing |
| Launch hint (`launchHintDialog`; `launchhint.go`, `launchhint_install_effect.go`) | Fix, pending install, keys, view, the install request and its image handoff | Launch-error routing, composer images, install admission and settle polling, `mouse.released` while open |

Root files are named `<feature>_<concern>.go`: a feature's state, keys, view, effect and tests share its prefix, so `settings_*` is all of Settings and `spawn_*` all of New Session. Only `keys.go`, `mouse.go`, `updates.go` and `ids.go` route instead of owning a feature. [check-file-names](../../tools/architecture/check-file-names) rejects a root source without a known feature and a test not named after its source. The [ordered effect lane](ui-effects.md) runs confirmed lifecycle, Rail persistence, geometry, attach, spawn/fork/group creation, rename/move and settings persistence
outside Update; captured input, installer, preference and editor adapters also follow that boundary. A cached read-only `View` does not make every `Update` path nonblocking. The [roadmap](roadmap-and-evidence.md) retains those units explicitly.

The New Session form (`spawn_form.go`, `spawn_form_view.go`), the New Group form (`group_form.go`, `group_form_view.go`), the quick bar (`quick.go`, `quick_state.go`, `quick_view.go`), path completion (`pathcomplete.go`), prompt composers (`composer.go`) and the model, effort and profile choice (`spawn_choice.go`) are [root feature types](ui-feature-packages.md#root-feature-types) with narrow hosts. Their requests run through root adapters in the same files and on the effect lane (`spawn_effect.go`, `quick_effect.go`, `spawn_choice_effect.go`); `spawn_preflight.go` supplies spawn defaults and `spawn_worktree.go` the worktree probe.

The notices panel (`noticesPanel`) and the review repo picker (`repoPicker`) are root feature types with their own host interfaces; editor launch and return policy lives on its captured values. The [feature contracts](ui-feature-packages.md#root-feature-types) list their hosts.

## Frame and dispatch contracts

Construction and completed updates prepare the root frame. `View` reads that cached frame. Feature geometry is prepared before publication; the composed frame sets the final IME anchor and strips cursor markers. Regression tests exercise cached rendering, root adapters, exact geometry and IME placement.

The message router handles completions and timers before input, preserving resize capture, active mode, reorder, menus, search and quick-prompt priorities. `handleMsg` in `updates.go` only routes: each message family goes to a `route…Msg` method in the file that owns it (effect completions `effects.go`, spawn reads `spawn_form.go`, settings, catalog, editor probe and base reads `settings.go`, resize `geometry.go`, startup and banner timers `startup.go`, preview `preview.go`, poll refresh `workspace_refresh.go`, release and feed notices `notices_update.go`, paste `composer.go`, focus and links `focus_state.go`, attach and reply copy `session_attach.go`, Review `review_adapter.go`, editor `session_editor.go`, errors `status_bar.go`); mouse, Rail auto-scroll and keys reach their dispatchers last. Each route reports whether it handled the message; there is no registry. `TestHandleMsgLineCap` holds the router under 60 lines. Review requests freeze their target directory and generation. Focus and detail rendering receive the observed live pane directory; unavailable panes fall back to the launch directory.

## Tests and evidence

Run pure policy/content tests without root's tmux `TestMain`:

```sh
go test -race ./internal/ui/help ./internal/ui/review ./internal/ui/focus ./internal/ui/rail ./internal/ui/presentation ./internal/diff/model
go run ./tools/architecture/check-ui-boundaries
```

The transitive production dependency guard rejects runtime dependencies from feature packages. Root tests still require tmux; use `env -u TMUX`, `SHELL=/bin/sh`, a clean `ZDOTDIR`, and a short, precreated private `TMUX_TMPDIR`. Full integration checks remain required for shared routing and frame changes. Per-package timings are not a whole-suite speedup.

The committed [real TUI harness](../../tools/e2e/README.md) exercises Help, Review, Focus, Rail and a real managed shell against disposable state. CI runs it separately and retains artifacts. Released-client, wider terminal/tool/platform, headless/extension and SSH compatibility remain separate acceptance work.

## Current source inventory

There are 297 Go files under `internal/ui`: help (7), review (13), focus (7), rail (15), presentation (3), and 252 root files. Tests are source-adjacent; root fixtures retain cross-feature integration.

### Help package

`help/catalog.go`, `help/catalog_test.go`, `help/content.go`, `help/content_test.go`, `help/state.go`, `help/state_test.go`, `help/test_helpers_test.go`.

### Review package

`review/comments.go`, `review/comments_subject_test.go`, `review/comments_test.go`, `review/keys.go`, `review/loading.go`, `review/model.go`, `review/model_ownership_external_test.go`, `review/model_test.go`, `review/navigation.go`, `review/progress.go`, `review/results.go`, `review/text.go`, `review/types.go`.

### Focus package

`focus/key.go`, `focus/model.go`, `focus/model_test.go`, `focus/mouse.go`, `focus/mouse_test.go`, `focus/render.go`, `focus/scroll.go`.

### Rail package

`rail/frame.go`, `rail/interaction_types.go`, `rail/menu.go`, `rail/menu_policy_test.go`, `rail/menu_render.go`, `rail/model.go`, `rail/model_test.go`, `rail/mouse.go`, `rail/policy.go`, `rail/policy_filter_test.go`, `rail/policy_selection_effect_test.go`, `rail/render.go`, `rail/render_window_test.go`, `rail/reorder.go`, `rail/reorder_drag_test.go`.

### Presentation package

`presentation/controls.go`, `presentation/text.go`, `presentation/text_test.go`.

### Root features

- **composer** (prompt composers, chips and pasted images): `composer.go`, `composer_paste_cleanup.go`, `composer_paste_cleanup_test.go`, `composer_test.go`.
- **confirm** (the confirm dialog): `confirm.go`, `confirm_keys.go`, `confirm_keys_test.go`, `confirm_test.go`.
- **effects** (the ordered effect lane and its executor): `effects.go`, `effects_async_test.go`, `effects_executor.go`, `effects_lifetime_test.go`.
- **focus** (the Focus adapter and pane watch): `focus_effect.go`, `focus_effect_test.go`, `focus_keys.go`, `focus_keys_test.go`, `focus_links.go`, `focus_links_test.go`, `focus_scroll.go`, `focus_scroll_test.go`, `focus_selection.go`, `focus_selection_test.go`, `focus_state.go`, `focus_test_helpers_test.go`, `focus_view.go`, `focus_view_test.go`, `focus_watch.go`, `focus_watch_lifecycle_test.go`, `focus_watch_test.go`, `focus_watch_unix_test.go`.
- **fork** (the fork dialog): `fork.go`, `fork_effect.go`, `fork_effect_test.go`, `fork_test.go`.
- **frame** (frame composition: dialog chrome, header, legend, IME, text layout and control escaping): `frame.go`, `frame_dialog.go`, `frame_dialog_render.go`, `frame_escape.go`, `frame_escape_test.go`, `frame_format.go`, `frame_header.go`, `frame_header_test.go`, `frame_ime.go`, `frame_ime_test.go`, `frame_integration_bench_test.go`, `frame_integration_test.go`, `frame_legend.go`, `frame_legend_test.go`, `frame_state_test.go`, `frame_test.go`.
- **geometry** (pane sizing, layout widths and the split ratio): `geometry.go`, `geometry_effect.go`, `geometry_layout.go`, `geometry_split.go`, `geometry_split_effect.go`, `geometry_split_test.go`, `geometry_test.go`.
- **group** (the New Group form): `group_form.go`, `group_form_test.go`, `group_form_view.go`.
- **help** (the Help adapter): `help_adapter.go`, `help_adapter_keys_test.go`, `help_adapter_view_test.go`, `help_test_helpers_test.go`.
- **input** (raw input forwarded to panes): `input_effect.go`, `input_effect_test.go`.
- **launchhint** (the launch fix dialog and CLI install): `launchhint.go`, `launchhint_install_effect.go`, `launchhint_install_effect_test.go`, `launchhint_test.go`, `launchhint_unix_test.go`.
- **model** (the root Model, its services and shared host methods): `model.go`, `model_host.go`, `model_services.go`, `model_test.go`.
- **move** (the move dialog): `move.go`, `move_test.go`, `move_view.go`.
- **notices** (the notices panel and release feed): `notices.go`, `notices_body.go`, `notices_body_test.go`, `notices_effect.go`, `notices_effect_test.go`, `notices_state.go`, `notices_state_test.go`, `notices_test.go`, `notices_text.go`, `notices_text_test.go`, `notices_update.go`, `notices_update_test.go`.
- **pathcomplete** (directory completion for path fields): `pathcomplete.go`, `pathcomplete_test.go`.
- **preview** (the session preview): `preview.go`, `preview_render.go`, `preview_render_test.go`, `preview_test.go`, `preview_view.go`, `preview_view_test.go`.
- **quick** (the quick bar): `quick.go`, `quick_effect.go`, `quick_effect_test.go`, `quick_state.go`, `quick_test.go`, `quick_view.go`, `quick_view_test.go`.
- **rail** (the Rail adapter): `rail_adapter.go`, `rail_adapter_drag_test.go`, `rail_adapter_groups_test.go`, `rail_adapter_menu_test.go`, `rail_adapter_reorder_test.go`, `rail_adapter_selection_test.go`, `rail_effect.go`, `rail_effect_ordering_test.go`, `rail_footer_view.go`, `rail_footer_view_test.go`, `rail_group_view.go`, `rail_helpers_test.go`, `rail_rows.go`, `rail_rows_filter_test.go`, `rail_rows_test.go`, `rail_view.go`, `rail_view_rows_test.go`, `rail_view_search_test.go`, `rail_view_terminal_test.go`, `rail_view_test.go`.
- **rename** (the rename dialog): `rename.go`, `rename_effect.go`, `rename_effect_test.go`, `rename_test.go`.
- **review** (the Review adapter and repo picker): `review_adapter.go`, `review_effect.go`, `review_effect_test.go`, `review_keys.go`, `review_keys_test.go`, `review_legacy_test_helpers_test.go`, `review_lifecycle.go`, `review_lifecycle_preflight_test.go`, `review_lifecycle_test.go`, `review_load.go`, `review_load_integration_test.go`, `review_load_test.go`, `review_navigation.go`, `review_navigation_test.go`, `review_picker.go`, `review_picker_async_test.go`, `review_picker_load.go`, `review_picker_test.go`, `review_store.go`, `review_store_comments_test.go`, `review_store_progress_test.go`, `review_store_test.go`, `review_view.go`, `review_view_test.go`.
- **session** (session lifecycle: launch, attach, kill, revive, archive, delete, restart, editor): `session_after_turn.go`, `session_after_turn_test.go`, `session_archive.go`, `session_archive_test.go`, `session_attach.go`, `session_attach_effect.go`, `session_attach_effect_test.go`, `session_attach_test.go`, `session_delete.go`, `session_delete_test.go`, `session_editor.go`, `session_editor_test.go`, `session_kill.go`, `session_kill_test.go`, `session_launch.go`, `session_launch_state.go`, `session_launch_test.go`, `session_lifecycle_effect.go`, `session_lifecycle_effect_partial_test.go`, `session_lifecycle_helpers_test.go`, `session_lifecycle_integration_test.go`, `session_restart.go`, `session_restart_test.go`, `session_revive.go`, `session_revive_test.go`.
- **settings** (the Settings dialog, key bindings and editor row): `settings.go`, `settings_clis.go`, `settings_editor.go`, `settings_editor_test.go`, `settings_effect.go`, `settings_effect_test.go`, `settings_keys.go`, `settings_keys_effect.go`, `settings_keys_effect_test.go`, `settings_keys_test.go`, `settings_keys_view.go`, `settings_state.go`, `settings_state_test.go`, `settings_test.go`, `settings_view.go`.
- **spawn** (the New Session form, model choice, spawn preflight and worktree probe): `spawn_choice.go`, `spawn_choice_effect.go`, `spawn_choice_effect_test.go`, `spawn_choice_test.go`, `spawn_effect.go`, `spawn_effect_test.go`, `spawn_form.go`, `spawn_form_groups.go`, `spawn_form_test.go`, `spawn_form_view.go`, `spawn_preflight.go`, `spawn_worktree.go`.
- **startup** (startup greeting and banner): `startup.go`, `startup_banner.go`, `startup_banner_test.go`, `startup_test.go`, `startup_view.go`, `startup_view_test.go`.
- **status** (the status bar and toasts): `status_bar.go`, `status_bar_test.go`, `status_toast.go`, `status_toast_test.go`, `status_view.go`, `status_view_test.go`.
- **terminal** (terminal handoff and link pages): `terminal.go`, `terminal_altscroll.go`, `terminal_altscroll_test.go`, `terminal_directory_test.go`, `terminal_exec.go`, `terminal_exec_test.go`, `terminal_link_page.go`, `terminal_link_page_test.go`, `terminal_test.go`.
- **theme** (themes, styles, surfaces and the stored theme): `theme.go`, `theme_prefs.go`, `theme_prefs_test.go`, `theme_styles.go`, `theme_surface.go`, `theme_surface_backdrop_test.go`, `theme_test.go`.
- **workspace** (polling, refresh and what the last poll returned): `workspace.go`, `workspace_poll.go`, `workspace_refresh.go`, `workspace_refresh_integration_test.go`, `workspace_refresh_notification_test.go`, `workspace_refresh_reorder_test.go`, `workspace_refresh_test.go`, `workspace_stats.go`, `workspace_stats_view.go`, `workspace_stats_view_test.go`.
- **dispatch and shared test support**: `helpers_test.go`, `ids.go`, `keys.go`, `main_test.go`, `mouse.go`, `mouse_guard_test.go`, `mouse_test.go`, `updates.go`, `updates_test.go`.

## Historical mechanical migration proof

`go run ./tools/architecture/check-ui-moves 560a463 7773665` reproduces the frozen filename migration check: 2,371 declarations and 4,350 comment tokens. It intentionally excludes subsequent feature extraction and behavior changes. Those changes are validated by feature contracts, root integration tests, dependency checks and the process harness; the historical lexical proof is not a claim that the final semantic diff is unchanged.

The root effect adapters now include `settings_keys_effect.go`, `focus_effect.go`, and
`review_effect.go`, with matching source-adjacent tests. They compose persisted
runtime services with the pure feature models; the child packages do not acquire
SQLite or tmux dependencies.
