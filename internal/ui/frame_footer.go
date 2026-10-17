package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// viewFooter is the app's legend: a tier of keys for whatever the cursor is
// on, then a quieter tier for the keys that always apply. A transient mode
// (quick prompt, rename, resize) owns the legend alone while it is up.
func (m *Model) viewFooter() string {
	if m.rail.Reordering() {
		return m.reorderFooter()
	}
	if m.quick.active {
		return m.quickFooter()
	}
	if m.layout.split.resizeMode || m.layout.split.dragging {
		return m.transientFooter(legendSection{title: "Resize", pairs: [][2]string{
			{"←→", "nudge"}, {"drag", "divider"}, {strings.TrimPrefix(m.listGlyph(keybind.Resize)+" / release", " / "), "commit"}, {"esc", "cancel"},
		}})
	}
	if m.mode == modeRename {
		return m.transientFooter(legendSection{title: "Rename", pairs: m.rename.Hints()})
	}
	// Focused, the keyboard belongs to the agent: the tier says so in its
	// title, carries the few keys the manager keeps, and drops the app-wide
	// tier, which would name keys the agent receives.
	if m.mode == modeFocus {
		// Clicking the session's own row only leaves focus where the list
		// is painted, so the full screen layout names the button and the
		// key alone.
		back := m.services.keys.Binding(keybind.Detach).Label()
		if !m.prefs.fullLayout {
			back += " / click its row"
		}
		back += " / mouse back"
		sess, selected := m.selected()
		pagesScrollback := selected && m.focusPagesScrollback(sess)
		pairs := [][2]string{{back, "back"}}
		if m.prefs.arrowStep {
			label := "prompt start: back"
			if m.prefs.fullLayout && pagesScrollback {
				label = "back"
			}
			pairs = append(pairs, [2]string{"←", label})
		}
		if label := m.services.keys.Binding(keybind.Review).Label(); label != "" {
			pairs = append(pairs, [2]string{label, "review"})
		}
		if label := m.services.keys.Binding(keybind.Editor).Label(); label != "" {
			pairs = append(pairs, [2]string{label, "editor"})
		}
		if pagesScrollback {
			pairs = append(pairs, [2]string{"pgup/pgdn", "scroll"})
		}
		if m.focus.pane.Pane().Mouse {
			pairs = append(pairs, [2]string{"click / alt+drag", "agent UI"})
		}
		return m.transientFooter(legendSection{title: "Focused", pairs: pairs})
	}
	return m.listFooter()
}

func (m *Model) listFooter() string {
	return legendBar([]legendSection{m.rowLegend(), m.viewLegend()}, m.layout.width)
}

// transientFooter renders one tier at the list footer's height: the footer
// sets the preview box, and a box that moves resizes every session's pane,
// which costs an agent drawing on the normal screen a full transcript redraw.
// The full screen layout has no preview box to hold still, so a tier there
// takes the one row it needs and hands the rest to the body.
func (m *Model) transientFooter(section legendSection) string {
	bar := legendBar([]legendSection{section}, m.layout.width)
	if m.prefs.fullLayout {
		return bar
	}
	return presentation.PadToHeight(bar, lipgloss.Height(m.listFooter()))
}

func (m *Model) listGlyph(actions ...string) string {
	joined := ""
	for _, action := range actions {
		glyph := m.services.listKeys.Binding(action).Glyph("/")
		if glyph == "" {
			continue
		}
		if joined != "" {
			joined += "/"
		}
		joined += glyph
	}
	return joined
}

func (m *Model) messagesAlert() (glyph, label string) {
	if len(m.activeNotices()) == 0 {
		return "", ""
	}
	glyph = m.listGlyph(keybind.Messages)
	if glyph == "" {
		return "", ""
	}
	return glyph, "unread"
}
