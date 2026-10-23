package ui

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

// themeStyles is every style derived from one palette. applyTheme builds a
// whole set and publishes it with one atomic store, and nothing writes a set
// after that, so a Model rendering on one goroutine while another Model
// applies a theme reads either set, never a half-built mix. The accessors
// below are what the rest of the package reads; nothing hardcodes a palette.
// See theme.go for the token meanings.
type themeStyles struct {
	theme Theme

	section     lipgloss.Style
	selectedRow lipgloss.Style

	muted  lipgloss.Style
	subtle lipgloss.Style
	value  lipgloss.Style
	label  lipgloss.Style
	err    lipgloss.Style
	done   lipgloss.Style
	warn   lipgloss.Style
	key    lipgloss.Style

	annotation  lipgloss.Style
	scopeBadge  lipgloss.Style
	focusBadge  lipgloss.Style
	inboxBadge  lipgloss.Style
	searchMatch lipgloss.Style
	focusEdge   lipgloss.Style

	chip             lipgloss.Style
	imageChip        lipgloss.Style
	imageChipPasting lipgloss.Style

	legendTitle lipgloss.Style
	legendBadge lipgloss.Style
	legendAlert lipgloss.Style
	legendLabel lipgloss.Style
	leadKey     lipgloss.Style
	leadLabel   lipgloss.Style
}

// activeStyles is the published set. It is a package variable rather than a
// field on Model because the free render helpers (keyCap, pill, paint's
// tones, hrule) read it from everywhere; one atomic pointer keeps that
// reach without a data race. It starts on the default palette.
var activeStyles = publishStyles(themes[0])

func publishStyles(t Theme) *atomic.Pointer[themeStyles] {
	active := new(atomic.Pointer[themeStyles])
	active.Store(newThemeStyles(t))
	return active
}

// newThemeStyles derives every style from a token set.
func newThemeStyles(t Theme) *themeStyles {
	var (
		bg       = lipgloss.Color(t.Bg)
		surface  = lipgloss.Color(t.Surface)
		bright   = lipgloss.Color(t.Bright)
		text     = lipgloss.Color(t.Text)
		dim      = lipgloss.Color(t.Dim)
		subtle   = lipgloss.Color(t.Subtle)
		accent   = lipgloss.Color(t.Accent)
		accent2  = lipgloss.Color(t.Accent2)
		working  = lipgloss.Color(t.Working)
		finished = lipgloss.Color(t.Finished)
		errored  = lipgloss.Color(t.Errored)
		lead     = lipgloss.Color(mix(t.Bg, t.Accent, 0.28))
	)
	return &themeStyles{
		theme: t,

		section:     lipgloss.NewStyle().Bold(true).Foreground(accent),
		selectedRow: lipgloss.NewStyle().Background(surface).Foreground(bright),

		muted:  lipgloss.NewStyle().Foreground(dim),
		subtle: lipgloss.NewStyle().Foreground(subtle),
		value:  lipgloss.NewStyle().Foreground(text),
		label:  lipgloss.NewStyle().Foreground(subtle),
		err:    lipgloss.NewStyle().Foreground(errored).Bold(true),
		done:   lipgloss.NewStyle().Foreground(finished),
		warn:   lipgloss.NewStyle().Foreground(working).Bold(true),
		key:    lipgloss.NewStyle().Foreground(accent).Bold(true),

		annotation:  lipgloss.NewStyle().Foreground(accent).Bold(true),
		scopeBadge:  lipgloss.NewStyle().Foreground(bg).Background(accent2).Bold(true).Padding(0, 1),
		focusBadge:  lipgloss.NewStyle().Foreground(bg).Background(accent).Bold(true),
		searchMatch: lipgloss.NewStyle().Foreground(bg).Background(accent),
		// Foreground only: a fill would punch a chip through the band a
		// selected row paints behind it.
		inboxBadge: lipgloss.NewStyle().Foreground(accent).Bold(true),
		focusEdge:  lipgloss.NewStyle().Foreground(accent),

		chip: lipgloss.NewStyle().Background(surface).Padding(0, 1),
		imageChip: lipgloss.NewStyle().
			Foreground(bright).
			Background(surface),
		imageChipPasting: lipgloss.NewStyle().
			Foreground(working).
			Background(surface),

		legendTitle: lipgloss.NewStyle().Foreground(subtle).Bold(true),
		legendBadge: lipgloss.NewStyle().
			Foreground(bg).Background(accent).Bold(true).Padding(0, 1),
		legendAlert: lipgloss.NewStyle().
			Foreground(bg).Background(working).Bold(true).Padding(0, 1),
		legendLabel: lipgloss.NewStyle().Foreground(text),
		leadKey:     lipgloss.NewStyle().Foreground(accent).Background(lead).Bold(true).PaddingLeft(1),
		leadLabel:   lipgloss.NewStyle().Foreground(bright).Background(lead).Padding(0, 1),
	}
}

// current is the live token set; renderers that need a raw SGR sequence
// (rather than a lipgloss style) read their hex from here.
func current() Theme { return activeStyles.Load().theme }

func colorBg() lipgloss.Color       { return lipgloss.Color(current().Bg) }
func colorSurface() lipgloss.Color  { return lipgloss.Color(current().Surface) }
func colorOverlay() lipgloss.Color  { return lipgloss.Color(current().Overlay) }
func colorBorder() lipgloss.Color   { return lipgloss.Color(current().Border) }
func colorBright() lipgloss.Color   { return lipgloss.Color(current().Bright) }
func colorText() lipgloss.Color     { return lipgloss.Color(current().Text) }
func colorDim() lipgloss.Color      { return lipgloss.Color(current().Dim) }
func colorSubtle() lipgloss.Color   { return lipgloss.Color(current().Subtle) }
func colorAccent() lipgloss.Color   { return lipgloss.Color(current().Accent) }
func colorAccent2() lipgloss.Color  { return lipgloss.Color(current().Accent2) }
func colorRemote() lipgloss.Color   { return lipgloss.Color(current().Remote) }
func colorSelBg() lipgloss.Color    { return colorSurface() }
func colorWorking() lipgloss.Color  { return lipgloss.Color(current().Working) }
func colorWaiting() lipgloss.Color  { return lipgloss.Color(current().Waiting) }
func colorFinished() lipgloss.Color { return lipgloss.Color(current().Finished) }
func colorErrored() lipgloss.Color  { return lipgloss.Color(current().Errored) }
func colorIdle() lipgloss.Color     { return lipgloss.Color(current().Idle) }

func sectionStyle() lipgloss.Style          { return activeStyles.Load().section }
func selectedRowStyle() lipgloss.Style      { return activeStyles.Load().selectedRow }
func mutedStyle() lipgloss.Style            { return activeStyles.Load().muted }
func subtleStyle() lipgloss.Style           { return activeStyles.Load().subtle }
func valueStyle() lipgloss.Style            { return activeStyles.Load().value }
func labelStyle() lipgloss.Style            { return activeStyles.Load().label }
func errStyle() lipgloss.Style              { return activeStyles.Load().err }
func doneStyle() lipgloss.Style             { return activeStyles.Load().done }
func warnStyle() lipgloss.Style             { return activeStyles.Load().warn }
func keyStyle() lipgloss.Style              { return activeStyles.Load().key }
func annotationStyle() lipgloss.Style       { return activeStyles.Load().annotation }
func scopeBadgeStyle() lipgloss.Style       { return activeStyles.Load().scopeBadge }
func focusBadgeStyle() lipgloss.Style       { return activeStyles.Load().focusBadge }
func inboxBadgeStyle() lipgloss.Style       { return activeStyles.Load().inboxBadge }
func searchMatchStyle() lipgloss.Style      { return activeStyles.Load().searchMatch }
func focusEdgeStyle() lipgloss.Style        { return activeStyles.Load().focusEdge }
func chipStyle() lipgloss.Style             { return activeStyles.Load().chip }
func imageChipStyle() lipgloss.Style        { return activeStyles.Load().imageChip }
func imageChipPastingStyle() lipgloss.Style { return activeStyles.Load().imageChipPasting }
func legendTitleStyle() lipgloss.Style      { return activeStyles.Load().legendTitle }
func legendBadgeStyle() lipgloss.Style      { return activeStyles.Load().legendBadge }
func legendAlertStyle() lipgloss.Style      { return activeStyles.Load().legendAlert }
func legendLabelStyle() lipgloss.Style      { return activeStyles.Load().legendLabel }
func legendLeadKey() lipgloss.Style         { return activeStyles.Load().leadKey }
func legendLeadLabel() lipgloss.Style       { return activeStyles.Load().leadLabel }

// renderSelectedRow wraps a pre-styled line with the selected row's
// background and foreground. Internal SGR resets emitted by per-segment
// lipgloss.Render calls (and padRight) would otherwise clear the outer
// background after the first segment, leaving only the bar glyph tinted.
// Re-applying the selected bg+fg after every reset keeps the row tinted
// end-to-end.
func renderSelectedRow(s string) string {
	reapply := "\x1b[0m" + bgSeq(current().Surface) + fgSeq(current().Bright)
	return selectedRowStyle().Render(strings.ReplaceAll(s, "\x1b[0m", reapply))
}

// The accent lift separates reviewer notes from the diff itself. Resolve it
// per call so live theme and color-profile changes take effect immediately.
func annotationBg() string {
	return bgSeq(mix(current().Bg, current().Accent, 0.18))
}

func handledAnnotationBg() string {
	return bgSeq(mix(current().Bg, current().Finished, 0.14))
}

func statusColor(s string) lipgloss.Color {
	switch s {
	case status.Working, status.Starting:
		return colorWorking()
	case status.Waiting:
		return colorWaiting()
	case status.Finished:
		return colorFinished()
	case status.Errored, status.Dead:
		return colorErrored()
	default:
		return colorIdle()
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
	return presentation.PadRight(s, width)
}

// gaugeGlyph is the meter's bar unit: a heavy rule reads as a slim
// continuous line rather than a row of stacked blocks.
const (
	gaugeGlyph = "━"
	gaugeHalf  = "╸"
)

// gauge renders a meter for a 0-100 percentage: a colored run over a muted
// track, the color ramping from calm to alarming as it fills, with a
// half-width cap so small changes still move the bar. invert flips which
// end of the scale reads as alarming — a full disk is bad, but a full
// battery is exactly what you want, so battery colors off how empty it is.
func gauge(percent float64, width int, invert bool) string {
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

	colorPercent := percent
	if invert {
		colorPercent = 100 - percent
	}
	color := lipgloss.Color(gaugeRamp(colorPercent))
	bar := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat(gaugeGlyph, filled))
	rest := width - filled
	if half && rest > 0 {
		bar += lipgloss.NewStyle().Foreground(color).Render(gaugeHalf)
		rest--
	}
	track := lipgloss.NewStyle().Foreground(colorOverlay()).Render(strings.Repeat(gaugeGlyph, rest))
	return bar + track
}

// gaugeRamp blends a meter from the theme's calm color to its alarm color
// as the load climbs. The anchors are deliberately "finished" and "errored"
// rather than any state color: a machine meter is a temperature, and a
// theme whose working color is blue would otherwise paint a cold gauge.
func gaugeRamp(percent float64) string {
	if percent >= 85 {
		return current().Errored
	}
	return mix(current().Finished, current().Errored, percent/85)
}

// pill renders a small label chip: tinted text on the surface fill, so a
// row of them reads as tokens rather than as more prose.
func pill(text string, fg lipgloss.Color) string {
	return chipStyle().Foreground(fg).Render(text)
}

// inboxBadge marks a session another agent has messages waiting for, in
// the manager's own accent so it cannot be read as a state the agent is in.
func inboxBadge(g presentation.GlyphSet, count int) string {
	return inboxBadgeStyle().Render(g.InboxGlyph + strconv.Itoa(count))
}

// afterTurnGlyph makes a requested archive or kill visible while the agent
// finishes its turn, before the action takes effect.
func (m *Model) afterTurnGlyph(action string) string {
	switch action {
	case store.AfterTurnArchive:
		return m.glyphs().AfterTurnArchiveGlyph
	case store.AfterTurnKill:
		return m.glyphs().AfterTurnKillGlyph
	default:
		return ""
	}
}

var afterTurnNote = map[string]string{
	store.AfterTurnArchive: "archives when this turn ends",
	store.AfterTurnKill:    "killed when this turn ends",
}

func (m *Model) afterTurnBadge(action string) string {
	return inboxBadgeStyle().Render(m.afterTurnGlyph(action))
}

// keyPill renders a chip with the key that changes it dimmed in front, so
// the header doubles as a key legend: each changeable value wears its
// shortcut. The key is dim enough to lose to the value at a glance but
// reads clearly when hunted.
func keyPill(key, text string, fg lipgloss.Color) string {
	return subtleStyle().Render(key+" ") + pill(text, fg)
}

// keyCap renders one binding: the key in accent, its action beside it. The
// key stays plain text; the legend's badge carries the visual weight, so a
// row of bindings reads as prose under a header rather than as buttons.
func keyCap(key, label string) string {
	return keyStyle().Render(key) + " " + legendLabelStyle().Render(label)
}

// The docs launch wears the Focused badge so it reads ahead of the keys beside it.
func hintCap(key, label string) string {
	if (key == "o" && label == "docs") || label == "open the docs" {
		return legendBadgeStyle().Render(key + " " + label)
	}
	return keyCap(key, label)
}

func keyCapLead(key, label string) string {
	return legendLeadKey().Render(key) + legendLeadLabel().Render(label)
}

func keyCapAlert(key, label string) string {
	return legendAlertStyle().Render(key + " " + label)
}

// keyCapQuiet is keyCap for a secondary tier: the label drops to the dim
// tone so the tier recedes behind the one above it.
func keyCapQuiet(key, label string) string {
	return keyStyle().Render(key) + " " + mutedStyle().Render(label)
}

// imageChip tints a pasted-image token. Color only: the token's own
// characters are what the textarea measured when it wrapped the line.
func imageChip(token string) string {
	return imageChipStyle().Render(token)
}

// imageChipPasting tints a chip whose clipboard read is still running.
func imageChipPasting(token string) string {
	return imageChipPastingStyle().Render(token)
}
