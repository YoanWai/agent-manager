package theme

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
