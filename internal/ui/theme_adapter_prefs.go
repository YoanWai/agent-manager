package ui

import "github.com/YoanWai/agent-manager/internal/ui/presentation"

type preferences struct {
	glyphMode presentation.GlyphMode
	// focusOnEnter mirrors the persisted focus-key setting; the footer
	// reads it every frame, so it lives here instead of the store.
	focusOnEnter bool
	// arrowStep mirrors the persisted ←→ step-in/step-out setting, read
	// on every keypress.
	arrowStep bool
	// comfortableRows mirrors the persisted list density: entries paint
	// their meta on a second line instead of alongside the name. Every
	// rail frame reads it, so it lives here instead of the store.
	comfortableRows bool
	// fullLayout mirrors the persisted sessions layout: the rail owns the
	// whole width, with no preview column beside it. Every list frame
	// reads it, so it lives here instead of the store.
	fullLayout bool
	// Header and stats visibility stay cached because rendering and sizing
	// read them every frame.
	hideHeader bool
	hideStats  bool
	// mouseDisabled mirrors the persisted mouse-reporting setting: true gives
	// the rail and content column back to the terminal's own click-drag text
	// selection. Read on every Update via syncMouseCapture. Named for its off
	// polarity, like hideHeader/hideStats, so a bare Model{} in a test still
	// defaults to mouse reporting on.
	mouseDisabled bool
	// terminalBackground leaves the backdrop's cells on the terminal's own
	// colors, for translucent windows. Off polarity, so a bare Model{}
	// paints the backdrop like the default does.
	terminalBackground bool
	// baseFetchOff mirrors the persisted fetch-on-spawn setting, read on
	// every Update while a worktree spawn is being set up.
	baseFetchOff bool
}
