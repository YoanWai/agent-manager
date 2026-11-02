package frame

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

type keyMsg = tea.KeyMsg
type legendSection = Section
type legendPart = Part
type legend = Legend
type legendMode = LegendMode

const (
	legendMaxRows = MaxRows
	legendTitled  = Titled
	legendWorded  = Worded
	legendBare    = Bare
)

func paintLegend(s []Section, w int, m presentation.GlyphMode) Legend { return PaintLive(s, w, m) }
func paintInline(s [][2]string, w int, m presentation.GlyphMode) Legend {
	return PaintInline(s, w, LiveCap(m))
}
func legendKey(s string) (keyMsg, bool) { return LegendKey(s) }
func legendLayout(s [][]Section, w, r int, m presentation.GlyphMode) (LegendMode, int) {
	a, b := TitleStyles()
	return LegendLayout(s, w, r, LiveCap(m), a, b, Separator())
}
func paintUntitledLegend(s []Section, w int, m presentation.GlyphMode, l LegendMode, r int) Legend {
	return PaintUntitledLegend(s, w, LiveCap(m), l, r)
}
func paintNarrowLegend(s []Section, w int, m presentation.GlyphMode, l LegendMode, r int) Legend {
	return PaintNarrowLegend(s, w, LiveCap(m), l, r)
}
