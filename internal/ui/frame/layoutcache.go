package frame

import (
	"reflect"
	"slices"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

type Measurement struct {
	Mode LegendMode
	Rows int
}

// LayoutCache retains geometry, not painted text: theme changes still paint
// with current styles, while animation frames reuse the same measurements.
type LayoutCache struct {
	valid       bool
	refs        [][]Section
	width, room int
	glyph       presentation.GlyphMode
	measured    Measurement
}

func (c *LayoutCache) Measure(refs [][]Section, width, room int, glyph presentation.GlyphMode) Measurement {
	if c.valid && c.width == width && c.room == room && c.glyph == glyph && reflect.DeepEqual(c.refs, refs) {
		return c.measured
	}
	badge, quiet := TitleStyles()
	mode, rows := LegendLayout(refs, width, room, LiveCap(glyph), badge, quiet, Separator())
	snapshot := make([][]Section, len(refs))
	for i, sections := range refs {
		snapshot[i] = slices.Clone(sections)
		for j := range snapshot[i] {
			section := &snapshot[i][j]
			section.Leads = slices.Clone(section.Leads)
			section.Alerts = slices.Clone(section.Alerts)
			section.Pairs = slices.Clone(section.Pairs)
			section.ActionPairs = slices.Clone(section.ActionPairs)
		}
	}
	*c = LayoutCache{valid: true, refs: snapshot, width: width, room: room, glyph: glyph, measured: Measurement{Mode: mode, Rows: rows}}
	return c.measured
}
