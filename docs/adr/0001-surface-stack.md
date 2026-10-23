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

Input is routed through a stack of surfaces, read from the top.

1. **Two-phase dispatch.** The root's priority layer runs first (`routeQuit`
   in `keys.go`: the drain escape and ctrl+c). Then the key walks the stack
   from the top until a surface takes it. A modal surface takes every key.
   With no surface left, the base `mode` handles it as today.
2. **The top surface states its preferences.** It says who reads plain keys
   (`Owner`: manager or agent), which hint pairs lead the footer, and what it
   paints. The root asks the stack instead of the mode: `agentOwnsKeyboard`,
   the IME anchor, the footer and the open card all read it.
3. **Bindings are data, work is a switch.** A surface lists each key once,
   with an action name, its footer label and its `?` row. Its handler
   switches on the action. The footer and the map are drawn from the list.
4. **Push and pop.** Opening an overlay pushes it; esc, a pick or leaving the
   mode under it pops it. That replaces the open flags and, later,
   `returnMode`.
5. **Mouse walks the same stack.** The top surface takes card clicks and the
   wheel. Painted keys stay in `keyHits` and press through the same router
   as typing.

The interface stays small, the way the feature hosts are (at most 8 methods):

```go
type surface interface {
	Key(m *Model, msg tea.KeyMsg) (handled bool, cmd tea.Cmd)
	Click(m *Model, line int) tea.Cmd
	Wheel(m *Model, arrow tea.KeyMsg) tea.Cmd
	View(m *Model, below string) string
	Footer() [][2]string
	Owner() inputOwner
}
```

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
and each PR deletes its share of the seven switches:

1. #861: the priority layer (`routeQuit`), the stack, the saved prompts list
   over focus, and Review's keymap as the bindings-as-data pilot.
2. The cards in `dialogs`: confirm, launch hint, move, fork, rename.
3. Help and notices.
4. Review's comment box, send prompt and pickers.
5. Settings and the connection dialog.
6. The rail's menu, search and reorder, and the quick bar.
7. The list, focus and Review become base surfaces, and `mode` goes.

`TestCtrlCOnEverySurface` and `TestEveryModeAnswersTheMouse` stay the
safety net throughout: each walks every mode and fails on one left
undecided.
