package frame

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

type FocusKeys struct {
	Detach      string
	Review      string
	Editor      string
	SavedPrompt string
}

// Focus is a focused session's footer: whether its pane pages scrollback and
// draws its own mouse UI, and the pairs the surface over it adds ahead of the
// manager's keys. Bare is a surface whose own keys replace the manager's.
type Focus struct {
	Keys            FocusKeys
	PagesScrollback bool
	Mouse           bool
	Surface         [][2]string
	Bare            bool
}

// Footer is the footer's height rule: the base screen's tiers are what a
// transient tier pads to, and the full screen layout has no preview box to
// hold still.
type Footer struct {
	Mode       presentation.GlyphMode
	List       []Section
	Width      int
	FullLayout bool
	ArrowStep  bool
}

func (f Footer) ListLegend() Legend { return PaintLive(f.List, f.Width, f.Mode) }

// Transient paints one tier at the list footer's height: the footer sets the
// preview box, and a box that moves resizes every session's pane, which costs
// an agent drawing on the normal screen a full transcript redraw. The full
// screen layout has no preview box to hold still, so a tier there takes the
// one row it needs and hands the rest to the body.
func (f Footer) Transient(tier Section) Legend {
	painted := PaintLive([]Section{tier}, f.Width, f.Mode)
	if f.FullLayout {
		return painted
	}
	painted.Text = presentation.PadToHeight(painted.Text, lipgloss.Height(f.ListLegend().Text))
	return painted
}

// Focused is the tier the manager keeps while the agent has the rest. A click
// on the list only leaves focus where the list is painted, so the full screen
// layout names the button and the key alone; the app's own tiers drop out,
// since they would name keys the agent receives.
func (f Footer) Focused(focus Focus) Section {
	back := focus.Keys.Detach
	if !f.FullLayout {
		back += " / click the list"
	}
	back += " / mouse back"
	pairs := [][2]string{{back, "back"}}
	if f.ArrowStep {
		label := "prompt start: back"
		if f.FullLayout && focus.PagesScrollback {
			label = "back"
		}
		pairs = append(pairs, [2]string{presentation.NavigationLabel(f.Mode, "←"), label})
	}
	if focus.Keys.Review != "" {
		pairs = append(pairs, [2]string{focus.Keys.Review, "review"})
	}
	if focus.Keys.Editor != "" {
		pairs = append(pairs, [2]string{focus.Keys.Editor, "editor"})
	}
	saved := focus.Keys.SavedPrompt
	if !focus.Bare {
		pairs = append(focus.Surface, pairs...)
		saved = ""
	}
	savedAt := len(pairs)
	if focus.PagesScrollback {
		pairs = append(pairs, [2]string{"pgup/pgdn", "scroll"})
	}
	if focus.Mouse {
		pairs = append(pairs, [2]string{"click / alt+drag", "agent UI"})
	}
	tier := Section{Title: "Focused", Pairs: pairs}
	if saved == "" {
		return tier
	}
	// Full layout keeps a one-row footer; the key map still names this key.
	withSaved := Section{Title: "Focused", Pairs: slices.Insert(slices.Clone(pairs), savedAt, [2]string{saved, "saved prompts"})}
	if f.FullLayout && lipgloss.Height(f.Transient(withSaved).Text) > lipgloss.Height(f.Transient(tier).Text) {
		return tier
	}
	return withSaved
}
