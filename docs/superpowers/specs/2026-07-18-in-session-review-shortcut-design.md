# In-session review shortcut (Ctrl+R)

## Problem

Review mode (the full-screen diff, `modeDiff`) is reachable only from the
session list, via `D`/`x` on the selected row. Once a user has attached into a
session's tmux pane (Enter → `attachSelected`), the only way to review that
session's diff is to detach with Ctrl+Q and press `D`. We want a single
keystroke from inside the pane that lands directly in that session's review.

## Approach

The manager (a Bubble Tea program) is not running inside the tmux pane, so the
only channel back to it is tmux itself. This mirrors the existing Ctrl+Q
"detach back to manager" binding installed by `installSessionUX`: a scoped,
server-global key that acts only inside `am_*` sessions and passes through
everywhere else.

Ctrl+R inside an `am_*` session sets a global tmux marker option and detaches.
The detach unblocks the manager's `tea.ExecProcess`, which delivers
`attachDoneMsg`. On that message the manager reads the marker; if set, it clears
it and opens the diff for the session it just attached (still the selected row,
since selection cannot change during a blocking attach).

## Changes

### tmux driver (`internal/tmux/tmux.go`)

- **Binding** in `installSessionUX`, mirroring the Ctrl+Q shape:
  ```
  bind-key -n C-r if-shell -F '#{m:am_*,#{session_name}}' \
    'set-option -g @am_review 1 ; detach-client' 'send-keys C-r'
  ```
  Inside `am_*` sessions Ctrl+R sets the marker and detaches; anywhere else it
  passes Ctrl+R through to the pane untouched.
- **Status hint**: `status-right` becomes
  ` agent-manager · Ctrl+Q = back · Ctrl+R = review `.
- **`ReviewRequested() (bool, error)`**: runs `show-option -gqv @am_review`,
  returns true when the value is `1`. `-q` keeps it quiet when unset; an absent
  server yields false, not an error (reuse the existing `noServer` handling).
- **`ClearReviewRequest() error`**: runs `set-option -gu @am_review` to unset
  the global option.

The marker is a global option rather than per-session because the manager only
needs to know "a review was requested"; it already knows which session it
attached.

### manager (`internal/ui/model.go`)

In the `attachDoneMsg` case, before the existing refresh:

- Call `m.tmux.ReviewRequested()`. On error, surface it (`m.err`) and fall
  through to current behavior.
- If true: `m.tmux.ClearReviewRequest()` (surface any error), then
  `return m, m.openDiff()` for the selected session. `openDiff` already guards
  the git driver and selection, sets `modeDiff`, and loads the diff.
- If false: current behavior (refresh).

No config schema change. No new mode.

## Edge cases

- **Ctrl+R with nothing selected**: `openDiff` already sets a "select a session
  to diff" error and stays in the list. Cannot happen from this path (we only
  arrive here after attaching a selected session) but is handled.
- **git not in PATH**: `openDiff` already surfaces "git not found in PATH".
- **Stale marker**: if `@am_review` were somehow set before an attach without
  Ctrl+R, the next detach would open review spuriously. `ClearReviewRequest`
  runs on every consume; the binding is the only setter. Low risk, self-healing
  on the next clear.
- **No tmux server**: `ReviewRequested` returns false via `noServer`, never
  errors the UI.

## Tests

- **`internal/tmux/tmux_test.go`**: create a session, set `@am_review 1`
  directly, assert `ReviewRequested()` is true, `ClearReviewRequest()` then
  makes it false. Assert `ReviewRequested()` on a clean marker is false.
- **`internal/ui/ui_test.go`**: the harness uses a real `*tmux.Driver` (tests
  skip when tmux is absent). Set `@am_review 1` via tmux, select a session, feed
  `attachDoneMsg{}` to `Update`, assert `m.mode == modeDiff`; with the marker
  cleared, assert it stays `modeList`. `gitDrv` is non-nil (built in `New`),
  so `openDiff` proceeds.

## Out of scope

Configurable keybinding, reviewing a session other than the attached one,
surfacing the shortcut anywhere but the status hint.
