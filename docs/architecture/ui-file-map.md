# UI concern map

The UI keeps one Go package because Bubble Tea runs one root model and existing features share input priority and layout state. Files are grouped by the behavior they support. Subdirectories would create separate Go packages, so introducing them requires real feature boundaries rather than moving files alone.

The mechanical migration preserves 2,371 declarations, 4,350 comment tokens, 1,045 exported names (including tests), both init functions, build constraints, and compiler directives. It relocates 1,410 declarations across 174 source/destination pairs. The help ownership change is separate. A concern-based filename identifies where code lives; it does not establish that every feature is independent of `Model`.

## Responsibility families

| Family | Responsibility |
| --- | --- |
| Root | Public construction, program lifecycle, message routing, and shared composition |
| Observations | Copied execution results and refresh adaptation; no saved connection catalog |
| Preview and sizing | Capture scheduling, stale-result checks, pane dimensions, and preview rendering |
| Rail | Tree rows, selection, filtering, search, drag, reorder, and row menus |
| Review | Review state, asynchronous loads, navigation, annotations, rendering, and repository picking |
| Focus | Pane state, key and mouse forwarding, selection, scrolling, links, watches, and IME |
| Dialogs | Help, confirm, fork, move, rename, forms, and launch guidance |
| Settings and notices | Settings state and pickers; message state, release metadata, input, and rendering |
| Shared presentation | Card chrome, styles, themes, legends, text composition, and terminal byte helpers |
| Session adapters | Existing launch, attach, archive, restore, restart, kill, and revive integration |

## Preserve dispatch priority

The root message router remains intact. Typed completions and timers are handled before key or mouse dispatch. Keyboard routing preserves resize capture, active modes, reorder, row menus, search, and quick-prompt priority. Mouse routing preserves focus handling, reorder, row menus, wheel events, and divider hit-testing.

Review loads retain request generations and captured targets. Focus retains its pane geometry and IME anchoring. The shared editor remains shared because rail, group, and review actions all call it.

## Feature ownership

Help owns search, scrolling, and input decisions. Its presentation context contains only the bindings and layout values it consumes. Root adapters supply that context and interpret navigation results. Shared card rendering consumes presentation values, so help does not duplicate the root dialog renderer. Help has no store, tmux driver, execution runner, or broad services dependency.

Other feature handlers retain root methods in this migration. Their file families expose the remaining coupling rather than claiming it has disappeared. Nonblocking effects, rendering without mutation, and extraction of larger feature hosts retain the acceptance requirements in the conformance audit.

## Verification boundaries

Mechanical checks compare declaration bodies and comments independently of file placement. They do not establish semantic equivalence without reviewing initialization notices and running behavior checks. The full race suite checks existing behavior under a disposable tmux socket and explicit shell environment. One unchanged tmux environment test can submit input before the relaunched shell is ready; its full package passed on a fresh socket. This UI change does not repair that test race. Disposable-profile frames exercise opening help, search, clearing, scrolling, closing, and starting a real terminal. These checks do not establish parity across every supported platform, tool, terminal, or SSH route.

Help goldens normalize ANSI, trailing whitespace, and outer blank rows to check readable content and layout. Separate real TUI captures check terminal frames. New help policy tests use copied bindings and values. Their fixtures do not open SQLite, create panes, or sleep. The existing package-wide `TestMain` still starts its shared tmux anchor. Real-pane tests remain integration tests. Timing claims identify the exact selection and include package harness overhead.

## Current source and test files

Tests use the source family prefix. Files containing `integration` retain contracts spanning several concerns; `helpers_test.go` retains shared fixtures.

### Root and shared presentation

`dialog_render.go`, `dialog_view.go`, `editor.go`, `editor_test.go`, `ime.go`, `ime_test.go`, `legend.go`, `legend_test.go`, `model.go`, `model_preferences.go`, `model_preferences_test.go`, `model_services.go`, `model_test.go`, `model_view.go`, `model_view_integration_test.go`, `model_view_test.go`, `mouse.go`, `mouse_guard_test.go`, `mouse_test.go`, `preview.go`, `preview_render.go`, `preview_render_test.go`, `preview_test.go`, `preview_view.go`, `preview_view_test.go`, `refresh.go`, `refresh_integration_test.go`, `refresh_test.go`, `sizing.go`, `sizing_test.go`, `startup.go`, `startup_banner.go`, `startup_banner_test.go`, `startup_test.go`, `startup_view.go`, `startup_view_test.go`, `styles.go`, `terminal.go`, `terminal_altscroll.go`, `terminal_exec.go`, `terminal_exec_test.go`, `terminal_link_page.go`, `terminal_link_page_test.go`, `terminal_test.go`, `theme.go`, `theme_test.go`, `updates.go`.

### Rail

`rail_drag.go`, `rail_drag_test.go`, `rail_filter.go`, `rail_filter_test.go`, `rail_footer_view.go`, `rail_footer_view_test.go`, `rail_group_view.go`, `rail_groups.go`, `rail_groups_test.go`, `rail_menu.go`, `rail_menu_test.go`, `rail_reorder.go`, `rail_reorder_test.go`, `rail_row_view.go`, `rail_row_view_test.go`, `rail_rows.go`, `rail_rows_test.go`, `rail_search.go`, `rail_search_test.go`, `rail_selection.go`, `rail_selection_test.go`, `rail_state.go`, `rail_terminal_integration_test.go`, `rail_view.go`, `rail_view_test.go`.

### Review

`review_comments.go`, `review_comments_test.go`, `review_integration_test.go`, `review_keys.go`, `review_keys_test.go`, `review_lifecycle.go`, `review_lifecycle_test.go`, `review_load.go`, `review_load_test.go`, `review_navigation.go`, `review_navigation_test.go`, `review_picker.go`, `review_picker_test.go`, `review_progress.go`, `review_progress_test.go`, `review_state.go`, `review_store.go`, `review_store_test.go`, `review_view.go`, `review_view_test.go`.

### Focus

`focus_keys.go`, `focus_keys_test.go`, `focus_links.go`, `focus_links_test.go`, `focus_notification_integration_test.go`, `focus_scroll.go`, `focus_scroll_test.go`, `focus_selection.go`, `focus_selection_test.go`, `focus_state.go`, `focus_view.go`, `focus_view_test.go`, `focus_watch.go`, `focus_watch_test.go`.

### Dialogs and feature settings

`confirm.go`, `confirm_keys.go`, `confirm_keys_test.go`, `confirm_test.go`, `fork.go`, `fork_test.go`, `form.go`, `form_test.go`, `form_view.go`, `format.go`, `group_form_view.go`, `help_catalog.go`, `help_catalog_test.go`, `help_keys.go`, `help_keys_test.go`, `help_state.go`, `help_test_helpers_test.go`, `help_view.go`, `help_view_test.go`, `helpers_test.go`, `move.go`, `move_test.go`, `move_view.go`, `notices.go`, `notices_state.go`, `notices_test.go`, `notices_update.go`, `notices_update_test.go`, `quick.go`, `quick_state.go`, `quick_test.go`, `quick_view.go`, `quick_view_test.go`, `rename.go`, `rename_test.go`, `settings.go`, `settings_keys.go`, `settings_keys_test.go`, `settings_keys_view.go`, `settings_state.go`, `settings_test.go`, `settings_view.go`.

### Session adapters

`session_archive.go`, `session_archive_test.go`, `session_attach.go`, `session_attach_test.go`, `session_delete.go`, `session_delete_test.go`, `session_kill.go`, `session_kill_test.go`, `session_launch.go`, `session_launch_state.go`, `session_launch_test.go`, `session_lifecycle_integration_test.go`, `session_restart.go`, `session_restart_test.go`, `session_revive.go`, `session_revive_test.go`.

### Other retained families

`altscroll_test.go`, `composer.go`, `composer_test.go`, `controlbytes.go`, `controlbytes_test.go`, `frame_integration_bench_test.go`, `header_view.go`, `header_view_test.go`, `ids.go`, `keys.go`, `launchhint.go`, `launchhint_test.go`, `launchhint_unix_test.go`, `layout.go`, `main_test.go`, `observations.go`, `observations_state.go`, `observations_view.go`, `observations_view_test.go`, `paste_cleanup.go`, `paste_cleanup_test.go`, `pathcomplete.go`, `pathcomplete_test.go`, `poll_adapter.go`, `split.go`, `split_test.go`, `status_bar.go`, `status_view.go`, `status_view_test.go`, `surface.go`, `toast.go`, `toast_test.go`.

## Reproduce the mechanical check

Run `go run ./tools/architecture/check-ui-moves 560a463 7773665` from the repository root. `-map-out PATH` before the refs writes an exhaustive declaration map. This check intentionally excludes the subsequent Help ownership change from its comparison.

The comparator reports lexical variable initializer changes for source review. The Chroma style and formatter retain their relative order but move after independent regular-expression and terminal-byte constructors. In pinned Chroma v2.27.0, style building creates local values and registry lookups only read initialized registries. Theme initialization does not mutate these constructors. This is reviewed evidence, not a general guarantee that initializer relocation is safe.

`altscroll_test.go` retains its old filename because a source scanner explicitly recognizes it. Shared fixtures remain in `helpers_test.go`; integration tests retain multiple-concern scenarios rather than forcing artificial ownership.

## Focused Help gate

The five new ownership, adapter, and baseline-layout tests pass in an uncached race run in 2.149 seconds on local macOS, including the package tmux anchor. This is a focused gate, not an overall suite speedup. Run it with:

```sh
env -u TMUX SHELL=/bin/sh ZDOTDIR=/tmp/am-clean-shell TMUX_TMPDIR=/tmp/am-help-policy go test -race ./internal/ui -run '^(TestHelpStateOwnsKeyboardSearchAndScroll|TestHelpStateOwnsQuitWithoutClosing|TestHelpAdapterPreservesQuitCommand|TestHelpAdapterRestoresReviewBeforeRestartingLoader|TestHelpMatchesBaselineContentAndLayout)$' -count=1
```

Create the two temporary directories first. Use a fresh tmux directory when running this gate beside other tests. The full race suite remains required for shared routing and rendering changes.
