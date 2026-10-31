package theme

import "github.com/YoanWai/agent-manager/internal/ui/presentation"

// BgSeq is the raw "set background" SGR for a hex color, for the few spots
// that paint a background around content lipgloss already styled. It goes
// through the live color profile, so a 256-color terminal gets an indexed
// sequence rather than a truecolor one lipgloss would have downsampled.
func BgSeq(hex string) string {
	return presentation.ColorSequence(hex, true)
}

func FgSeq(hex string) string {
	return presentation.ColorSequence(hex, false)
}
