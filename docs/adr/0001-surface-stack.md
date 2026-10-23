# ADR 0001: a surface stack decides who takes input

Status: proposed, first part landed in #861

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
3. **A surface reaches the root only through `surfaceHost`.** The cap and
   the type rules of `model_host_test.go` apply: at most 8 methods, and
   never `Model`, `services` or a driver. A surface that needs more returns
   a command for a root adapter, as features already do.
4. **The top surface states its preferences.** `Owner` says who reads plain
   keys (manager or agent); `Dismissible` says whether esc and a click off
   the card close it; `Footer` leads the legend; `View` paints. ctrl+c is
   the priority layer's: over a manager screen it quits, card or no card;
   over the agent's pane it closes a dismissible card and otherwise stays
   with the card, never reaching the agent behind it.
   The root asks the stack: `agentOwnsKeyboard`, `closesOnBackdrop`,
   `cardOpen`, the footer and the IME anchor all read it.
5. **Lifecycle.** `pop` runs the surface's `Leave` once; leaving the mode
   under a stack pops each surface, top first. Cleanup lives in `Leave`, so
   `returnMode` and `resume` go.
6. **Bindings are data, work is a switch.** A surface lists each key once,
   with an action name, its footer label and its `?` row. Its handler
   switches on the action. The footer and the map are drawn from the list.
7. **Mouse walks the same stack.** The top surface takes card clicks and the
   wheel. Painted keys stay in `keyHits` and press through the same router
   as typing.
8. **Completions find their surface by type**, with `surfaceOf[T]`, instead
   of comparing the mode: a result lands on the surface that asked or is
   dropped.

```go
type surface interface {
	Key(h surfaceHost, msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	Click(h surfaceHost, line int) tea.Cmd
	Wheel(h surfaceHost, arrow tea.KeyMsg) tea.Cmd
	View(h surfaceHost, below string) string
	Footer() [][2]string
	Owner() inputOwner
	Dismissible() bool
	Leave(h surfaceHost)
}
```

One `<feature>_surface.go` per surface in the root, holding the type, its
constructor and these methods; the child package keeps the state and view
it has today. `check-file-names` learns the `_surface` role.

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

`mode` stays the base layer while surfaces move onto the stack, one PR each,
and each PR deletes its share of the seven `switch m.mode` statements and
the `Model` field for the state that moved:

1. #861: the priority layer (`routeQuit`), the stack and its host, the saved
   prompts list over focus, and Review's keymap as the bindings-as-data pilot.
2. The cards in `dialogs`: confirm, launch hint, move, fork, rename.
3. Help and notices.
4. Review's comment box, send prompt and pickers.
5. Settings and the connection dialog.
6. The rail's menu, search and reorder, and the quick bar.
7. Focus becomes a surface over the list; the list and Review are the two
   base screens; `mode` and its tests go.

`TestCtrlCOnEverySurface`, `TestEveryModeAnswersTheMouse`,
`TestFeatureHostsStayNarrow` and `TestModelFieldCap` are the safety net
throughout: each fails on a surface left undecided, a host grown wide, or
state left on `Model`.
