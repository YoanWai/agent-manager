package ui

import (
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A legend is the app's key map made visible: tiers of bindings, each tier
// named for what its keys act on, so the footer answers "what can I do to
// the thing under the cursor" before it answers "what keys exist".
const (
	// legendTitleColumn keeps every tier's first binding on one column, so
	// stacked tiers read as a table rather than as ragged prose.
	legendTitleColumn = 11
	legendGap         = 2
	// legendMaxRows is the titled footer's height budget. Past it the tail
	// is cut and marked; the full map is one ? away. A footer too narrow for
	// titles may take more rows, up to half the body, so every key stays
	// painted.
	legendMaxRows = 3
)

// legendMode is how much of each binding a footer paints. A footer steps
// down in this order as the terminal narrows, never back.
type legendMode int

const (
	// legendTitled names each tier and paints every part whole.
	legendTitled legendMode = iota
	// legendWorded drops the titles and every part a click cannot press.
	legendWorded
	// legendBare paints those keys without their words.
	legendBare
)

// legendSection is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type legendSection struct {
	title       string
	leads       [][2]string
	alerts      [][2]string
	hint        string
	pairs       [][2]string
	quiet       bool
	actionPairs []bool
}

type legendPartKind int

const (
	legendInferred legendPartKind = iota
	legendAction
	legendGesture
)

// legendPart is one painted binding and the key label a click on it
// presses; label is empty for a part that only reads, like a tier's hint.
type legendPart struct {
	text  string
	label string
	bare  string
	kind  legendPartKind
}

func (p legendPart) clickable() bool {
	_, ok := p.key()
	return ok
}

func (p legendPart) key() (tea.KeyMsg, bool) {
	if p.kind == legendGesture {
		return tea.KeyMsg{}, false
	}
	if p.kind == legendAction {
		if key, ok := pressKey(strings.TrimSpace(p.label)); ok {
			return key, true
		}
		return pressKey(firstKey(p.label))
	}
	return legendKey(p.label)
}

func (hits keyHits) addPart(x0, x1, y int, part legendPart) keyHits {
	key, ok := part.key()
	if !ok {
		return hits
	}
	return append(hits, keyHit{x0: x0, x1: x1, y: y, key: key})
}

// bareKey is the first key a label joins with " / ", as a bare footer
// paints it.
func bareKey(label string) string {
	key, _, _ := strings.Cut(label, " / ")
	return strings.TrimSpace(key)
}

func (s legendSection) parts(mode presentation.GlyphMode) []legendPart {
	var parts []legendPart
	for _, lead := range s.leads {
		if lead[0] != "" {
			parts = append(parts, legendPart{keyCapLead(presentation.KeyLabel(mode, lead[0]), lead[1]), lead[0], legendLeadKey().Render(presentation.KeyLabel(mode, bareKey(lead[0]))), legendAction})
		}
	}
	for _, alert := range s.alerts {
		if alert[0] != "" {
			parts = append(parts, legendPart{keyCapAlert(presentation.KeyLabel(mode, alert[0]), alert[1]), alert[0], legendAlertStyle().Render(presentation.KeyLabel(mode, bareKey(alert[0]))), legendAction})
		}
	}
	if s.hint != "" {
		parts = append(parts, legendPart{text: mutedStyle().Render(s.hint)})
	}
	for i, pair := range s.pairs {
		kind := legendInferred
		if s.actionPairs != nil {
			kind = legendGesture
			if s.actionPairs[i] {
				kind = legendAction
			}
		}
		bare := keyStyle().Render(presentation.KeyLabel(mode, bareKey(pair[0])))
		if s.quiet {
			parts = append(parts, legendPart{keyCapQuiet(presentation.KeyLabel(mode, pair[0]), pair[1]), pair[0], bare, kind})
		} else {
			parts = append(parts, legendPart{keyCap(presentation.KeyLabel(mode, pair[0]), pair[1]), pair[0], bare, kind})
		}
	}
	return parts
}

// legend is a footer as painted: its text, and where each key it names
// landed, from its own first row and column.
type legend struct {
	text string
	hits keyHits
}

// paintLegend renders a legend as the app's footer in legendMaxRows rows:
// titled where every tier's first key fits beside its title, untitled
// otherwise.
func paintLegend(sections []legendSection, width int, mode presentation.GlyphMode) legend {
	layout, rows := legendLayout([][]legendSection{sections}, width, legendMaxRows, mode)
	if layout == legendTitled {
		bar, _ := paintTitledLegend(sections, width, mode)
		return bar
	}
	return paintUntitledLegend(sections, width, mode, layout, rows)
}

// legendLayout is the mode and row budget that paint every footer in refs:
// titled when each paints its tiers' first keys whole, worded when each
// paints every key whole in room rows, bare otherwise, cut past room.
func legendLayout(refs [][]legendSection, width, room int, mode presentation.GlyphMode) (legendMode, int) {
	titled := true
	for _, ref := range refs {
		if _, fits := paintTitledLegend(ref, width, mode); !fits {
			titled = false
			break
		}
	}
	if titled {
		return legendTitled, legendMaxRows
	}
	worded := 0
	for _, ref := range refs {
		rows := untitledRows(ref, width, mode, legendWorded)
		if rows < 0 || rows > room {
			worded = -1
			break
		}
		worded = max(worded, rows)
	}
	if worded >= 0 {
		return legendWorded, worded
	}
	bare := 0
	for _, ref := range refs {
		bare = max(bare, untitledRows(ref, width, mode, legendBare))
	}
	return legendBare, min(bare, room)
}

// paintTitledLegend renders one tier per line where the terminal allows it
// and the tail marked when it does not. fits reports whether each tier with
// a key a click can press painted its first such key whole.
func paintTitledLegend(sections []legendSection, width int, mode presentation.GlyphMode) (legend, bool) {
	indent := strings.Repeat(" ", min(railGutter, max(width, 0)))
	sep := subtleStyle().Render(" · ")
	more := subtleStyle().Render("…")

	var out []string
	var hits keyHits
	fits := true
	for i, section := range sections {
		parts := section.parts(mode)
		pending := slices.ContainsFunc(parts, legendPart.clickable)
		if len(parts) == 0 || len(out) >= legendMaxRows {
			fits = fits && !pending
			continue
		}
		maxRows := legendMaxRows
		for _, next := range sections[i+1:] {
			if len(next.parts(mode)) > 0 {
				maxRows--
			}
		}
		title := legendBadgeStyle().Render(section.title)
		if section.quiet {
			title = legendTitleStyle().Render(section.title)
		}
		column := min(max(legendTitleColumn, ansi.StringWidth(title)+legendGap), max(width-ansi.StringWidth(indent)-2, 0))
		head := indent + padRight(title, column)
		cont := indent + strings.Repeat(" ", column)
		line, lineWidth, started := head, ansi.StringWidth(head), false
		cut := false
		gap := strings.Repeat(" ", legendGap)
		if section.quiet {
			gap = sep
		}
		gapWidth := ansi.StringWidth(gap)
		for _, part := range parts {
			full := ansi.StringWidth(part.text)
			partWidth := full
			// The row that cannot wrap further keeps room for the cut
			// marker, so the marker never lands past the terminal edge.
			avail := width
			if len(out) >= maxRows-1 {
				avail = width - 1 - ansi.StringWidth(more)
			}
			x := 0
			switch {
			case !started:
				x = lineWidth
				started = true
			case lineWidth+gapWidth+partWidth <= avail:
				x = lineWidth + gapWidth
				line += gap
			case len(out) < maxRows-1:
				out = append(out, line)
				x = ansi.StringWidth(cont)
				line = cont
			default:
				cut = true
			}
			if cut {
				break
			}
			// A wrapped part is the first part on its new row, too.
			avail = width
			if len(out) >= maxRows-1 {
				avail = width - 1 - ansi.StringWidth(more)
			}
			text := ansi.Truncate(part.text, max(avail-x, 0), "…")
			partWidth = ansi.StringWidth(text)
			line += text
			lineWidth = x + partWidth
			if partWidth > 0 {
				hits = hits.addPart(x, lineWidth, len(out), part)
			}
			if pending && part.clickable() {
				pending = false
				fits = fits && partWidth == full
			}
		}
		if cut {
			line += ansi.Truncate(" "+more, max(width-lineWidth, 0), "")
		}
		fits = fits && !pending
		out = append(out, line)
	}
	return legend{text: strings.Join(out, "\n"), hits: hits}, fits
}

// untitledTiers is each tier's parts a click can press, as mode paints them.
func untitledTiers(sections []legendSection, glyph presentation.GlyphMode, mode legendMode) [][]legendPart {
	tiers := make([][]legendPart, len(sections))
	for i, section := range sections {
		for _, part := range section.parts(glyph) {
			if !part.clickable() {
				continue
			}
			if mode == legendBare {
				part = legendPart{text: part.bare, label: bareKey(part.label), kind: part.kind}
			}
			tiers[i] = append(tiers[i], part)
		}
	}
	return tiers
}

// untitledRows is the rows an untitled footer needs to paint every key
// whole, or -1 in worded mode when one key is wider than a row.
func untitledRows(sections []legendSection, width int, glyph presentation.GlyphMode, mode legendMode) int {
	indentWidth := min(railGutter, max(width, 0))
	rows := 0
	for _, tier := range untitledTiers(sections, glyph, mode) {
		lineWidth := indentWidth
		for i, part := range tier {
			partWidth := ansi.StringWidth(part.text)
			if mode == legendWorded && partWidth > width-indentWidth {
				return -1
			}
			switch {
			case i == 0:
				rows++
				lineWidth += partWidth
			case lineWidth+legendGap+partWidth <= width:
				lineWidth += legendGap + partWidth
			default:
				rows++
				lineWidth = indentWidth + partWidth
			}
		}
	}
	return rows
}

// paintNarrowLegend renders sections untitled in a footer's mode and rows,
// bare where their words would not fit those rows.
func paintNarrowLegend(sections []legendSection, width int, glyph presentation.GlyphMode, mode legendMode, rows int) legend {
	if mode == legendWorded {
		if need := untitledRows(sections, width, glyph, mode); need < 0 || need > rows {
			mode = legendBare
		}
	}
	return paintUntitledLegend(sections, width, glyph, mode, rows)
}

// paintUntitledLegend renders the keys of each tier, worded or bare, from
// the gutter in at most rows rows. Each tier keeps a row for every tier
// after it, and a tier cut short ends in a marker.
func paintUntitledLegend(sections []legendSection, width int, glyph presentation.GlyphMode, mode legendMode, rows int) legend {
	indentWidth := min(railGutter, max(width, 0))
	indent := strings.Repeat(" ", indentWidth)
	gap := strings.Repeat(" ", legendGap)
	more := subtleStyle().Render("…")
	markWidth := 1 + ansi.StringWidth(more)
	tiers := untitledTiers(sections, glyph, mode)

	var out []string
	var hits keyHits
	for i, tier := range tiers {
		if len(tier) == 0 || len(out) >= rows {
			continue
		}
		later := 0
		for _, next := range tiers[i+1:] {
			if len(next) > 0 {
				later++
			}
		}
		last := len(out) + max(rows-len(out)-later, 1) - 1
		// The tier's last row keeps room for the marker unless the rest of
		// the tier fits on it or nothing follows.
		avail := func(x, from int) int {
			rest := x
			for j, part := range tier[from:] {
				if j > 0 {
					rest += legendGap
				}
				rest += ansi.StringWidth(part.text)
			}
			if len(out) < last || rest <= width || from == len(tier)-1 {
				return width
			}
			return width - markWidth
		}
		line, lineWidth := indent, indentWidth
		cut := false
		for j, part := range tier {
			x := lineWidth
			switch {
			case j == 0:
			case lineWidth+legendGap+ansi.StringWidth(part.text) <= avail(lineWidth+legendGap, j):
				x = lineWidth + legendGap
				line += gap
			case len(out) < last:
				out = append(out, line)
				line, x = indent, indentWidth
			default:
				cut = true
			}
			if cut {
				break
			}
			text := ansi.Truncate(part.text, max(avail(x, j)-x, 0), "…")
			line += text
			lineWidth = x + ansi.StringWidth(text)
			if lineWidth > x {
				hits = hits.addPart(x, lineWidth, len(out), part)
			}
		}
		if cut {
			line += ansi.Truncate(" "+more, max(width-lineWidth, 0), "")
		}
		out = append(out, line)
	}
	return legend{text: strings.Join(out, "\n"), hits: hits}
}

// legendInline renders one tier as a single untitled run, for the foot of a
// modal card where the tier's subject is the card's own title.
func legendInline(pairs [][2]string, width int, mode presentation.GlyphMode) string {
	return paintInline(pairs, width, mode).text
}

func paintInline(pairs [][2]string, width int, mode presentation.GlyphMode) legend {
	gap := strings.Repeat(" ", legendGap)
	var lines []string
	var parts []string
	var hits keyHits
	lineWidth := 0
	for _, pair := range pairs {
		part := hintCap(presentation.KeyLabel(mode, pair[0]), pair[1])
		partWidth := ansi.StringWidth(part)
		if len(parts) > 0 && lineWidth+legendGap+partWidth > width {
			lines = append(lines, strings.Join(parts, gap))
			parts, lineWidth = nil, 0
		}
		if len(parts) > 0 {
			lineWidth += legendGap
		}
		hits = hits.add(lineWidth, lineWidth+partWidth, len(lines), pair[0])
		parts = append(parts, part)
		lineWidth += partWidth
	}
	lines = append(lines, strings.Join(parts, gap))
	return legend{text: strings.Join(lines, "\n"), hits: hits}
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
	key, ok := legendKey(label)
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

// placeKeys records a legend's keys painted with its first row at y and
// its first column at x.
func (m *Model) placeKeys(hits keyHits, x, y int) {
	m.layout.keyHits = append(m.layout.keyHits, hits.moved(x, y)...)
}

// placePainted records keys a feature painted relative to row y, from
// column x.
func (m *Model) placePainted(keys []presentation.PaintedKey, x, y int) {
	var hits keyHits
	for _, key := range keys {
		hits = hits.add(key.X0, key.X1, key.Y, key.Label)
	}
	m.placeKeys(hits, x, y)
}

// moved is hits painted with their first row at y and first column at x.
func (hits keyHits) moved(x, y int) keyHits {
	out := make(keyHits, len(hits))
	for i, hit := range hits {
		out[i] = keyHit{x0: hit.x0 + x, x1: hit.x1 + x, y: hit.y + y, key: hit.key}
	}
	return out
}

// legendKey is the key a click on a pair presses: the first key its label
// names. A pair that moves a cursor or a field stays with the keyboard,
// since the rows and the wheel answer for it, and a pair naming a gesture
// presses nothing.
func legendKey(label string) (tea.KeyMsg, bool) {
	if strings.ContainsAny(label, "↑↓←→") || strings.Contains(label, "pgup") {
		return tea.KeyMsg{}, false
	}
	if key, ok := pressKey(strings.TrimSpace(label)); ok {
		return key, true
	}
	return pressKey(firstKey(label))
}

// firstKey is the first of the keys a label joins with "/". A "/" that
// opens the label is the search key itself, not a separator.
func firstKey(label string) string {
	if len(label) > 1 {
		if i := strings.Index(label[1:], "/"); i >= 0 {
			label = label[:i+1]
		}
	}
	return strings.TrimSpace(label)
}

// pressKey is the message a key named the way a legend draws it arrives
// as when typed.
func pressKey(name string) (tea.KeyMsg, bool) {
	if key, drawn := legendGlyphKeys[name]; drawn {
		name = key
	}
	alt := false
	if rest, ok := strings.CutPrefix(name, "alt+"); ok && rest != "" {
		alt, name = true, rest
	}
	if keyType, ok := namedKeyTypes[name]; ok {
		key := tea.KeyMsg{Type: keyType, Alt: alt}
		if keyType == tea.KeySpace {
			key.Runes = []rune{' '}
		}
		return key, true
	}
	if runes := []rune(name); len(runes) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes, Alt: alt}, true
	}
	return tea.KeyMsg{}, false
}

// legendGlyphKeys reads back the glyphs a legend draws for named keys.
var legendGlyphKeys = map[string]string{"↵": "enter", "⌫": "backspace", "↑": "up", "↓": "down", "←": "left", "→": "right"}

// namedKeyTypes is every key Bubble Tea names, under the spelling a key
// table stores.
var namedKeyTypes = func() map[string]tea.KeyType {
	types := map[string]tea.KeyType{}
	for keyType := tea.KeyF20; keyType <= tea.KeyBackspace; keyType++ {
		if name := keyType.String(); name != "" && keyType != tea.KeyRunes {
			types[keybind.Normalize(name)] = keyType
		}
	}
	return types
}()
