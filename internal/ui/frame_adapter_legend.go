package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
)

// paintLegend renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func paintLegend(sections []legendSection, width int) legend {
	badge, quiet := legendTitleStyles()
	return rootLegend(uiframe.PaintLegend(frameSections(sections), width, frameCap(), badge, quiet, legendSeparator()))
}

// legendInline renders one tier as a single untitled run, for the foot of a
// modal card where the tier's subject is the card's own title.
func legendInline(pairs [][2]string, width int) string {
	return uiframe.LegendInline(pairs, width, frameCap())
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

func legendKey(label string) (tea.KeyMsg, bool) { return uiframe.LegendKey(label) }
