package theme

import "github.com/charmbracelet/lipgloss"

// Pill renders a small label chip: tinted text on the surface fill, so a
// row of them reads as tokens rather than as more prose.
func Pill(text string, fg lipgloss.Color) string {
	return ChipStyle().Foreground(fg).Render(text)
}

// KeyPill renders a chip with the key that changes it dimmed in front, so
// the header doubles as a key legend: each changeable value wears its
// shortcut. The key is dim enough to lose to the value at a glance but
// reads clearly when hunted.
func KeyPill(key, text string, fg lipgloss.Color) string {
	return SubtleStyle().Render(key+" ") + Pill(text, fg)
}

// KeyCap renders one binding: the key in accent, its action beside it. The
// key stays plain text; the legend's badge carries the visual weight, so a
// row of bindings reads as prose under a header rather than as buttons.
func KeyCap(key, label string) string {
	return KeyStyle().Render(key) + " " + LegendLabelStyle().Render(label)
}

// The docs launch wears the Focused badge so it reads ahead of the keys beside it.
func HintCap(key, label string) string {
	if (key == "o" && label == "docs") || label == "open the docs" {
		return LegendBadgeStyle().Render(key + " " + label)
	}
	return KeyCap(key, label)
}

func KeyCapLead(key, label string) string {
	return LegendLeadKey().Render(key) + LegendLeadLabel().Render(label)
}

func KeyCapAlert(key, label string) string {
	return LegendAlertStyle().Render(key + " " + label)
}

// KeyCapQuiet is KeyCap for a secondary tier: the label drops to the dim
// tone so the tier recedes behind the one above it.
func KeyCapQuiet(key, label string) string {
	return KeyStyle().Render(key) + " " + MutedStyle().Render(label)
}

// ImageChip tints a pasted-image token. Color only: the token's own
// characters are what the textarea measured when it wrapped the line.
func ImageChip(token string) string {
	return ImageChipStyle().Render(token)
}

// ImageChipPasting tints a chip whose clipboard read is still running.
func ImageChipPasting(token string) string {
	return ImageChipPastingStyle().Render(token)
}
