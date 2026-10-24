package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// paintLegend renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func paintLegend(sections []legendSection, width int) legend {
	badge, quiet := legendTitleStyles()
	return rootLegend(uiframe.PaintLegend(frameSections(sections), width, frameCap(), badge, quiet, legendSeparator()))
}

// placeKeys records a legend's keys painted with its first row at y and
// its first column at x.
func (m *Model) placeKeys(hits keyHits, x, y int) {
	m.layout.keyHits = append(m.layout.keyHits, hits.moved(x, y)...)
}

// placePainted records keys a feature painted on row y, from column x.
func (m *Model) placePainted(keys []presentation.PaintedKey, x, y int) {
	var hits keyHits
	for _, key := range keys {
		hits = hits.add(key.X0, key.X1, 0, key.Label)
	}
	m.placeKeys(hits, x, y)
}
