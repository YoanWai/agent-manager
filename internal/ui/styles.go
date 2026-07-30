package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Colors are resolved from the active Theme by applyTheme; nothing here
// hardcodes a palette. See theme.go for the token meanings.
var (
	colorBg      lipgloss.Color
	colorSurface lipgloss.Color
	colorOverlay lipgloss.Color
	colorBorder  lipgloss.Color
	colorBright  lipgloss.Color
	colorText    lipgloss.Color
	colorDim     lipgloss.Color
	colorSubtle  lipgloss.Color
	colorAccent  lipgloss.Color
	colorAccent2 lipgloss.Color
	colorSelBg   lipgloss.Color

	colorWorking  lipgloss.Color
	colorWaiting  lipgloss.Color
	colorFinished lipgloss.Color
	colorErrored  lipgloss.Color
	colorIdle     lipgloss.Color
)

var (
	sectionStyle     lipgloss.Style
	selectedRowStyle lipgloss.Style

	mutedStyle  lipgloss.Style
	subtleStyle lipgloss.Style
	valueStyle  lipgloss.Style
	labelStyle  lipgloss.Style
	errStyle    lipgloss.Style
	keyStyle    lipgloss.Style

	annotationStyle lipgloss.Style
	scopeBadgeStyle lipgloss.Style
	focusBadgeStyle lipgloss.Style
	focusEdgeStyle  lipgloss.Style

	chipStyle             lipgloss.Style
	imageChipStyle        lipgloss.Style
	imageChipPastingStyle lipgloss.Style
)

func init() { applyTheme(themes[0]) }

// rebuildStyles re-derives every style from the colors applyTheme just set.
func rebuildStyles() {
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)

	selectedRowStyle = lipgloss.NewStyle().Background(colorSelBg).Foreground(colorBright)

	mutedStyle = lipgloss.NewStyle().Foreground(colorDim)
	subtleStyle = lipgloss.NewStyle().Foreground(colorSubtle)
	valueStyle = lipgloss.NewStyle().Foreground(colorText)
	labelStyle = lipgloss.NewStyle().Foreground(colorSubtle)
	errStyle = lipgloss.NewStyle().Foreground(colorErrored).Bold(true)
	keyStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	annotationStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	scopeBadgeStyle = lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent2).Bold(true).Padding(0, 1)
	focusBadgeStyle = lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent).Bold(true)
	focusEdgeStyle = lipgloss.NewStyle().Foreground(colorAccent)

	chipStyle = lipgloss.NewStyle().Background(colorSurface).Padding(0, 1)
	imageChipStyle = lipgloss.NewStyle().
		Foreground(colorBright).
		Background(colorSurface)
	imageChipPastingStyle = lipgloss.NewStyle().
		Foreground(colorWorking).
		Background(colorSurface)
}

// renderSelectedRow wraps a pre-styled line with the selected row's
// background and foreground. Internal SGR resets emitted by per-segment
// lipgloss.Render calls (and padRight) would otherwise clear the outer
// background after the first segment, leaving only the bar glyph tinted.
// Re-applying the selected bg+fg after every reset keeps the row tinted
// end-to-end.
func renderSelectedRow(s string) string {
	reapply := "\x1b[0m" + bgSeq(current.Surface) + fgSeq(current.Bright)
	return selectedRowStyle.Render(strings.ReplaceAll(s, "\x1b[0m", reapply))
}

// annotationBg is the wash behind a reviewer's inline comment: the theme's
// backdrop lifted toward the accent, so the note reads as ours rather than
// as part of the diff. Resolved per call so it follows the live color
// profile as well as the live theme.
func annotationBg() string {
	return bgSeq(mix(current.Bg, current.Accent, 0.18))
}

func statusColor(s string) lipgloss.Color {
	switch s {
	case status.Working, status.Starting:
		return colorWorking
	case status.Waiting:
		return colorWaiting
	case status.Finished:
		return colorFinished
	case status.Errored, status.Dead:
		return colorErrored
	default:
		return colorIdle
	}
}

// statusGlyph is one geometric mark per state, all from the same weight
// family so a column of them reads as a single scale rather than a mix of
// punctuation. Shape carries the state; color reinforces it.
func statusGlyph(s string) string {
	switch s {
	case status.Working:
		return "◐"
	case status.Starting:
		return "◌"
	case status.Waiting:
		return "◆"
	case status.Finished:
		return "●"
	case status.Errored, status.Dead:
		return "✕"
	default:
		return "○"
	}
}

// statusLabel is the human text for a status; most match the raw value, but
// the transient launch state reads better spelled out.
func statusLabel(s string) string {
	if s == status.Starting {
		return "starting up"
	}
	return s
}

// padRight pads or clips a possibly-styled string to an exact display width.
func padRight(s string, width int) string {
	w := ansi.StringWidth(s)
	if w > width {
		s = ansi.Truncate(s, width, "…")
		w = ansi.StringWidth(s)
	}
	if w < width {
		s += strings.Repeat(" ", width-w)
	}
	if strings.ContainsRune(s, 0x1b) {
		s += "\x1b[0m"
	}
	return s
}

// gaugeGlyph is the meter's bar unit: a heavy rule reads as a slim
// continuous line rather than a row of stacked blocks.
const (
	gaugeGlyph = "━"
	gaugeHalf  = "╸"
)

// gauge renders a meter for a 0-100 percentage: a colored run over a muted
// track, the color ramping from calm to alarming as it fills, with a
// half-width cap so small changes still move the bar.
func gauge(percent float64, width int) string {
	if width < 1 {
		width = 1
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	units := percent / 100 * float64(width)
	filled := int(units)
	if filled > width {
		filled = width
	}
	half := units-float64(filled) >= 0.5

	color := lipgloss.Color(gaugeRamp(percent))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(gaugeGlyph, filled))
	rest := width - filled
	if half && rest > 0 {
		bar += lipgloss.NewStyle().Foreground(color).Render(gaugeHalf)
		rest--
	}
	track := lipgloss.NewStyle().Foreground(colorOverlay).Render(strings.Repeat(gaugeGlyph, rest))
	return bar + track
}

// gaugeRamp blends a meter from the theme's calm color to its alarm color
// as the load climbs. The anchors are deliberately "finished" and "errored"
// rather than any state color: a machine meter is a temperature, and a
// theme whose working color is blue would otherwise paint a cold gauge.
func gaugeRamp(percent float64) string {
	if percent >= 85 {
		return current.Errored
	}
	return mix(current.Finished, current.Errored, percent/85)
}

// pill renders a small label chip: tinted text on the surface fill, so a
// row of them reads as tokens rather than as more prose.
func pill(text string, fg lipgloss.Color) string {
	return chipStyle.Foreground(fg).Render(text)
}

// keyPill renders a chip with the key that changes it dimmed in front, so
// the header doubles as a key legend: each changeable value wears its
// shortcut. The key is dim enough to lose to the value at a glance but
// reads clearly when hunted.
func keyPill(key, text string, fg lipgloss.Color) string {
	return subtleStyle.Render(key+" ") + pill(text, fg)
}

// keyCap renders one footer binding: the key in accent, its action muted.
func keyCap(key, label string) string {
	return keyStyle.Render(key) + " " + mutedStyle.Render(label)
}

// imageChip tints a pasted-image token. Color only: the token's own
// characters are what the textarea measured when it wrapped the line.
func imageChip(token string) string {
	return imageChipStyle.Render(token)
}

// imageChipPasting tints a chip whose clipboard read is still running.
func imageChipPasting(token string) string {
	return imageChipPastingStyle.Render(token)
}
