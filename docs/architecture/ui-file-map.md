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

Other dialogs, settings, notices, and session adapters remain in root. The [ordered effect lane](ui-effects.md) runs confirmed lifecycle, Rail persistence, geometry, attach, spawn/fork/group creation, rename/move and settings persistence
outside Update; other synchronous families remain. A cached read-only `View` does not make every `Update` path nonblocking. The [roadmap](roadmap-and-evidence.md) retains those units explicitly.

## Frame and dispatch contracts

Construction and completed updates prepare the root frame. `View` reads that cached frame. Feature geometry is prepared before publication; the composed frame sets the final IME anchor and strips cursor markers. Regression tests exercise cached rendering, root adapters, exact geometry and IME placement.

The message router handles completions and timers before input, preserving resize capture, active mode, reorder, menus, search and quick-prompt priorities. Review requests freeze their target directory and generation. Focus and detail rendering receive the observed live pane directory; unavailable panes fall back to the launch directory.

## Tests and evidence

Run pure policy/content tests without root's tmux `TestMain`:

```sh
go test -race ./internal/ui/help ./internal/ui/review ./internal/ui/focus ./internal/ui/rail ./internal/ui/presentation ./internal/diff/model
go run ./tools/architecture/check-ui-boundaries
```

The transitive production dependency guard rejects runtime dependencies from feature packages. Root tests still require tmux; use `env -u TMUX`, `SHELL=/bin/sh`, a clean `ZDOTDIR`, and a short, precreated private `TMUX_TMPDIR`. Full integration checks remain required for shared routing and frame changes. Per-package timings are not a whole-suite speedup.

The committed [real TUI harness](../../tools/e2e/README.md) exercises Help, Review, Focus, Rail and a real managed shell against disposable state. CI runs it separately and retains artifacts. Released-client, wider terminal/tool/platform, headless/extension and SSH compatibility remain separate acceptance work.

## Current source inventory

There are 251 Go files under `internal/ui`: help (7), review (12), focus (7), rail (15), presentation (2), and 208 root files. Tests are source-adjacent; root fixtures retain cross-feature integration.

### Help package

`help/catalog.go`, `help/catalog_test.go`, `help/content.go`, `help/content_test.go`, `help/state.go`, `help/state_test.go`, `help/test_helpers_test.go`.

### Review package

`review/comments.go`, `review/comments_test.go`, `review/keys.go`, `review/loading.go`, `review/model.go`, `review/model_test.go`, `review/navigation.go`, `review/ownership_external_test.go`, `review/progress.go`, `review/results.go`, `review/text.go`, `review/types.go`.

### Focus package

`focus/key.go`, `focus/model.go`, `focus/model_test.go`, `focus/mouse.go`, `focus/mouse_test.go`, `focus/render.go`, `focus/scroll.go`.

### Rail package

`rail/drag_policy_test.go`, `rail/filter_policy_test.go`, `rail/frame.go`, `rail/interaction_types.go`, `rail/menu.go`, `rail/menu_policy_test.go`, `rail/menu_render.go`, `rail/model.go`, `rail/model_test.go`, `rail/mouse.go`, `rail/policy.go`, `rail/render.go`, `rail/reorder.go`, `rail/selection_effect_test.go`, `rail/window_test.go`.

### Presentation package

`presentation/text.go`, `presentation/text_test.go`.

### Root composition and adapters

`altscroll_test.go`, `async_effects_test.go`, `composer.go`, `composer_test.go`, `confirm.go`, `confirm_keys.go`, `confirm_keys_test.go`, `confirm_test.go`, `controlbytes.go`, `controlbytes_test.go`, `dialog_render.go`, `dialog_view.go`, `editor.go`, `editor_test.go`, `effect_attach.go`, `effect_executor.go`, `effect_fork.go`, `effect_fork_test.go`, `effect_geometry.go`, `effect_lifecycle.go`, `effect_lifetime_test.go`, `effect_rail.go`, `effect_rail_ordering_test.go`, `effect_rename.go`, `effect_rename_test.go`, `effect_settings.go`, `effect_settings_test.go`, `effect_spawn.go`, `effect_spawn_test.go`, `effects.go`, `focus_keys.go`, `focus_keys_test.go`, `focus_links.go`, `focus_links_test.go`, `focus_notification_integration_test.go`, `focus_scroll.go`, `focus_scroll_test.go`, `focus_selection.go`, `focus_selection_test.go`, `focus_state.go`, `focus_test_helpers_test.go`, `focus_view.go`, `focus_view_test.go`, `focus_watch.go`, `focus_watch_test.go`, `fork.go`, `fork_test.go`, `form.go`, `form_test.go`, `form_view.go`, `format.go`, `frame_integration_bench_test.go`, `frame_state_test.go`, `group_form_view.go`, `group_options.go`, `header_view.go`, `header_view_test.go`, `help_adapter.go`, `help_keys_test.go`, `help_test_helpers_test.go`, `help_view_test.go`, `helpers_test.go`, `ids.go`, `ime.go`, `ime_test.go`, `keys.go`, `launchhint.go`, `launchhint_test.go`, `launchhint_unix_test.go`, `layout.go`, `legend.go`, `legend_test.go`, `lifecycle_fixture_test.go`, `lifecycle_partial_test.go`, `lifecycle_watch_test.go`, `main_test.go`, `model.go`, `model_preferences.go`, `model_preferences_test.go`, `model_services.go`, `model_test.go`, `model_view.go`, `model_view_integration_test.go`, `model_view_test.go`, `mouse.go`, `mouse_guard_test.go`, `mouse_test.go`, `move.go`, `move_test.go`, `move_view.go`, `notices.go`, `notices_state.go`, `notices_test.go`, `notices_update.go`, `notices_update_test.go`, `observations.go`, `observations_state.go`, `observations_view.go`, `observations_view_test.go`, `paste_cleanup.go`, `paste_cleanup_test.go`, `pathcomplete.go`, `pathcomplete_test.go`, `poll_adapter.go`, `preview.go`, `preview_render.go`, `preview_render_test.go`, `preview_test.go`, `preview_view.go`, `preview_view_test.go`, `quick.go`, `quick_state.go`, `quick_test.go`, `quick_view.go`, `quick_view_test.go`, `rail_adapter.go`, `rail_drag_test.go`, `rail_filter_test.go`, `rail_footer_view.go`, `rail_footer_view_test.go`, `rail_group_view.go`, `rail_groups_test.go`, `rail_menu_test.go`, `rail_reorder_test.go`, `rail_row_view_test.go`, `rail_rows.go`, `rail_rows_test.go`, `rail_search_test.go`, `rail_selection_test.go`, `rail_terminal_integration_test.go`, `rail_test_support_test.go`, `rail_view.go`, `rail_view_test.go`, `refresh.go`, `refresh_integration_test.go`, `refresh_test.go`, `rename.go`, `rename_test.go`, `review_adapter.go`, `review_comments_test.go`, `review_integration_test.go`, `review_keys.go`, `review_keys_test.go`, `review_legacy_test_helpers_test.go`, `review_lifecycle.go`, `review_lifecycle_test.go`, `review_load.go`, `review_load_test.go`, `review_navigation.go`, `review_navigation_test.go`, `review_picker.go`, `review_picker_test.go`, `review_progress_test.go`, `review_store.go`, `review_store_test.go`, `review_view.go`, `review_view_test.go`, `session_archive.go`, `session_archive_test.go`, `session_attach.go`, `session_attach_test.go`, `session_delete.go`, `session_delete_test.go`, `session_directory_test.go`, `session_kill.go`, `session_kill_test.go`, `session_launch.go`, `session_launch_state.go`, `session_launch_test.go`, `session_lifecycle_integration_test.go`, `session_restart.go`, `session_restart_test.go`, `session_revive.go`, `session_revive_test.go`, `settings.go`, `settings_keys.go`, `settings_keys_test.go`, `settings_keys_view.go`, `settings_state.go`, `settings_test.go`, `settings_view.go`, `sizing.go`, `sizing_test.go`, `split.go`, `split_test.go`, `startup.go`, `startup_banner.go`, `startup_banner_test.go`, `startup_test.go`, `startup_view.go`, `startup_view_test.go`, `status_bar.go`, `status_view.go`, `status_view_test.go`, `styles.go`, `surface.go`, `terminal.go`, `terminal_altscroll.go`, `terminal_exec.go`, `terminal_exec_test.go`, `terminal_link_page.go`, `terminal_link_page_test.go`, `terminal_test.go`, `theme.go`, `theme_test.go`, `toast.go`, `toast_test.go`, `updates.go`.

## Historical mechanical migration proof

`go run ./tools/architecture/check-ui-moves 560a463 7773665` reproduces the frozen filename migration check: 2,371 declarations and 4,350 comment tokens. It intentionally excludes subsequent feature extraction and behavior changes. Those changes are validated by feature contracts, root integration tests, dependency checks and the process harness; the historical lexical proof is not a claim that the final semantic diff is unchanged.
