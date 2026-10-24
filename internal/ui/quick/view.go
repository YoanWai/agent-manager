package quick

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// BarMaxRows is the most prompt rows the bar shows before it scrolls.
const BarMaxRows = 5

// Facts are the root facts the bar paints: the row naming an answered
// session, the selected group's name, and what the target directory allows.
type Facts struct {
	Answer                         string
	Group                          string
	WorktreeKnown, WorktreeCapable bool
	WorktreeOn                     bool
	// Remote is the connection target's row, which a spawn there extends
	// with what it launches with; WorktreeInherit leaves the worktree to
	// the host. The root decides both and paints Remote.
	Remote          string
	RemoteSpawn     bool
	WorktreeInherit bool
	// LabelWidth is the column the target's label takes.
	LabelWidth int
	Styles     spawn.Styles
}

// View is the docked prompt: enter answers the selected session, or
// spawns a fresh agent when a group is selected.
func (q *Bar) View(h Host, facts Facts, width, maxRows int) string {
	st := facts.Styles
	label := func(text string) string {
		return st.Label.Render(presentation.PadRight(text, facts.LabelWidth)) + st.Subtle.Render("│ ")
	}
	target := label("target") + st.Muted.Render("no selection")
	q.hits = q.hits[:0]
	if facts.Remote != "" {
		target = label("answer") + facts.Remote
		if facts.RemoteSpawn {
			if q.picking != PickNone {
				return q.viewSheet(h, facts, width, maxRows, facts.Remote)
			}
			target = q.statusRow(h, facts, label("new")+facts.Remote, width, 0)
		}
	} else if selected, ok := h.Target(); ok {
		if selected.IsGroup {
			group := lipgloss.NewStyle().Foreground(st.Accent2).Render(facts.Group)
			if q.picking != PickNone {
				return q.viewSheet(h, facts, width, maxRows, group)
			}
			// Spawning: what the new agent launches with sits beside the
			// target, where the eye lands before typing.
			target = q.statusRow(h, facts, label("new")+group, width, 0)
		} else {
			target = facts.Answer
		}
	}
	// Count wrapped rows only after setting the width.
	prompt := h.Prompt()
	prompt.Fit(width, max(min(maxRows-1, BarMaxRows), 1))
	bar := target + "\n" + prompt.View()
	if !q.savedList.Open() {
		return bar
	}
	lines := q.savedList.Lines(width, savedListRows, st)
	for i := range q.hits {
		q.hits[i].Line += len(lines)
	}
	for i := range lines {
		q.hits = append(q.hits, Hit{Line: i, X0: 0, X1: width, Action: ClickSaved, Entry: i})
	}
	return strings.Join(lines, "\n") + "\n" + bar
}

// savedListRows caps the saved prompts list above the prompt.
const savedListRows = 6

// statusRow puts what a spawn launches with against the right edge of
// the row: the CLI, model, effort, profile and worktree, each a stretch a
// click steps the way its key does.
func (q *Bar) statusRow(h Host, facts Facts, left string, width, line int) string {
	st := facts.Styles
	toolName, ch := q.Tool(), &q.choice
	type segment struct {
		text   string
		action int
	}
	orDefault := func(value, fallback string) string {
		if value == "" {
			return st.Subtle.Render(fallback)
		}
		return st.Value.Render(value)
	}
	segments := []segment{{st.Value.Render(toolName), ClickTool}}
	caps := h.ChoiceCaps(toolName)
	switch note, listed := spawn.ModelNote(h, toolName, st); {
	case listed:
		segments = append(segments, segment{orDefault(ch.Model(), "default model"), ClickModel})
		if _, active := ch.EffortShown(h, toolName); active {
			segments = append(segments, segment{orDefault(ch.EffortLevel(h, toolName), "default effort"), ClickEffort})
		}
	case caps.Catalog && caps.Models:
		// Still reading, or the CLI failed to answer: say so. A CLI with
		// nothing to pick shows nothing.
		segments = append(segments, segment{note, ClickModel})
	}
	if spawn.HasProfiles(h, toolName) {
		segments = append(segments, segment{orDefault(ch.ProfileName(h, toolName), "default profile"), ClickProfile})
	}
	worktree := st.Subtle.Render(presentation.GlyphsFor(st.GlyphMode).WorktreeGlyph + " off")
	switch {
	case facts.WorktreeInherit:
		worktree = st.Subtle.Render(presentation.GlyphsFor(st.GlyphMode).WorktreeGlyph + " host default")
	case !facts.WorktreeKnown || !facts.WorktreeCapable:
		worktree = st.Subtle.Render(presentation.GlyphsFor(st.GlyphMode).WorktreeGlyph + " no repo")
	case facts.WorktreeOn:
		worktree = lipgloss.NewStyle().Foreground(st.Accent2).Render(presentation.GlyphsFor(st.GlyphMode).WorktreeGlyph + " on")
	}
	segments = append(segments, segment{worktree, ClickWorktree})

	var status strings.Builder
	offsets := make([]int, len(segments))
	for i, seg := range segments {
		if i > 0 {
			status.WriteString(st.Subtle.Render(" · "))
		}
		offsets[i] = ansi.StringWidth(status.String())
		status.WriteString(seg.text)
	}
	leftWidth := ansi.StringWidth(left)
	statusWidth := min(ansi.StringWidth(status.String()), max(width-leftWidth-2, 0))
	start := width - statusWidth
	for i, seg := range segments {
		if offsets[i] >= statusWidth {
			break
		}
		x1 := min(offsets[i]+ansi.StringWidth(seg.text), statusWidth)
		q.hits = append(q.hits, Hit{Line: line, X0: start + offsets[i], X1: start + x1, Action: seg.action, Entry: -1})
	}
	return left + strings.Repeat(" ", max(start-leftWidth, 0)) + ansi.Truncate(status.String(), statusWidth, "…")
}

// viewSheet stands in for the whole bar while a choice is picked, so
// the list has the rows the prompt and its target would take.
func (q *Bar) viewSheet(h Host, facts Facts, width, maxRows int, group string) string {
	st := facts.Styles
	toolName, ch := q.Tool(), &q.choice
	var lines []string
	if q.picking == PickEffort {
		lines = []string{
			st.Subtle.Render("effort for ") + group,
			ch.TypedEffortView(st) + "  " + st.Subtle.Render("typed · "+toolName+" lists no levels"),
		}
	} else {
		lines = []string{st.Subtle.Render("model for ") + group, ch.FilterView(st)}
		list, entries := ch.ViewSuggestions(h, toolName, 0, width, max(maxRows-len(lines)-1, 1), st)
		for i, line := range list {
			if entries[i] >= 0 {
				q.hits = append(q.hits, Hit{Line: len(lines), X0: 0, X1: width, Action: ClickEntry, Entry: entries[i]})
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(append(lines, q.statusRow(h, facts, "", width, len(lines))), "\n")
}
