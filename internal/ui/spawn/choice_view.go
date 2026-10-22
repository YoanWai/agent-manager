package spawn

import (
	"fmt"
	"slices"
	"strings"

	catalog "github.com/YoanWai/agent-manager/internal/catalog/value"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Styles are the root's current presentation tokens, copied for one paint.
type Styles struct {
	GlyphMode                         presentation.GlyphMode
	Subtle, Value, Muted, Warn, Label lipgloss.Style
	SubtleColor, Accent, Accent2      lipgloss.Color
	// Input paints a text field, marking its caret for the IME.
	Input func(textinput.Model) string
}

// ModelNote says why the model row has no list, or reports it has one.
func ModelNote(h ChoiceHost, toolName string, st Styles) (string, bool) {
	state, supported := choiceAnswer(h, toolName)
	switch {
	case !supported || !h.ChoiceCaps(toolName).Models:
		return st.Subtle.Render("not supported by " + toolName), false
	case state == nil || (!state.Loaded && state.Loading):
		return st.Subtle.Render("reading models from " + toolName + "…"), false
	case state.Err != nil && len(state.Cat.Models) == 0 && h.ChoiceRemoteHost() != "":
		return st.Subtle.Render(toolName + "'s default on " + h.ChoiceRemoteHost() + "; this machine's " + toolName + " lists no models to pick from"), false
	case state.Err != nil && len(state.Cat.Models) == 0:
		return st.Warn.Render("couldn't read models from " + toolName + ": " + state.Err.Error()), false
	case len(state.Cat.Models) == 0 && len(state.Cat.Profiles) == 0:
		return st.Subtle.Render(toolName + " lists no models"), false
	}
	return "", true
}

// EffortRow is the row's value, whether it shows, and whether it takes keys.
func (ch *Choice) EffortRow(h ChoiceHost, toolName string, st Styles) (value string, shown, active bool) {
	state, supported := choiceAnswer(h, toolName)
	if !supported || !h.ChoiceCaps(toolName).Efforts {
		return st.Subtle.Render("not supported by " + toolName), true, false
	}
	if state == nil || !state.Loaded || len(ch.models(h, toolName)) == 0 {
		return "", false, false
	}
	model, known := ch.effortModel(h, toolName)
	switch {
	case !known:
		return st.Subtle.Render("pick a model to see its levels"), true, false
	case model.EffortTyped:
		return ch.TypedEffortView(st) + "  " + st.Subtle.Render("typed · "+toolName+" lists no levels"), true, true
	case len(model.Efforts) == 0:
		return "", false, false
	}
	level := ch.EffortLevel(h, toolName)
	shownLevel := st.Subtle.Render("default")
	if level != "" {
		shownLevel = st.Value.Render(level)
	}
	value = st.Subtle.Render("◂ ") + shownLevel + st.Subtle.Render(" ▸")
	if model.DefaultEffort != "" {
		value += "  " + st.Subtle.Render("default is "+model.DefaultEffort)
	}
	return value, true, true
}

// ProfileRow is the profile row's value, and whether it shows.
func (ch *Choice) ProfileRow(h ChoiceHost, toolName string, st Styles) (string, bool) {
	profiles := choiceProfiles(h, toolName)
	if len(profiles) == 0 {
		return "", false
	}
	value := st.Subtle.Render(toolName + "'s default")
	if ch.profile > 0 {
		profile := profiles[ch.profile-1]
		value = st.Value.Render(profile.Name)
		if profile.Detail != "" {
			value += "  " + st.Subtle.Render(profile.Detail)
		}
	}
	return st.Subtle.Render("◂ ") + value + st.Subtle.Render(" ▸"), true
}

// FilterView paints the model filter.
func (ch *Choice) FilterView(st Styles) string { return st.Input(ch.filter) }

// TypedEffortView paints the typed level.
func (ch *Choice) TypedEffortView(st Styles) string { return st.Input(ch.typedEffort) }

// ViewSuggestions returns each line's list index in entries, -1 for a
// heading.
func (ch *Choice) ViewSuggestions(h ChoiceHost, toolName string, indent, width, visible int, st Styles) (lines []string, entries []int) {
	query := ch.query()
	list := ch.suggestions(h, toolName, query)
	headingStyle := lipgloss.NewStyle().Foreground(st.SubtleColor).Italic(true)
	var rows []string
	heading := func(text string) {
		rows = append(rows, "  "+headingStyle.Render(text))
		entries = append(entries, -1)
	}
	highlight := -1
	state, _ := choiceAnswer(h, toolName)
	for i, entry := range list {
		if i == 0 && entry.recent {
			heading("recent")
		}
		if !entry.recent && (i == 0 || list[i-1].recent) {
			source := fmt.Sprintf("from %s · %d models", toolName, len(ch.models(h, toolName)))
			if state.Err != nil {
				source += " · refresh failed: " + state.Err.Error()
			}
			heading(source)
		}
		marker, style := "  ", st.Muted
		if i == ch.sugg.index {
			marker = lipgloss.NewStyle().Foreground(st.Accent).Render(presentation.GlyphsFor(st.GlyphMode).SelectionGlyph + " ")
			style = lipgloss.NewStyle().Foreground(st.Accent2).Bold(true)
			highlight = len(rows)
		}
		row := marker + style.Render(entry.model.ID)
		if detail := modelDetail(entry.model); detail != "" {
			row += "  " + st.Subtle.Render(detail)
		}
		rows = append(rows, row)
		entries = append(entries, i)
	}
	if len(list) == 0 {
		heading("no model matches " + strings.TrimSpace(query))
	}
	pad := strings.Repeat(" ", indent)
	if len(rows) <= visible {
		for _, row := range rows {
			lines = append(lines, pad+ansi.Truncate(row, width, "…"))
		}
		return lines, entries
	}
	ch.sugg.offset = scrollToShow(ch.sugg.offset, highlight, len(rows), visible, entries)
	bar := scrollBar(ch.sugg.offset, len(rows), visible, st)
	for i, row := range rows[ch.sugg.offset : ch.sugg.offset+visible] {
		lines = append(lines, pad+presentation.PadRight(row, width-2)+" "+bar[i])
	}
	return lines, entries[ch.sugg.offset : ch.sugg.offset+visible]
}

// scrollToShow returns to the top on the first entry, so the headings above
// it show.
func scrollToShow(offset, highlight, total, visible int, entries []int) int {
	switch {
	case highlight < 0:
	case highlight == slices.IndexFunc(entries, func(entry int) bool { return entry >= 0 }):
		offset = 0
	case highlight < offset:
		offset = highlight
	case highlight >= offset+visible:
		offset = highlight - visible + 1
	}
	return min(max(offset, 0), total-visible)
}

func scrollBar(offset, total, visible int, st Styles) []string {
	thumb := max(visible*visible/total, 1)
	start := offset * (visible - thumb) / (total - visible)
	track, lit := st.Subtle.Render("│"), lipgloss.NewStyle().Foreground(st.Accent).Render("┃")
	bar := make([]string, visible)
	for i := range bar {
		bar[i] = track
		if i >= start && i < start+thumb {
			bar[i] = lit
		}
	}
	return bar
}

// modelDetail is the provider a model routes through, or the CLI's name for
// it when that says more than the id.
func modelDetail(model catalog.Model) string {
	if model.Provider != "" {
		return model.Provider
	}
	if model.Label != model.ID {
		return model.Label
	}
	return ""
}
