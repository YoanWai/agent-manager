# Steps 6 and 7: the list's own overlays, focus, and the end of `mode`

Read `docs/adr/0001-surface-stack.md` first, then `docs/adr/0001-migration-brief.md`
for the shape of a surface and the local gate. This brief is for one session
doing the two sequential steps; everything here touches `keys.go`, `mouse.go`,
`frame.go`, `frame_footer.go` and `model.go`, which is why it is not parallel.

Base: `fork/refactor/quit-router` at `103de18` or later. Eleven cards are
surfaces already; `mode` has four values left: `modeList`, `modeForm`,
`modeDiff`, `modeFocus`. Reads of `m.mode` in non-test code: 81. Reads of
overlay flags (`quick.active`, `rail.Searching()`, `rail.MenuOpen()`,
`rail.Reordering()`, `rail.InputMode()`, `split.resizeMode`,
`split.dragging`): 55. Six `switch m.mode` statements remain.

## What is and is not a surface (decided; do not relitigate)

Three different things hide under "overlay flag". Treat them differently.

1. **Cards over the list are surfaces.** The quick bar (`quickBar`,
   `quick_adapter.go`) and the spawn form (`formDialog`, `modeForm`,
   `spawn_adapter.go`). They take keys, paint over the frame, hold a draft.
   Same shape as the eleven cards already done: `quick_surface.go`,
   `spawn_surface.go`, one constructor each, state moves off `Model`
   (`Model.quick`, `Model.form` go), completions fence with `surfaceOf`.
   `Dismissible()` is false for both: each holds typed text. `Owner()` is
   `ownerManager`.

2. **Search, the row menu and reorder are modes of the rail, not
   surfaces.** `rail.Model` owns their state and `rail.Key` already
   dispatches on `InputMode()`. Pulling that state into root surfaces would
   be a regression in colocation. Instead the rail answers one question:
   add `func (m Model) Busy() bool { return m.InputMode() != BrowseMode }`
   (or a better name) and collapse the root's reads of the four methods to
   it where they mean "the rail owns the keyboard". The footer's
   `reorderFooter` and the mouse's menu hover stay as they are: they are
   not routing.

3. **Resize is a keyboard grab, not a screen.** `split.resizeMode` and
   `split.dragging` are pointer-gesture state in `geometry_split.go` and
   `mouse.go`. Leave the paint and the mouse handling alone. Move only the
   key routing in `keys.go` (lines ~66–98: the drag-arm block and the
   resize-mode switch) into a `resizeSurface` in `geometry_surface.go`
   that `enterResizeMode` pushes and `exitResizeMode` pops. Its `View`
   returns `below` unchanged; `Footer` returns the resize legend that
   `frame_footer.go` builds today; `Dismissible()` false (esc cancels via
   its own Key). The two `split` booleans stay for the mouse; the
   `TestCtrlCIsDecidedInKeysGo` lint still passes because the surface
   never compares ctrl+c.

## Step 6, in order, one commit each

1. `refactor(rail): one answer for whether the rail owns the keyboard` —
   `Busy()`; replace the twelve root reads that mean that.
2. `refactor(geometry): resize is a surface` — as above.
3. `refactor(quick): the quick bar is a surface` — `Model.quick` goes;
   `closeQuickOffTheList` (mouse.go) becomes a pop; the `quick.active &&
   gen` fences in `settings_prompts.go`, `settings_effect.go`,
   `quick_effect.go`, `spawn_effect.go`, `spawn_worktree.go`,
   `spawn_catalog.go`, `spawn_defaults.go`, `launchhint_install_effect.go`,
   `focus_effect.go` use `surfaceOf[*quickSurface]`. `composer.go:613`
   asks the stack.
4. `refactor(spawn): the new-session form is a surface` — `Model.form`
   goes; `modeForm` goes; same fences; `pathcomplete.go` loses its last
   non-stack case via `pathCompleting`; the launch hint opens over the
   form (already a surface, so `TestLaunchHintOverTheFormLeavesItsModeAlone`
   just drops its mode read); the group picker the group form borrows from
   `m.form` moves to the spawn surface and the group form reaches it by
   message as it does today.

After step 6: `mode` has three values, `switch m.mode` is gone from
`keys.go`, `mouse.go` (clickCard/wheelCard), `pathcomplete.go`.

## Step 7, in order, one commit each

5. `refactor(focus): focus is a surface over the list` — `focusSurface` in
   `focus_surface.go` with `Owner() = ownerAgent`, `Dismissible() = false`;
   `enterFocus` pushes, `leaveFocus`/`leaveFocusMode` pop; `handleFocusKey`
   becomes its `Key`; `focusMouse` its `Click`/`Wheel` (routeMouse asks the
   stack; see note). `inputOwner` and `baseOwner` in `model_surface.go`
   lose their `modeFocus` branch; `routeQuit` needs no change. The saved
   prompts list already stacks over it. `frame_ime.go`, `frame_footer.go`,
   `syncMouseCapture` read `agentOwnsKeyboard()` / `surfaceOf`.
   Mouse note: today `routeMouse` has `case m.mode == modeFocus: return
   m.focusMouse(msg)` and `cardMouse` only wheels inside the card rect.
   Focus is not a card: give `surface` nothing new; instead `routeMouse`
   asks the top surface first via a small optional interface
   (`mouseTaker interface { Mouse(h surfaceHost, msg tea.MouseMsg) tea.Cmd }`),
   the way `columnClicker` and `pathCompleting` are optional today.
   `cardOpen()` returns false for a surface implementing `mouseTaker`.
6. `refactor(ui): two base screens, no mode` — `screen` gets `base
   baseScreen` with `baseList`, `baseReview`; `modeList`/`modeDiff`/
   `modeCount` and `type mode` go; `openDiff` sets `base = baseReview`,
   `closeDiff` sets `baseList` (keep `reviewReturn`: it is which session to
   return to, not a return mode). The six `switch m.mode` become `if m.base
   == baseReview` or vanish. `setMode` in `model_host.go` goes.
   `listClear()` becomes `m.base == baseList && len(m.surfaces) == 0`.
   `previewFeeds()` likewise.
7. `test(ui): the surface stack is the safety net` — `ctrlCQuits` and
   `TestCtrlCOnEverySurface` go (the per-surface table
   `TestCtrlCQuitsThroughEveryCardOverTheList` already exists; add focus
   and review rows to a sibling that asserts pass/quit per base);
   `TestEveryModeAnswersTheMouse` walks every surface type instead of
   every mode — build each with its constructor, push, send a wheel and a
   click, assert no panic and the stack unchanged unless the surface popped
   itself; `TestModelFieldCap` comes down to the new count. Rewrite the
   ~830 `mode*`/`m.mode` references in tests: `m.mode == modeFocus` →
   `focusOpen(m)` (surfaceOf), `m.mode == modeList` → `m.listClear()` or
   `m.base == baseList`, `m.mode = modeX` setups → the real opener, failure
   messages drop the word.

## Rules

- `gofmt -l .` is the only local command. No `go build`, `go vet`, `go
  test`, `go run`. CI compiles and runs everything on push; the coordinator
  pushes `refactor/quit-router` after each of your pushes and reads CI back
  to you. Expect two or three compile-fix rounds across the test rewrite;
  that is the plan, not a failure.
- Commit after every numbered step and push to `fork` on your branch. Name
  the branch `surfaces/steps-6-7`. Do not merge, do not open a PR.
- Conventional commits, no `Co-Authored-By`. Each body: what left `Model`,
  which switches lost cases or went, what tests moved to `Update`.
- `surfaceHost` is at 7 of 8 methods. Do not spend the slot without saying
  why in the commit; a per-surface narrow host (`helpHost`,
  `settingsHost`, `confirmHost` are the pattern) is preferred.
- `check-ui-boundaries`: child packages never import the root. The rail's
  `Busy()` is the only child-package change step 6 needs.
- Non-test line count must go **down** by the end of step 7. Report the
  per-file delta (`git diff --stat <base>..HEAD -- ':!*_test.go'`).
- If a decision in "what is and is not a surface" turns out wrong against
  the code, stop at that commit, push, and say exactly what you found.
  Do not ask multiple-choice questions through the CLI's dialog; the
  coordinator cannot answer those. Write the question into your report and
  take the ADR-consistent option.

## Report

Branch, commits, per-file non-test delta, the count of `m.mode` reads
(must be 0), the count of overlay-flag reads outside `geometry_split.go`,
`mouse.go`, `rail_adapter.go` and the rail package, `Model` field count,
`surfaceHost` method count, and anything you left.
