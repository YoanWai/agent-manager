package frame

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A legend is the app's key map made visible: tiers of bindings, each tier
// named for what its keys act on, so the footer answers "what can I do to
// the thing under the cursor" before it answers "what keys exist".
const (
	// TitleColumn keeps every tier's first binding on one column, so stacked
	// tiers read as a table rather than as ragged prose.
	TitleColumn = 11
	Gap         = 2
	// MaxRows is the footer's height budget. Past it the tail is cut and
	// marked; the full map is one ? away.
	MaxRows    = 3
	RailGutter = 2
)

// Section is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type Section struct {
	Title  string
	Leads  [][2]string
	Alerts [][2]string
	Hint   string
	Pairs  [][2]string
	Quiet  bool
}

// Part is one painted binding and the key label a click on it presses; label
// is empty for a part that only reads, like a tier's hint.
type Part struct {
	Text  string
	Label string
}

func (s Section) Parts(cap Cap) []Part {
	var parts []Part
	for _, lead := range s.Leads {
		if lead[0] != "" {
			parts = append(parts, Part{cap.leadCap(lead[0], lead[1]), lead[0]})
		}
	}
	for _, alert := range s.Alerts {
		if alert[0] != "" {
			parts = append(parts, Part{cap.alertCap(alert[0], alert[1]), alert[0]})
		}
	}
	if s.Hint != "" {
		parts = append(parts, Part{Text: cap.Muted.Render(s.Hint)})
	}
	for _, pair := range s.Pairs {
		if s.Quiet {
			parts = append(parts, Part{cap.quietCap(pair[0], pair[1]), pair[0]})
		} else {
			parts = append(parts, Part{cap.keyCap(pair[0], pair[1]), pair[0]})
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

// PaintLegend renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not. title paints
// a tier's name as its badge, quietTitle paints a secondary tier's, and sep
// joins the bindings of a secondary tier.
func PaintLegend(sections []Section, width int, cap Cap, title, quietTitle func(string) string, sep string) Legend {
	indent := strings.Repeat(" ", min(RailGutter, max(width, 0)))
	more := cap.Muted.Render("…")

	var out []string
	var hits KeyHits
	for i, section := range sections {
		parts := section.Parts(cap)
		if len(parts) == 0 || len(out) >= MaxRows {
			continue
		}
		maxRows := MaxRows
		for _, next := range sections[i+1:] {
			if len(next.Parts(cap)) > 0 {
				maxRows--
			}
		}
		heading := title(section.Title)
		if section.Quiet {
			heading = quietTitle(section.Title)
		}
		column := min(max(TitleColumn, ansi.StringWidth(heading)+Gap), max(width-ansi.StringWidth(indent)-2, 0))
		head := indent + presentation.PadRight(heading, column)
		cont := indent + strings.Repeat(" ", column)
		line, lineWidth, started := head, ansi.StringWidth(head), false
		cut := false
		gap := strings.Repeat(" ", Gap)
		if section.Quiet {
			gap = sep
		}
		gapWidth := ansi.StringWidth(gap)
		for _, part := range parts {
			partWidth := ansi.StringWidth(part.Text)
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
				hits = hits.Add(x, lineWidth, len(out), part.Label)
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
