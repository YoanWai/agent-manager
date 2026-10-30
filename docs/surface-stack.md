# A surface stack decides who takes input

Status: accepted, landed in #861

## Context

`internal/ui` tracks what is on screen with one `mode` enum (15 values) plus
loose flags for whatever floats over a mode: the quick bar, the row menu,
rail search, resize and drag, the saved prompts list over focus, Review's
comment box and send prompt. 156 lines read `m.mode`, 88 conditions read
those flags, and seven `switch m.mode` statements each work out on their own
what is on top: one paints the frame, one routes keys, four route the mouse,
one completes paths.

#855 lived between those switches. Focus forwards ctrl+c to the agent, but
the quit check at the top of `Update` answered first. Once #856 fixed that,
the saved prompts list over focus was still undecided: ctrl+c went into its
filter, and the IME anchor stayed on the pane's caret behind the card. Each
answer was right in the file that gave it and wrong together.

Each surface also names its keys up to three times by hand: in its `Key`
switch, in its footer, and in the `?` map. Nothing checks they agree.

## Decision

Input is routed through a stack of surfaces, read from the top. `mode` goes
away: the bottom of the stack is the base screen, and focus is a surface
over the list that hands the keyboard to the agent.

1. **Two-phase dispatch.** The root's priority layer runs first (`routeQuit`
   in `keys.go`: the drain escape and ctrl+c). Then the key walks the stack
   from the top until a surface takes it. A modal surface takes every key.
2. **A surface owns its state.** Pushing one constructs it; popping drops
   it. `Model` loses a field for every card that moves onto the stack, and
   the field cap in `model_test.go` comes down with it. One constructor per
   surface, so a surface on the stack is never half-built.
3. **A surface reaches the root only through its host.** The shared one is
   `surface.Host`: paint a card, read the terminal size, pop. A surface that
   needs more declares a `Root` interface beside it. The cap and the type
   rules of `model_host_test.go` apply to the root's hosts and to every
   package's `Root`: at most 8 methods, and never `Model`, `services` or a
   driver. A surface that needs more returns a command for a root adapter.
4. **The top surface states its preferences.** `Owner` says who reads plain
   keys (manager or agent); `Dismissible` says whether esc and a click off
   the card close it; `Footer` leads the legend; `View` paints. ctrl+c is
   the priority layer's: over a manager screen it quits, card or no card;
   over the agent's pane it closes a dismissible card and otherwise stays
   with the card, never reaching the agent behind it.
   The root asks the stack: `agentOwnsKeyboard`, `closesOnBackdrop`,
   `cardOpen`, the footer and the IME anchor all read it.
5. **Lifecycle.** `Pop` runs the surface's `Leave` once; leaving the screen
   under a stack pops each surface, top first (`clearSurfaces`). Cleanup
   lives in `Leave`, so there is no return mode to restore.
6. **Bindings are data, work is a switch.** A surface lists each key once,
   with an action name, its footer label and its `?` row. Its handler
   switches on the action. The footer and the map are drawn from the list.
   Review's keymap (`review/keymap.go`) is the one that does this today;
   the other surfaces name their keys in their `Key`, `Footer` and help rows.
7. **Mouse walks the same stack.** The top surface takes card clicks and the
   wheel. Painted keys stay in `layout.keyHits` and press through the same
   router as typing.
8. **Completions find their surface by type**, with `surfaceOf[T]`, instead
   of comparing the mode: a result lands on the surface that asked or is
   dropped.
9. **Not every overlay is a surface.** A surface is something that takes
   the keyboard over the screen below it. Cards and the quick bar are; so
   are focus (owner: the agent) and a resize in progress (a keyboard grab
   that paints nothing). Rail search, the row menu and reorder are modes of
   the rail: `rail.Model` owns them and answers `Busy()`. Drag state stays
   with the mouse in `geometry_adapter_split.go`. A surface that is not a
   card takes the mouse through the optional `mouseTaker`, and `cardOpen` is
   false for it.
10. **State flows up as returned values.** A package hands the root what to
    do and the root does it: `rail.Model` returns a `Decision` carrying an
    `Intent` and `Mutation`s, and a surface's `Key` returns a `tea.Cmd`. A
    package does not change root state through an interface. `Host` and
    `Root` interfaces are for reads (size, copied tables, styles, geometry).
    Some older `Root` methods still write (`quick.Root.ClearAccepted` in
    `quick.Apply`); new code returns the value instead.
11. **Child packages never import the root.** `check-ui-boundaries` lists
    every package's allowed module imports and fails on `internal/ui`
    itself. The root's side of each package is a set of `<feature>Root`
    types in its adapter files, which answer the package's `Root`.

The contract lives in the leaf package `internal/ui/surface`, which imports
nothing from the module, so a feature package can implement it without
reaching the root. The root imports it as `uisurface`.

```go
type Surface interface {
	Key(h Host, msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	Click(h Host, line int) tea.Cmd
	Wheel(h Host, arrow tea.KeyMsg) tea.Cmd
	View(h Host, below string) string
	Footer() [][2]string
	Owner() Owner
	Dismissible() bool
	Leave(h Host)
}

type Host interface {
	CardSized(width int, title, body string, hint [][2]string) string
	Size() (width, height int)
	Pop()
}
```

Every surface lives in its feature's package, behind a `Root` interface
the package declares: `help.Surface` reads the root through `help.Root`, and
the same holds for `review`, `notices`, `connections`, `settings`,
`geometry` (resize), `focus`, `spawn`, `quick`, `onboarding` and the cards of `dialogs`
(`ConfirmRoot`, `ForkRoot`, `MoveRoot`, `RenameRoot`, `GroupFormRoot`,
`LaunchHintRoot`). The root keeps the adapter: a `<feature>Root` type that
answers the interface, in `<feature>_adapter.go` or
`<feature>_adapter_surface.go`, plus the opener that pushes the surface.
`check-file-names` accepts a packaged feature's root files only as
`<feature>_adapter.go`, `<feature>_adapter_*.go`, `<feature>_*_effect.go` or
`<feature>_*_surface.go`; `model_*.go` is the closed `rootOnly` feature
(`model.go`, `model_host.go`, `model_services.go`, `model_surface.go`), and
`keys.go`, `mouse.go`, `updates.go` and `ids.go` route. A surface that needs
more of the root than `surface.Host` takes its own `Root` rather than
widening the shared one (`focus.SavedRoot` for the saved prompts list over
focus, for example).

The screen is `screen{base, surfaces}`, embedded in `Model`: the base is the
list or Review, and the stack sits over it. Focus is a surface over the list whose `Owner` is
the agent, so "is the agent reading the keyboard" is one question of the
stack and `routeQuit` needs no case for it.

## Effects follow the same split

`internal/ui/effects` owns the effect contract and no request type:
`Request` (`EffectName`, `EffectCopy`), `Runner[S]` (`Run(S)`), `Bind[S]`
and `Result` (`EffectResult`), with the lanes, the lifetime and the quit
bookkeeping. Each feature's runtime package declares its own requests and
results and a `Services` struct of what the work needs. The root names none
of them. `effects_adapter_executor.go` holds two switches: `bindEffect`,
one `uieffects.Bind` line per package `Services`, and `applyEffect`, one
type-switch case per package result that calls the feature's adapter. There
is no root request or result type and no fallback case.
`TestEveryPackageEffectIsBoundAndApplied` fails on a request whose
`Services` is not bound or a result with no case.

## Prior art

- Textual: a screen stack, bindings declared as data with descriptions, a
  key bubbling from the focused widget to the app, `priority` bindings first,
  and a footer drawn from the active bindings.
- iOS: the responder chain for keys, `UIKeyCommand` driving both dispatch
  and the shortcut overlay, and the top view controller declaring
  preferences the system reads.
- Android `OnBackPressedDispatcher`: the newest enabled callback takes back.
- DOM, WPF and Compose: a capture phase before a bubble phase.
- lazygit: a stack of contexts, each returning bindings that drive both
  dispatch and its `?` menu.

## What this rules out

Two layers claiming one key, an overlay leaking input to what is under it,
a footer naming a key nothing handles, and a stale return mode.

## Migration

Landed in #861 in this order, one commit per surface: the priority layer
(`routeQuit`), the stack and its host, the saved prompts list over focus,
Review's keymap as the bindings-as-data pilot; the cards in `dialogs`
(confirm, move, rename, fork, launch hint); help and notices; Review's
picker; the New Group form, Settings and the connection dialog; the rail's
`Busy()`; resize; the quick bar; the New Session form; focus; then the two
base screens, with `mode`, its constants, `setMode` and every `switch
m.mode` deleted. Reads of `m.mode` outside tests: 156 before, 0 after.
`Model`: 30 fields on `main` at the merge, 21 after. The contract then moved
to `internal/ui/surface`, every surface moved into its feature's package
behind a `Root` interface, and the effect contract moved to
`internal/ui/effects` with each package declaring its own requests.

Left as it was, on purpose: Review's comment box and send prompt stay in
the `review` package behind its keymap; the rail's menu, search and reorder
stay in the rail (decision 9).

The safety net is `TestCtrlCOnEveryBase` and
`TestCtrlCQuitsThroughEveryCardOverTheList` (ctrl+c through `Update` on
every screen), `TestCtrlCIsDecidedInKeysGo` (nothing outside `keys.go`
compares it), `TestEverySurfaceAnswersTheMouse` (every type with a `Leave`
method is mounted by its real opener and takes a wheel and a click),
`TestFeatureHostsStayNarrow`, `TestModelFieldCap` and
`TestEveryPackageEffectIsBoundAndApplied`. Each fails on a surface left
undecided, a host grown wide, state left on `Model`, or an effect nothing
binds or applies.
