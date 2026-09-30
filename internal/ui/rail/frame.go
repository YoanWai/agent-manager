package rail

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	railGutter      = 2
	railInset       = railGutter - 1
	menuButtonWidth = 4
	ReorderGlyph    = "⠿"
	MenuGlyph       = "[…]"
	reorderGrip     = ReorderGlyph
	shellGlyph      = "❯"
)

// Theme is the feature-local copy of the active root presentation tokens.
type Theme struct {
	Bg, Surface, Overlay, Border        string
	Bright, Text, Dim, Subtle           string
	Accent, Accent2                     string
	Working, Waiting, Finished, Errored string
	Idle                                string
}

// Rename is a root-owned editor rendered in the row named by Target.
// View is already measured by the root widget; Rail only places it.
type Rename struct {
	Target Selection
	View   string
}

// Line is one prepared rail row. Root composes these lines with its edge,
// seam, footer, and active content column without re-running Rail layout.
type Line struct {
	Text  string
	Tone  string
	Rule  bool
	Raw   bool
	Hit   Selection
	HitOK bool
}

type Range struct {
	Start int
	End   int
}

type Rect struct {
	Left, Top, Width, Height int
}

type RenderContext struct {
	Width           int
	Height          int
	TerminalWidth   int
	TerminalHeight  int
	BodyOriginY     int
	Theme           Theme
	ListKeys        keybind.Table
	ComfortableRows bool
	MouseDisabled   bool
	Focused         bool
	EnterFocuses    bool
	StartupPhase    int
	CursorMarker    string
	Rename          Rename
	Foot            []Line
}

// Frame is the immutable geometry and content prepared for one display.
// The following input receives this exact value.
type Frame struct {
	Lines     []Line
	Window    Range
	Width     int
	Handles   map[string]int
	Menu      []string
	MenuRect  Rect
	FootStart int
	FootLines int
	visible   map[string]bool
}

func (f Frame) Hits() []Selection {
	hits := make([]Selection, 0, len(f.Lines))
	for _, line := range f.Lines {
		if line.HitOK {
			hits = append(hits, line.Hit)
		}
	}
	return hits
}

func (f Frame) RowAt(x, y, dividerX int, fullWidth bool) (Selection, bool) {
	if !fullWidth && x >= dividerX {
		return Selection{}, false
	}
	if y < 0 || y >= len(f.Lines) {
		return Selection{}, false
	}
	line := f.Lines[y]
	return line.Hit, line.HitOK
}

func (f Frame) InWindow(selection Selection) bool {
	return f.visible[selection.key()]
}

// Overlay paints the already prepared menu over root's composed frame.
func (f Frame) Overlay(frame string) string {
	if len(f.Menu) == 0 {
		return frame
	}
	return overlayAt(frame, f.Menu, f.MenuRect.Left, f.MenuRect.Top)
}

type palette struct {
	theme                                           Theme
	section, muted, subtle, value, key              lipgloss.Style
	searchMatch, scopeBadge, focusBadge, inboxBadge lipgloss.Style
}

func newPalette(theme Theme) palette {
	return palette{
		theme:       theme,
		section:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(theme.Accent)),
		muted:       lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Dim)),
		subtle:      lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Subtle)),
		value:       lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Text)),
		key:         lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent)).Bold(true),
		searchMatch: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Bg)).Background(lipgloss.Color(theme.Accent)),
		scopeBadge:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Bg)).Background(lipgloss.Color(theme.Accent2)).Bold(true).Padding(0, 1),
		focusBadge:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Bg)).Background(lipgloss.Color(theme.Accent)).Bold(true),
		inboxBadge:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent)).Bold(true),
	}
}

func (p palette) panel() string       { return mix(p.theme.Bg, p.theme.Surface, 0.55) }
func (p palette) selected() string    { return p.theme.Surface }
func (p palette) searchField() string { return mix(p.panel(), p.theme.Accent, 0.2) }
func (p palette) lifted() string      { return mix(p.theme.Surface, p.theme.Accent, 0.3) }
func (p palette) drop() string        { return mix(p.theme.Surface, p.theme.Accent2, 0.3) }
func (p palette) restingMark() string { return mix(p.theme.Bg, p.theme.Text, 0.3) }

func (p palette) statusColor(state string) lipgloss.Color {
	switch state {
	case "working", "starting":
		return lipgloss.Color(p.theme.Working)
	case "waiting":
		return lipgloss.Color(p.theme.Waiting)
	case "finished":
		return lipgloss.Color(p.theme.Finished)
	case "errored", "dead":
		return lipgloss.Color(p.theme.Errored)
	default:
		return lipgloss.Color(p.theme.Idle)
	}
}

func statusGlyph(state string) string {
	switch state {
	case "working":
		return "◐"
	case "starting":
		return "◌"
	case "waiting":
		return "◆"
	case "finished":
		return "●"
	case "errored", "dead":
		return "✕"
	default:
		return "○"
	}
}

func statusLabel(state string) string {
	if state == "starting" {
		return "starting up"
	}
	return state
}

func (p palette) highlight(name, query string, base lipgloss.Style) string {
	want := []rune(strings.TrimSpace(query))
	runes := []rune(name)
	if len(want) == 0 {
		return base.Render(name)
	}
	for start := 0; start+len(want) <= len(runes); start++ {
		end := start + len(want)
		if strings.EqualFold(string(runes[start:end]), string(want)) {
			return base.Render(string(runes[:start])) + p.searchMatch.Render(string(runes[start:end])) + base.Render(string(runes[end:]))
		}
	}
	return base.Render(name)
}

func (p palette) keyCap(key, label string) string {
	return p.key.Render(key) + " " + p.muted.Render(label)
}

func (p palette) inbox(count int) string {
	return p.inboxBadge.Render("✉" + strconv.Itoa(count))
}

func paint(text string, width int, bg string) string {
	if width <= 0 {
		return ""
	}
	text = ansi.Truncate(text, width, "")
	padding := max(width-ansi.StringWidth(text), 0)
	if bg == "" {
		return text + strings.Repeat(" ", padding)
	}
	return lipgloss.NewStyle().Background(lipgloss.Color(bg)).Render(text + strings.Repeat(" ", padding))
}

func rowColumns(lead, meta string, width int) string {
	if meta == "" {
		return lead
	}
	const gap = 2
	leadWidth, metaWidth := ansi.StringWidth(lead), ansi.StringWidth(meta)
	if width < 1 || leadWidth+gap+metaWidth > width {
		return lead + strings.Repeat(" ", gap) + meta
	}
	return lead + strings.Repeat(" ", width-leadWidth-metaWidth) + meta
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

func relSince(at time.Time) string {
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()/5)*5)
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func centerLine(text string, width int) string {
	if width <= 0 {
		return text
	}
	visible := ansi.StringWidth(text)
	if visible >= width {
		return ansi.Truncate(text, width, "…")
	}
	return strings.Repeat(" ", (width-visible)/2) + text
}

func overlayAt(frame string, overlay []string, left, top int) string {
	lines := strings.Split(frame, "\n")
	for row, over := range overlay {
		y := top + row
		if y < 0 || y >= len(lines) {
			continue
		}
		line := lines[y]
		before := ansi.Truncate(line, max(left, 0), "")
		if ansi.StringWidth(before) < left {
			before = presentation.PadRight(before, left)
		}
		afterAt := left + ansi.StringWidth(over)
		after := ""
		if afterAt < ansi.StringWidth(line) {
			after = ansi.TruncateLeft(line, afterAt, "")
		}
		lines[y] = before + over + after
	}
	return strings.Join(lines, "\n")
}

func mix(a, b string, ratio float64) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	parse := func(hex string) (int, int, int) {
		hex = strings.TrimPrefix(hex, "#")
		if len(hex) != 6 {
			return 0, 0, 0
		}
		value, _ := strconv.ParseInt(hex, 16, 64)
		return int(value >> 16), int(value >> 8 & 0xff), int(value & 0xff)
	}
	ar, ag, ab := parse(a)
	br, bg, bb := parse(b)
	blend := func(x, y int) int { return int(float64(x)*(1-ratio) + float64(y)*ratio + 0.5) }
	return fmt.Sprintf("#%02x%02x%02x", blend(ar, br), blend(ag, bg), blend(ab, bb))
}
