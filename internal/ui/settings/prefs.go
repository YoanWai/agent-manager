package settings

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// Prefs is the running manager's cached copy of the rows whose value a
// frame reads. It is a live preview of what the dialog shows, not a
// persistence receipt: a save that later fails reconciles it back to the
// committed values.
type Prefs struct {
	GlyphMode presentation.GlyphMode
	Editor    string
	// Flags holds the two-way rows by key: the row's on side.
	Flags map[string]bool
}

// Read is the preferences the cached values describe. A row the store never
// wrote reads its default.
func Read(value func(key string) string) Prefs {
	prefs := Prefs{GlyphMode: presentation.GlyphMode(value(GlyphModeSetting)), Flags: map[string]bool{}}
	prefs.Editor = value(EditorSetting)
	for _, key := range FlagKeys() {
		prefs.Flags[key] = FlagOn(key, value(key))
	}
	return prefs
}

// Mirrored is what one stored row means for the live preferences: the
// symbols mode, the editor line, or a two-way row's side.
type Mirrored struct {
	GlyphMode presentation.GlyphMode
	Editor    string
	Flag      *bool
}

// Mirror reads one row the way the frame's cached preferences spell it. A
// key the frame never reads mirrors nothing.
func Mirror(key, value string) Mirrored {
	switch key {
	case GlyphModeSetting:
		return Mirrored{GlyphMode: presentation.GlyphMode(value)}
	case EditorSetting:
		return Mirrored{Editor: value}
	}
	if !slices.Contains(FlagKeys(), key) {
		return Mirrored{}
	}
	on := FlagOn(key, value)
	return Mirrored{Flag: &on}
}

// Apply mirrors the just chosen preferences into the live session, as a
// deliberate preview of the staged choices.
func (p *Prefs) Apply(d *State) {
	p.GlyphMode = d.GlyphMode
	p.Editor = d.Editor.Line()
	for _, key := range FlagKeys() {
		p.Flags[key] = d.Flag(key)
	}
}
