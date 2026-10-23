# Migrating a card onto the surface stack

Read `docs/adr/0001-surface-stack.md` first. This is the working brief for
one migration step; every step follows the same moves.

## The worked example

`internal/ui/focus_saved.go` is a finished surface. Copy its shape exactly:

- a struct that owns all the card's state (the thing that today lives in a
  `Model` field), plus whatever it paints with (styles), captured at build;
- one constructor `newX(...)`, the only way to build it, so a surface on the
  stack is never half-built;
- `func (m *Model) openX(...)` that builds it and calls `m.push(s)`;
- the eight `surface` methods from `internal/ui/model_surface.go`:
  `Key`, `Click`, `Wheel`, `View`, `Footer`, `Owner`, `Dismissible`, `Leave`;
- every root reach goes through `h surfaceHost`. Never take `*Model`.
  `model_host_test.go` fails the build of a host over 8 methods or one that
  passes `Model`, `services` or a driver. If you need a root fact
  `surfaceHost` lacks, add a method to it only if the cap allows, otherwise
  return a `tea.Cmd` whose message a root adapter handles (the existing
  `*_effect.go` pattern).

Completions that today check `m.mode == modeX && gen == request.gen` use
`surfaceOf[*xSurface](m)` instead: find the mounted surface by type, or drop
the result.

A card with a directory field also implements `pathCompleting`
(`model_surface.go`); then its case in `pathcomplete.go`'s three switches
is deleted.

## What each migration deletes

This is the point. For every card you move, remove:

1. its `case modeX:` line(s) in `handleKey` (`keys.go`), `renderFrame`
   (`frame.go`), `clickCard`, `wheelCard`, `closesOnBackdrop`, `cardOpen`
   (`mouse.go`), and `pathcomplete.go` if it had one;
2. its field on `Model` (`model.go`) — the surface owns that state now;
3. its `modeX` constant (`model.go`) **only if nothing else reads it yet**;
   if `session_*` or an effect still sets it, leave the constant and note it;
4. its `returnMode` / `resume` plumbing, replaced by `Leave`;
5. its entry in `mouse_coverage_test.go`'s exemption maps, and its row in
   `keys_test.go`'s `ctrlCQuits` table.

Net lines in non-test code should go **down**. If they go up, stop and ask.

## File rules

- The surface lives in `internal/ui/<feature>_surface.go`.
  `check-file-names` accepts the `_surface` role.
- Do not add lines to `keys.go`, `mouse.go`, `frame.go`, `frame_footer.go`,
  `model.go`, `pathcomplete.go`, or `model_surface.go`. Delete from them
  only. Three other sessions are deleting from the same files in parallel;
  additions there collide, deletions in different hunks merge clean.
- Touch only the files your brief names as yours. If a fix needs a file
  outside that set, write down what and why in your final report and leave
  it.

## Gate (local) — nothing wider

```
gofmt -l .                      # prints nothing
go vet ./...
go build ./...
go run ./tools/architecture/check-ui-boundaries
go run ./tools/architecture/check-file-names
go run ./tools/architecture/check-file-sizes
env -u TMUX TMUX_TMPDIR=/tmp/amtest go test -count=1 -run '<YourNewTests>|TestCtrlC|TestEveryModeAnswersTheMouse|TestFeatureHostsStayNarrow|TestModelFieldCap|Surface' ./internal/ui/
```

Do **not** run `go test ./...`, `-race`, whole-package `./internal/ui`
runs, or E2E locally. CI does that on push. The laptop cannot take it.

## Tests to write

For each surface, in `<feature>_surface_test.go`:

- it opens by its real trigger (the key or action that opens it today),
  not by hand-constructing the struct;
- esc / ctrl+c / click-off-card close it iff `Dismissible()` is true
  (drive these through `m.Update`, not the handler);
- `Leave` ran once on close; `Model` field for it is gone (compile proves it);
- one completion-fencing test if it had `m.mode == modeX && gen` checks:
  a stale completion for a surface that was popped is dropped.

Existing tests that drove `m.handleXKey(...)` directly: switch them to
`m.Update(...)` so they go through the router, the way a terminal does.
Existing tests that assert `m.mode == modeX`: assert `surfaceOf[*xSurface]`.

## Commits and reporting

- Conventional commits, one per surface:
  `refactor(<feature>): <card> is a surface`. Body: what it owned on
  `Model`, what switches lost a case, what tests moved to `Update`.
- No `Co-Authored-By` lines. Author is the user.
- Push your branch to the `fork` remote when the gate passes. Do not open
  a PR; do not merge. The coordinator merges the streams into
  `refactor/quit-router`.
- Final report: branch name, commits, the per-file line delta for
  non-test code (`git diff --stat <base>..HEAD -- ':!*_test.go'`), the
  `mode*` constants you could not yet delete and why, and anything you
  had to leave outside your file set.
