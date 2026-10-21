package ui

import "github.com/YoanWai/agent-manager/internal/ui/presentation"

func (m *Model) glyphs() presentation.GlyphSet {
	return presentation.GlyphsFor(m.prefs.glyphMode)
}
