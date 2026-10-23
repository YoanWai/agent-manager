package ui

import (
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	tea "github.com/charmbracelet/bubbletea"
)

// The root keeps the legend's wire types under their own field names, because
// every prefix writes section and pair literals; ui/frame paints them.

// legendSection is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type legendSection struct {
	title  string
	leads  [][2]string
	alerts [][2]string
	hint   string
	pairs  [][2]string
	quiet  bool
}

// legend is a footer as painted: its text, and where each key it names
// landed, from its own first row and column.
type legend struct {
	text string
	hits keyHits
}

// keyHit is a key pair as painted: a left press inside it presses key.
type keyHit struct {
	x0, x1, y int
	key       tea.KeyMsg
}

type keyHits []keyHit

// add records a part painted across columns [x0, x1) of row y, when its
// label names a key a click can press.
func (hits keyHits) add(x0, x1, y int, label string) keyHits {
	key, ok := uiframe.LegendKey(label)
	if !ok {
		return hits
	}
	return append(hits, keyHit{x0: x0, x1: x1, y: y, key: key})
}

func (hits keyHits) at(x, y int) (keyHit, bool) {
	for _, hit := range hits {
		if y == hit.y && x >= hit.x0 && x < hit.x1 {
			return hit, true
		}
	}
	return keyHit{}, false
}

// above keeps the hits on the rows a frame clamped to height still shows.
func (hits keyHits) above(height int) keyHits {
	kept := hits[:0]
	for _, hit := range hits {
		if hit.y < height {
			kept = append(kept, hit)
		}
	}
	return kept
}

// moved is hits painted with their first row at y and first column at x.
func (hits keyHits) moved(x, y int) keyHits {
	out := make(keyHits, len(hits))
	for i, hit := range hits {
		out[i] = keyHit{x0: hit.x0 + x, x1: hit.x1 + x, y: hit.y + y, key: hit.key}
	}
	return out
}

// frameSections copies the root's tiers into the package's, which paints them.
func frameSections(sections []legendSection) []uiframe.Section {
	out := make([]uiframe.Section, len(sections))
	for i, section := range sections {
		out[i] = uiframe.Section{
			Title:  section.title,
			Leads:  section.leads,
			Alerts: section.alerts,
			Hint:   section.hint,
			Pairs:  section.pairs,
			Quiet:  section.quiet,
		}
	}
	return out
}

// rootHits copies a box's painted keys back to the root's own type.
func rootHits(hits uiframe.KeyHits) keyHits {
	out := make(keyHits, len(hits))
	for i, hit := range hits {
		out[i] = keyHit{x0: hit.X0, x1: hit.X1, y: hit.Y, key: hit.Key}
	}
	return out
}

// rootLegend copies a painted legend back to the root's own types.
func rootLegend(painted uiframe.Legend) legend {
	return legend{text: painted.Text, hits: rootHits(painted.Hits)}
}

// cursorAnchorMarker is the private marker an input view paints at its caret.
const cursorAnchorMarker = uiframe.CursorMarker

const (
	legendTitleColumn = uiframe.TitleColumn
	legendGap         = uiframe.Gap
	legendMaxRows     = uiframe.MaxRows
)

// frameCap is the live theme's painting of a key and its action, the way the
// legend rules need it.
func frameCap() uiframe.Cap {
	return uiframe.Cap{
		Key:     keyStyle(),
		Label:   legendLabelStyle(),
		Muted:   mutedStyle(),
		LeadKey: legendLeadKey(),
		Lead:    legendLeadLabel(),
		Alert:   legendAlertStyle(),
		Badge:   legendBadgeStyle(),
	}
}

// legendTitleStyles is a tier's name as the footer paints it: the badge for
// the tier the cursor is on, the quieter title for a secondary tier.
func legendTitleStyles() (badge, quiet func(string) string) {
	return legendBadgeStyle().Render, legendTitleStyle().Render
}

func legendSeparator() string { return subtleStyle().Render(" · ") }
