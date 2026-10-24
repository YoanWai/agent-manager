package frame

import (
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// LiveCap is the live theme's painting of a key and its action, the way the
// legend rules need it.
func LiveCap() Cap {
	return Cap{
		Key:     uitheme.KeyStyle(),
		Label:   uitheme.LegendLabelStyle(),
		Muted:   uitheme.MutedStyle(),
		LeadKey: uitheme.LegendLeadKey(),
		Lead:    uitheme.LegendLeadLabel(),
		Alert:   uitheme.LegendAlertStyle(),
		Badge:   uitheme.LegendBadgeStyle(),
	}
}

// TitleStyles is a tier's name as the footer paints it: the badge for the tier
// the cursor is on, the quieter title for a secondary tier.
func TitleStyles() (badge, quiet func(string) string) {
	badgeStyle, titleStyle := uitheme.LegendBadgeStyle(), uitheme.LegendTitleStyle()
	return func(s string) string { return badgeStyle.Render(s) }, func(s string) string { return titleStyle.Render(s) }
}

// Separator joins the bindings of a secondary tier.
func Separator() string { return uitheme.SubtleStyle().Render(" · ") }

// LiveChrome is a card's painting: its edge tone pulled toward the accent so a
// dialog reads as the app's own surface rather than as a box drawn around it,
// the accent its title reads in, the block fill inside it, and the caps on its
// foot.
func LiveChrome() Chrome {
	return Chrome{
		Border: lipgloss.NewStyle().Foreground(lipgloss.Color(uitheme.Mix(uitheme.Current().Border, uitheme.Current().Accent, 0.35))),
		Title:  lipgloss.NewStyle().Foreground(uitheme.ColorAccent()).Bold(true),
		Block:  uitheme.BlockHex(),
		Cap:    LiveCap(),
	}
}

// PaintLive renders the app's footer, one tier per line where the terminal
// allows it and the tail marked when it does not.
func PaintLive(sections []Section, width int) Legend {
	badge, quiet := TitleStyles()
	return PaintLegend(sections, width, LiveCap(), badge, quiet, Separator())
}
