package theme

import (
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
)

// themeStyles is every style derived from one palette. Apply builds a
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
// field on Model because the free render helpers (KeyCap, Pill, Paint's
// tones, Hrule) read it from everywhere; one atomic pointer keeps that
// reach without a data race. It starts on the default palette.
var activeStyles = publishStyles(Palettes[0])

// Apply publishes the styles for a token set; a theme switch repaints
// from the next frame on.
func Apply(t Theme) { activeStyles.Store(newThemeStyles(t)) }

func publishStyles(t Theme) *atomic.Pointer[themeStyles] {
	active := new(atomic.Pointer[themeStyles])
	active.Store(newThemeStyles(t))
	return active
}

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
		lead     = lipgloss.Color(Mix(t.Bg, t.Accent, 0.28))
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

// Current is the live token set; renderers that need a raw SGR sequence
// (rather than a lipgloss style) read their hex from here.
func Current() Theme { return activeStyles.Load().theme }

func ColorBg() lipgloss.Color       { return lipgloss.Color(Current().Bg) }
func ColorOverlay() lipgloss.Color  { return lipgloss.Color(Current().Overlay) }
func ColorBorder() lipgloss.Color   { return lipgloss.Color(Current().Border) }
func ColorBright() lipgloss.Color   { return lipgloss.Color(Current().Bright) }
func ColorSubtle() lipgloss.Color   { return lipgloss.Color(Current().Subtle) }
func ColorAccent() lipgloss.Color   { return lipgloss.Color(Current().Accent) }
func ColorAccent2() lipgloss.Color  { return lipgloss.Color(Current().Accent2) }
func ColorRemote() lipgloss.Color   { return lipgloss.Color(Current().Remote) }
func ColorWorking() lipgloss.Color  { return lipgloss.Color(Current().Working) }
func ColorWaiting() lipgloss.Color  { return lipgloss.Color(Current().Waiting) }
func ColorFinished() lipgloss.Color { return lipgloss.Color(Current().Finished) }
func ColorErrored() lipgloss.Color  { return lipgloss.Color(Current().Errored) }
func ColorIdle() lipgloss.Color     { return lipgloss.Color(Current().Idle) }

func SectionStyle() lipgloss.Style          { return activeStyles.Load().section }
func SelectedRowStyle() lipgloss.Style      { return activeStyles.Load().selectedRow }
func MutedStyle() lipgloss.Style            { return activeStyles.Load().muted }
func SubtleStyle() lipgloss.Style           { return activeStyles.Load().subtle }
func ValueStyle() lipgloss.Style            { return activeStyles.Load().value }
func LabelStyle() lipgloss.Style            { return activeStyles.Load().label }
func ErrStyle() lipgloss.Style              { return activeStyles.Load().err }
func DoneStyle() lipgloss.Style             { return activeStyles.Load().done }
func WarnStyle() lipgloss.Style             { return activeStyles.Load().warn }
func KeyStyle() lipgloss.Style              { return activeStyles.Load().key }
func AnnotationStyle() lipgloss.Style       { return activeStyles.Load().annotation }
func InboxBadgeStyle() lipgloss.Style       { return activeStyles.Load().inboxBadge }
func SearchMatchStyle() lipgloss.Style      { return activeStyles.Load().searchMatch }
func FocusEdgeStyle() lipgloss.Style        { return activeStyles.Load().focusEdge }
func ChipStyle() lipgloss.Style             { return activeStyles.Load().chip }
func ImageChipStyle() lipgloss.Style        { return activeStyles.Load().imageChip }
func ImageChipPastingStyle() lipgloss.Style { return activeStyles.Load().imageChipPasting }
func LegendTitleStyle() lipgloss.Style      { return activeStyles.Load().legendTitle }
func LegendBadgeStyle() lipgloss.Style      { return activeStyles.Load().legendBadge }
func LegendAlertStyle() lipgloss.Style      { return activeStyles.Load().legendAlert }
func LegendLabelStyle() lipgloss.Style      { return activeStyles.Load().legendLabel }
func LegendLeadKey() lipgloss.Style         { return activeStyles.Load().leadKey }
func LegendLeadLabel() lipgloss.Style       { return activeStyles.Load().leadLabel }

// The accent lift separates reviewer notes from the diff itself. Resolve it
// per call so live theme and color-profile changes take effect immediately.
func AnnotationBg() string {
	return BgSeq(Mix(Current().Bg, Current().Accent, 0.18))
}

func HandledAnnotationBg() string {
	return BgSeq(Mix(Current().Bg, Current().Finished, 0.14))
}
