package settings

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

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
