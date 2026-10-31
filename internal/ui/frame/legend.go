package frame

import (
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A Legend is the app's key map made visible: tiers of bindings, each tier
// named for what its keys act on, so the footer answers "what can I do to
// the thing under the cursor" before it answers "what keys exist".
const (
	// TitleColumn keeps every tier's first binding on one column, so
	// stacked tiers read as a table rather than as ragged prose.
	TitleColumn = 11
	Gap         = 2
	// MaxRows is the titled footer's height budget. Past it the tail
	// is cut and marked; the full map is one ? away. A footer too narrow for
	// titles may take more rows, up to half the body, so every key stays
	// painted.
	MaxRows    = 3
	RailGutter = 2
)

// LegendMode is how much of each binding a footer paints. A footer steps
// down in this order as the terminal narrows, never back.
type LegendMode int

const (
	// Titled names each tier and paints every part whole.
	Titled LegendMode = iota
	// Worded drops the titles and every part a click cannot press.
	Worded
	// Bare paints those keys without their words.
	Bare
)

// Section is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type Section struct {
	Title       string
	Leads       [][2]string
	Alerts      [][2]string
	Hint        string
	Pairs       [][2]string
	Quiet       bool
	ActionPairs []bool
}

type PartKind int

const (
	Inferred PartKind = iota
	Action
	Gesture
)

// Part is one painted binding and the key label a click on it
// presses; label is empty for a part that only reads, like a tier's hint.
type Part struct {
	Text  string
	Label string
	Bare  string
	Kind  PartKind
}

func (p Part) clickable() bool {
	_, ok := p.key()
	return ok
}

func (p Part) key() (tea.KeyMsg, bool) {
	if p.Kind == Gesture {
		return tea.KeyMsg{}, false
	}
	if p.Kind == Action {
		if key, ok := pressKey(strings.TrimSpace(p.Label)); ok {
			return key, true
		}
		return pressKey(firstKey(p.Label))
	}
	return LegendKey(p.Label)
}

func (hits KeyHits) addPart(x0, x1, y int, part Part) KeyHits {
	key, ok := part.key()
	if !ok {
		return hits
	}
	return append(hits, KeyHit{X0: x0, X1: x1, Y: y, Key: key})
}

// bareKey is the first key a label joins with " / ", as a bare footer
// paints it.
func bareKey(label string) string {
	key, _, _ := strings.Cut(label, " / ")
	return strings.TrimSpace(key)
}

func (s Section) Parts(cap Cap) []Part {
	var parts []Part
	for _, lead := range s.Leads {
		if lead[0] != "" {
			parts = append(parts, Part{cap.leadCap(presentation.KeyLabel(cap.Mode, lead[0]), lead[1]), lead[0], cap.LeadKey.Render(presentation.KeyLabel(cap.Mode, bareKey(lead[0]))), Action})
		}
	}
	for _, alert := range s.Alerts {
		if alert[0] != "" {
			parts = append(parts, Part{cap.alertCap(presentation.KeyLabel(cap.Mode, alert[0]), alert[1]), alert[0], cap.Alert.Render(presentation.KeyLabel(cap.Mode, bareKey(alert[0]))), Action})
		}
	}
	if s.Hint != "" {
		parts = append(parts, Part{Text: cap.Muted.Render(s.Hint)})
	}
	for i, pair := range s.Pairs {
		kind := Inferred
		if s.ActionPairs != nil {
			kind = Gesture
			if s.ActionPairs[i] {
				kind = Action
			}
		}
		bare := cap.Key.Render(presentation.KeyLabel(cap.Mode, bareKey(pair[0])))
		if s.Quiet {
			parts = append(parts, Part{cap.quietCap(presentation.KeyLabel(cap.Mode, pair[0]), pair[1]), pair[0], bare, kind})
		} else {
			parts = append(parts, Part{cap.keyCap(presentation.KeyLabel(cap.Mode, pair[0]), pair[1]), pair[0], bare, kind})
		}
	}
	return parts
}

// Legend is a footer as painted: its text, and where each key it names
// landed, from its own first row and column.
type Legend struct {
	Text string
	Hits KeyHits
}

// PaintLegend renders a Legend as the app's footer in MaxRows rows:
// titled where every tier's first key fits beside its title, untitled
// otherwise.
func PaintLegend(sections []Section, width int, cap Cap, title, quietTitle func(string) string, sep string) Legend {
	layout, rows := LegendLayout([][]Section{sections}, width, MaxRows, cap, title, quietTitle, sep)
	if layout == Titled {
		bar, _ := PaintTitledLegend(sections, width, cap, title, quietTitle, sep)
		return bar
	}
	return PaintUntitledLegend(sections, width, cap, layout, rows)
}

// LegendLayout is the mode and row budget that paint every footer in refs:
// titled when each paints its tiers' first keys whole, worded when each
// paints every key whole in room rows, bare otherwise, cut past room.
func LegendLayout(refs [][]Section, width, room int, cap Cap, title, quietTitle func(string) string, sep string) (LegendMode, int) {
	titled := true
	for _, ref := range refs {
		if _, fits := PaintTitledLegend(ref, width, cap, title, quietTitle, sep); !fits {
			titled = false
			break
		}
	}
	if titled {
		return Titled, MaxRows
	}
	worded := 0
	for _, ref := range refs {
		rows := untitledRows(ref, width, cap, Worded)
		if rows < 0 || rows > room {
			worded = -1
			break
		}
		worded = max(worded, rows)
	}
	if worded >= 0 {
		return Worded, worded
	}
	bare := 0
	for _, ref := range refs {
		bare = max(bare, untitledRows(ref, width, cap, Bare))
	}
	return Bare, min(bare, room)
}

// PaintTitledLegend renders one tier per line where the terminal allows it
// and the tail marked when it does not. fits reports whether each tier with
// a key a click can press painted its first such key whole.
func PaintTitledLegend(sections []Section, width int, cap Cap, title, quietTitle func(string) string, sep string) (Legend, bool) {
	indent := strings.Repeat(" ", min(RailGutter, max(width, 0)))
	more := cap.Muted.Render("…")

	var out []string
	var hits KeyHits
	fits := true
	for i, section := range sections {
		parts := section.Parts(cap)
		pending := slices.ContainsFunc(parts, Part.clickable)
		if len(parts) == 0 || len(out) >= MaxRows {
			fits = fits && !pending
			continue
		}
		maxRows := MaxRows
		for _, next := range sections[i+1:] {
			if len(next.Parts(cap)) > 0 {
				maxRows--
			}
		}
		titleLabel := title(section.Title)
		if section.Quiet {
			titleLabel = quietTitle(section.Title)
		}
		column := min(max(TitleColumn, ansi.StringWidth(titleLabel)+Gap), max(width-ansi.StringWidth(indent)-2, 0))
		head := indent + presentation.PadRight(titleLabel, column)
		cont := indent + strings.Repeat(" ", column)
		line, lineWidth, started := head, ansi.StringWidth(head), false
		cut := false
		gap := strings.Repeat(" ", Gap)
		if section.Quiet {
			gap = sep
		}
		gapWidth := ansi.StringWidth(gap)
		for _, part := range parts {
			full := ansi.StringWidth(part.Text)
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
			text := ansi.Truncate(part.Text, max(avail-x, 0), "…")
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
	return Legend{Text: strings.Join(out, "\n"), Hits: hits}, fits
}

// untitledTiers is each tier's parts a click can press, as mode paints them.
func untitledTiers(sections []Section, cap Cap, mode LegendMode) [][]Part {
	tiers := make([][]Part, len(sections))
	for i, section := range sections {
		for _, part := range section.Parts(cap) {
			if !part.clickable() {
				continue
			}
			if mode == Bare {
				part = Part{Text: part.Bare, Label: bareKey(part.Label), Kind: part.Kind}
			}
			tiers[i] = append(tiers[i], part)
		}
	}
	return tiers
}

// untitledRows is the rows an untitled footer needs to paint every key
// whole, or -1 in worded mode when one key is wider than a row.
func untitledRows(sections []Section, width int, cap Cap, mode LegendMode) int {
	indentWidth := min(RailGutter, max(width, 0))
	rows := 0
	for _, tier := range untitledTiers(sections, cap, mode) {
		lineWidth := indentWidth
		for i, part := range tier {
			partWidth := ansi.StringWidth(part.Text)
			if mode == Worded && partWidth > width-indentWidth {
				return -1
			}
			switch {
			case i == 0:
				rows++
				lineWidth += partWidth
			case lineWidth+Gap+partWidth <= width:
				lineWidth += Gap + partWidth
			default:
				rows++
				lineWidth = indentWidth + partWidth
			}
		}
	}
	return rows
}

// PaintNarrowLegend renders sections untitled in a footer's mode and rows,
// bare where their words would not fit those rows.
func PaintNarrowLegend(sections []Section, width int, cap Cap, mode LegendMode, rows int) Legend {
	if mode == Worded {
		if need := untitledRows(sections, width, cap, mode); need < 0 || need > rows {
			mode = Bare
		}
	}
	return PaintUntitledLegend(sections, width, cap, mode, rows)
}

// PaintUntitledLegend renders the keys of each tier, worded or bare, from
// the gutter in at most rows rows. Each tier keeps a row for every tier
// after it, and a tier cut short ends in a marker.
func PaintUntitledLegend(sections []Section, width int, cap Cap, mode LegendMode, rows int) Legend {
	indentWidth := min(RailGutter, max(width, 0))
	indent := strings.Repeat(" ", indentWidth)
	gap := strings.Repeat(" ", Gap)
	more := cap.Muted.Render("…")
	markWidth := 1 + ansi.StringWidth(more)
	tiers := untitledTiers(sections, cap, mode)

	var out []string
	var hits KeyHits
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
					rest += Gap
				}
				rest += ansi.StringWidth(part.Text)
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
			case lineWidth+Gap+ansi.StringWidth(part.Text) <= avail(lineWidth+Gap, j):
				x = lineWidth + Gap
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
			text := ansi.Truncate(part.Text, max(avail(x, j)-x, 0), "…")
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
	return Legend{Text: strings.Join(out, "\n"), Hits: hits}
}

// LegendInline renders one tier as a single untitled run, for the foot of a
// modal card where the tier's subject is the card's own title.
func LegendInline(pairs [][2]string, width int, cap Cap) string {
	return PaintInline(pairs, width, cap).Text
}

func PaintInline(pairs [][2]string, width int, cap Cap) Legend {
	gap := strings.Repeat(" ", Gap)
	var lines []string
	var parts []string
	var hits KeyHits
	lineWidth := 0
	for _, pair := range pairs {
		part := cap.hintCap(pair[0], pair[1])
		partWidth := ansi.StringWidth(part)
		if len(parts) > 0 && lineWidth+Gap+partWidth > width {
			lines = append(lines, strings.Join(parts, gap))
			parts, lineWidth = nil, 0
		}
		if len(parts) > 0 {
			lineWidth += Gap
		}
		hits = hits.Add(lineWidth, lineWidth+partWidth, len(lines), pair[0])
		parts = append(parts, part)
		lineWidth += partWidth
	}
	lines = append(lines, strings.Join(parts, gap))
	return Legend{Text: strings.Join(lines, "\n"), Hits: hits}
}

// KeyHit is a key pair as painted: a left press inside it presses Key.
type KeyHit struct {
	X0, X1, Y int
	Key       tea.KeyMsg
}

type KeyHits []KeyHit

// Add records a part painted across columns [x0, x1) of row y, when its
// label names a key a click can press.
func (hits KeyHits) Add(x0, x1, y int, label string) KeyHits {
	key, ok := LegendKey(label)
	if !ok {
		return hits
	}
	return append(hits, KeyHit{X0: x0, X1: x1, Y: y, Key: key})
}

// At is the hit under column x of row y.
func (hits KeyHits) At(x, y int) (KeyHit, bool) {
	for _, hit := range hits {
		if y == hit.Y && x >= hit.X0 && x < hit.X1 {
			return hit, true
		}
	}
	return KeyHit{}, false
}

// Above keeps the hits on the rows a frame clamped to height still shows.
func (hits KeyHits) Above(height int) KeyHits {
	kept := hits[:0]
	for _, hit := range hits {
		if hit.Y < height {
			kept = append(kept, hit)
		}
	}
	return kept
}

// Moved is hits painted with their first row at y and first column at x.
func (hits KeyHits) Moved(x, y int) KeyHits {
	out := make(KeyHits, len(hits))
	for i, hit := range hits {
		out[i] = KeyHit{X0: hit.X0 + x, X1: hit.X1 + x, Y: hit.Y + y, Key: hit.Key}
	}
	return out
}
