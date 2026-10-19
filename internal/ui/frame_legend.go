package ui

import (
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
	// legendMaxRows is the footer's height budget. Past it the tail is cut
	// and marked; the full map is one ? away.
	legendMaxRows = 3
)

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

// legendPart is one painted binding and the key label a click on it
// presses; label is empty for a part that only reads, like a tier's hint.
type legendPart struct {
	text  string
	label string
}

func (s legendSection) parts() []legendPart {
	var parts []legendPart
	for _, lead := range s.leads {
		if lead[0] != "" {
			parts = append(parts, legendPart{keyCapLead(lead[0], lead[1]), lead[0]})
		}
	}
	for _, alert := range s.alerts {
		if alert[0] != "" {
			parts = append(parts, legendPart{keyCapAlert(alert[0], alert[1]), alert[0]})
		}
	}
	if s.hint != "" {
		parts = append(parts, legendPart{text: mutedStyle().Render(s.hint)})
	}
	for _, pair := range s.pairs {
		if s.quiet {
			parts = append(parts, legendPart{keyCapQuiet(pair[0], pair[1]), pair[0]})
		} else {
			parts = append(parts, legendPart{keyCap(pair[0], pair[1]), pair[0]})
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

// paintLegend renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func paintLegend(sections []legendSection, width int) legend {
	indent := strings.Repeat(" ", railGutter)
	sep := subtleStyle().Render(" · ")
	more := subtleStyle().Render("…")

	var out []string
	var hits keyHits
	for i, section := range sections {
		parts := section.parts()
		if len(parts) == 0 || len(out) >= legendMaxRows {
			continue
		}
		maxRows := legendMaxRows
		for _, next := range sections[i+1:] {
			if len(next.parts()) > 0 {
				maxRows--
			}
		}
		title := legendBadgeStyle().Render(section.title)
		if section.quiet {
			title = legendTitleStyle().Render(section.title)
		}
		column := max(legendTitleColumn, ansi.StringWidth(title)+legendGap)
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
			partWidth := ansi.StringWidth(part.text)
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
				line, lineWidth, started = line+part.text, lineWidth+partWidth, true
			case lineWidth+gapWidth+partWidth <= avail:
				x = lineWidth + gapWidth
				line += gap + part.text
				lineWidth += gapWidth + partWidth
			case len(out) < maxRows-1:
				out = append(out, line)
				x = ansi.StringWidth(cont)
				line, lineWidth = cont+part.text, x+partWidth
			default:
				cut = true
			}
			if cut {
				break
			}
			hits = hits.add(x, x+partWidth, len(out), part.label)
		}
		if cut {
			line += " " + more
		}
		out = append(out, line)
	}
	return legend{text: strings.Join(out, "\n"), hits: hits}
}

// legendInline renders one tier as a single untitled run, for the foot of a
// modal card where the tier's subject is the card's own title.
func legendInline(pairs [][2]string, width int) string {
	return paintInline(pairs, width).text
}

func paintInline(pairs [][2]string, width int) legend {
	gap := strings.Repeat(" ", legendGap)
	var lines []string
	var parts []string
	var hits keyHits
	lineWidth := 0
	for _, pair := range pairs {
		part := hintCap(pair[0], pair[1])
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

// placePainted records keys a feature painted on row y, from column x.
func (m *Model) placePainted(keys []presentation.PaintedKey, x, y int) {
	var hits keyHits
	for _, key := range keys {
		hits = hits.add(key.X0, key.X1, 0, key.Label)
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
var legendGlyphKeys = map[string]string{"↵": "enter", "⌫": "backspace"}

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
