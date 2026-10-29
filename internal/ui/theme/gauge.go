package theme

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

const gaugeHalf = "╸"

// Gauge renders a meter for a 0-100 percentage: a colored run over a muted
// track, the color ramping from calm to alarming as it fills, with a
// half-width cap in Advanced mode. Compatible rounds to whole cells.
// invert flips which end reads as alarming — a full disk is bad, but a full
// battery is exactly what you want, so battery colors off how empty it is.
func Gauge(percent float64, width int, invert bool, mode presentation.GlyphMode) string {
	if width < 1 {
		width = 1
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	// The explicit conversion rounds here, so arm64 cannot fuse this into the half check below.
	units := float64(percent / 100 * float64(width))
	filled := int(units)
	if filled > width {
		filled = width
	}
	half := units-float64(filled) >= 0.5
	glyph := presentation.ChromeFor(mode).RuleGlyph
	if mode.Effective() == presentation.GlyphModeBasic {
		if half {
			filled = min(filled+1, width)
		}
		half = false
	}

	colorPercent := percent
	if invert {
		colorPercent = 100 - percent
	}
	color := lipgloss.Color(GaugeRamp(colorPercent))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(glyph, filled))
	rest := width - filled
	if half && rest > 0 {
		bar += lipgloss.NewStyle().Foreground(color).Render(gaugeHalf)
		rest--
	}
	track := lipgloss.NewStyle().Foreground(ColorOverlay()).Render(strings.Repeat(glyph, rest))
	return bar + track
}

// GaugeRamp blends a meter from the theme's calm color to its alarm color
// as the load climbs. The anchors are deliberately "finished" and "errored"
// rather than any state color: a machine meter is a temperature, and a
// theme whose working color is blue would otherwise paint a cold gauge.
func GaugeRamp(percent float64) string {
	if percent >= 85 {
		return Current().Errored
	}
	return Mix(Current().Finished, Current().Errored, percent/85)
}
