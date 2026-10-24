package ui

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/keybind"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) viewFooter() string { return m.footer().text }

// footer is the app's legend: a tier of keys for whatever the cursor is
// on, then a quieter tier for the keys that always apply. A transient mode
// (quick prompt, rename, resize) owns the legend alone while it is up.
func (m *Model) footer() legend {
	if m.rail.Reordering() {
		return m.transientFooter(legendOf(m.rail.ReorderLegend()))
	}
	if q := m.quick(); q != nil {
		return m.quickFooter(q)
	}
	if s, ok := surfaceOf[*uigeometry.Surface](m); ok {
		return m.transientFooter(legendSection{title: "Resize", pairs: s.Footer()})
	}
	if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok {
		return m.transientFooter(legendSection{title: "Rename", pairs: s.Footer()})
	}
	// Focused, the keyboard belongs to the agent: the tier says so in its
	// title, carries the few keys the manager keeps, and drops the app-wide
	// tier, which would name keys the agent receives.
	if m.focused() {
		// A click on the list only leaves focus where the list is painted,
		// so the full screen layout names the button and the key alone.
		back := m.services.keys.Binding(keybind.Detach).Label()
		if !m.prefs.fullLayout {
			back += " / click the list"
		}
		back += " / mouse back"
		sess, selected := m.selected()
		pagesScrollback := selected && m.focus.pane.PagesScrollback(sess.ID)
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
		savedLabel := m.services.keys.Binding(keybind.SavedPrompt).Label()
		if top, _ := m.top(); top != nil {
			if _, bare := top.(*uifocus.Surface); !bare {
				pairs = append(top.Footer(), pairs...)
				savedLabel = ""
			}
		}
		savedAt := len(pairs)
		if pagesScrollback {
			pairs = append(pairs, [2]string{"pgup/pgdn", "scroll"})
		}
		if m.focus.pane.Pane().Mouse {
			pairs = append(pairs, [2]string{"click / alt+drag", "agent UI"})
		}
		footer := m.transientFooter(legendSection{title: "Focused", pairs: pairs})
		if savedLabel == "" {
			return footer
		}
		// Full layout keeps a one-row footer; the key map still names this key.
		withSaved := m.transientFooter(legendSection{title: "Focused", pairs: slices.Insert(slices.Clone(pairs), savedAt, [2]string{savedLabel, "saved prompts"})})
		if m.prefs.fullLayout && lipgloss.Height(withSaved.text) > lipgloss.Height(footer.text) {
			return footer
		}
		return withSaved
	}
	return m.listLegend()
}

func (m *Model) listFooter() string { return m.listLegend().text }

func (m *Model) listLegend() legend {
	return paintLegend([]legendSection{m.rowLegend(), m.viewLegend()}, m.layout.width)
}

// transientFooter renders one tier at the list footer's height: the footer
// sets the preview box, and a box that moves resizes every session's pane,
// which costs an agent drawing on the normal screen a full transcript redraw.
// The full screen layout has no preview box to hold still, so a tier there
// takes the one row it needs and hands the rest to the body.
func (m *Model) transientFooter(section legendSection) legend {
	bar := paintLegend([]legendSection{section}, m.layout.width)
	if !m.prefs.fullLayout {
		bar.text = presentation.PadToHeight(bar.text, lipgloss.Height(m.listFooter()))
	}
	return bar
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
