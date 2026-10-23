package frame

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

// Cap is the painting the app's live theme gives a key and its action. The
// root fills it from its published styles, so the legend rules live here once
// and still read the theme the rest of the frame reads.
type Cap struct {
	Key     lipgloss.Style
	Label   lipgloss.Style
	Muted   lipgloss.Style
	LeadKey lipgloss.Style
	Lead    lipgloss.Style
	Alert   lipgloss.Style
	Badge   lipgloss.Style
}

// keyCap renders one binding: the key in accent, its action beside it. The
// key stays plain text; the legend's badge carries the visual weight, so a
// row of bindings reads as prose under a header rather than as buttons.
func (c Cap) keyCap(key, label string) string {
	return c.Key.Render(key) + " " + c.Label.Render(label)
}

// hintCap is the foot-of-card cap: the docs launch wears the Focused badge
// so it reads ahead of the keys beside it.
func (c Cap) hintCap(key, label string) string {
	if (key == "o" && label == "docs") || label == "open the docs" {
		return c.Badge.Render(key + " " + label)
	}
	return c.keyCap(key, label)
}

func (c Cap) leadCap(key, label string) string {
	return c.LeadKey.Render(key) + c.Lead.Render(label)
}

func (c Cap) alertCap(key, label string) string {
	return c.Alert.Render(key + " " + label)
}

// quietCap is keyCap for a secondary tier: the label drops to the dim tone so
// the tier recedes behind the one above it.
func (c Cap) quietCap(key, label string) string {
	return c.Key.Render(key) + " " + c.Muted.Render(label)
}

// Divider renders a labeled section rule that fills the given width: an
// accent tick, the label, then a hairline out to the edge.
func Divider(label string, width int, heading, rule lipgloss.Style) string {
	return presentation.Divider(label, width, heading, rule)
}
