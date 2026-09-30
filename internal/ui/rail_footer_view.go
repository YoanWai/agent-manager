package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
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
		worktreeHint := "off"
		switch {
		case !m.worktreeCapable(m.quickTargetDir()):
			worktreeHint = worktreeUnavailable
		case m.quickWorktreeOn():
			worktreeHint = "on"
		}
		return m.transientFooter(legendSection{title: "Prompt", pairs: [][2]string{
			{"↵", "send"}, {"↑↓", "target or caret"}, {"tab", "tool: " + m.quickTool()},
			{"shift+tab", "worktree: " + worktreeHint}, {"esc", "close"},
		}})
	}
	if m.split.resizeMode || m.split.dragging {
		return m.transientFooter(legendSection{title: "Resize", pairs: [][2]string{
			{"←→", "nudge"}, {"drag", "divider"}, {strings.TrimPrefix(m.listGlyph(keybind.Resize)+" / release", " / "), "commit"}, {"esc", "cancel"},
		}})
	}
	if m.mode == modeRename {
		pairs := [][2]string{{"↵", "save"}, {"esc", "cancel"}}
		if m.rename.isGroup {
			pairs = [][2]string{{"tab", "name / path"}, {"↵", "save"}, {"esc", "cancel"}}
		} else if tool := m.renameTool(); tool != "" {
			pairs = [][2]string{{"tab", "tool: " + tool}, {"↵", "save"}, {"esc", "cancel"}}
		}
		return m.transientFooter(legendSection{title: "Rename", pairs: pairs})
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
		pairs := [][2]string{
			{"typing", "to agent"},
			{back, "back"},
		}
		if m.prefs.arrowStep {
			pairs = append(pairs, [2]string{"←", "prompt start: back"})
		}
		if label := m.services.keys.Binding(keybind.Review).Label(); label != "" {
			pairs = append(pairs, [2]string{label, "review"})
		}
		if label := m.services.keys.Binding(keybind.Editor).Label(); label != "" {
			pairs = append(pairs, [2]string{label, "editor"})
		}
		// The word and line gestures stay in the key map, where there is
		// room to name all three.
		pairs = append(pairs, [2]string{"drag / click", "copy"})
		if m.focusPane.Pane().Mouse {
			pairs = append(pairs, [2]string{"click / alt+drag", "agent UI"})
		}
		return m.transientFooter(legendSection{title: "Focused", pairs: pairs})
	}
	return m.listFooter()
}

func (m *Model) listFooter() string {
	return legendBar([]legendSection{m.rowLegend(), m.viewLegend()}, m.width)
}

// transientFooter renders one tier at the list footer's height: the footer
// sets the preview box, and a box that moves resizes every session's pane,
// which costs an agent drawing on the normal screen a full transcript redraw.
// The full screen layout has no preview box to hold still, so a tier there
// takes the one row it needs and hands the rest to the body.
func (m *Model) transientFooter(section legendSection) string {
	bar := legendBar([]legendSection{section}, m.width)
	if m.prefs.fullLayout {
		return bar
	}
	return padToHeight(bar, lipgloss.Height(m.listFooter()))
}

// rowLegend is the tier for the entry under the cursor: what this session or
// this group can be told to do.
func (m *Model) rowLegend() legendSection {
	k := m.listGlyph
	enterHint, attachHint := "focus / fold", "attach"
	if !m.enterFocuses() {
		enterHint, attachHint = "attach / fold", "focus"
	}
	row, ok := m.selectedRow()
	if !ok {
		return legendSection{}
	}
	if row.isGroup {
		foldAction := "fold"
		if m.rail.IsCollapsed(row.group) {
			foldAction = "unfold"
		}
		pairs := [][2]string{{k(keybind.Open), foldAction}}
		if !m.prefs.mouseDisabled {
			pairs = append(pairs, [2]string{"double click", foldAction})
		}
		if m.prefs.arrowStep {
			pairs = append(pairs, m.legendPair(keybind.StepOut, "close", keybind.StepIn, "open"))
		}
		pairs = append(pairs, [][2]string{
			{k(keybind.Editor), "editor"}, {k(keybind.Rename), "rename"}, {k(keybind.Move), "move"},
			m.legendPair(keybind.Kill, "kill", keybind.KillAll, "all"), m.legendPair(keybind.Revive, "revive", keybind.ReviveAll, "all"),
			m.archiveRestoreLegend(), {k(keybind.Delete), "delete"},
		}...)
		return legendSection{title: "Group", pairs: legendPairsBound(pairs)}
	}
	title := "Session"
	conversation := [][2]string{{k(keybind.Prompt), "prompt"}, {k(keybind.CopyReply), "copy"}, {k(keybind.Review), "review"}, {k(keybind.Fork), "fork"}}
	if m.isShell(row.sess.Tool) {
		// A shell has no conversation, so the keys that would prompt,
		// review or fork one are left off rather than offered and refused.
		title, conversation = "Shell", nil
	}
	pairs := [][2]string{{k(keybind.Open), enterHint}, {k(keybind.Attach), attachHint}}
	if !m.prefs.mouseDisabled {
		gesture := "double click"
		if !m.prefs.fullLayout {
			gesture = "click"
		}
		pairs = append(pairs, [2]string{gesture, "focus"})
	}
	if m.prefs.arrowStep {
		pairs = append(pairs, [2]string{k(keybind.StepIn), "focus"})
	}
	if row.sess.Status == status.Finished && !row.sess.Archived {
		pairs = append(pairs, [2]string{k(keybind.MarkIdle), "mark idle"})
	}
	pairs = append(pairs, conversation...)
	// o sits outside the conversation keys: a shell's directory is worth
	// opening as much as an agent's.
	pairs = append(pairs, [][2]string{
		{k(keybind.Editor), "editor"}, {k(keybind.Rename), "rename"}, {k(keybind.Move), "move"},
		m.legendPair(keybind.Kill, "kill", keybind.KillAll, "all"), m.legendPair(keybind.Revive, "revive", keybind.ReviveAll, "all"), {k(keybind.Restart), "restart"},
		m.archiveRestoreLegend(), {k(keybind.Delete), "delete"},
	}...)
	return legendSection{title: title, pairs: legendPairsBound(pairs)}
}

// archiveRestoreLegend leaves out the key of the pair that no-ops in this view.
func (m *Model) archiveRestoreLegend() [2]string {
	if m.rail.ShowArchived() {
		return [2]string{m.listGlyph(keybind.Restore), "restore"}
	}
	return [2]string{m.listGlyph(keybind.Archive), "archive"}
}

func (m *Model) legendPair(first, firstLabel, second, secondLabel string) [2]string {
	switch {
	case m.listGlyph(first) == "":
		return [2]string{m.listGlyph(second), secondLabel}
	case m.listGlyph(second) == "":
		return [2]string{m.listGlyph(first), firstLabel}
	}
	return [2]string{m.listGlyph(first, second), firstLabel + " / " + secondLabel}
}

func legendPairsBound(pairs [][2]string) [][2]string {
	kept := pairs[:0]
	for _, pair := range pairs {
		if pair[0] != "" {
			kept = append(kept, pair)
		}
	}
	return kept
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

// viewLegend is the tier that never changes with the cursor: moving around
// the list, filtering it, and leaving.
func (m *Model) viewLegend() legendSection {
	emptyGroupsAction := "hide empty"
	if m.rail.HideEmptyGroups() {
		emptyGroupsAction = "show empty"
	}
	statusFilterAction := "attention"
	if m.rail.FilteringAttention() {
		statusFilterAction = "show all"
	}
	archivedAction := "archived"
	if m.rail.ShowArchived() {
		archivedAction = "back to active"
	}
	foldAllAction := "fold all"
	if m.rail.AllGroupsCollapsed() {
		foldAllAction = "unfold all"
	}
	// Ordered by what a narrow terminal must keep: moving around, making
	// something, the filters, then the keys a user already knows to look for.
	k := m.listGlyph
	emptyGroupsKey := k(keybind.EmptyGroups)
	if m.rail.ShowArchived() {
		emptyGroupsKey = ""
	}
	pairs := [][2]string{{strings.TrimSpace(k(keybind.Up) + " " + k(keybind.Down)), "navigate"}}
	pairs = append(pairs, [][2]string{
		{k(keybind.NewSession), "new"}, {k(keybind.Terminal), "terminal"}, {k(keybind.NewGroup), "group"}, {k(keybind.Search), "search"},
		{k(keybind.Archived), archivedAction}, {k(keybind.Filter), statusFilterAction}, {emptyGroupsKey, emptyGroupsAction},
		{k(keybind.Help), "keys"}, {k(keybind.Quit), "quit"},
		{k(keybind.ReorderUp, keybind.ReorderDown), "reorder"}, {k(keybind.FoldAll), foldAllAction}, {k(keybind.Resize), "resize"}, {k(keybind.Settings), "settings"},
	}...)
	return legendSection{title: "View", quiet: true, pairs: legendPairsBound(pairs)}
}

func (m *Model) reorderFooter() string {
	info := m.rail.ReorderInfo()
	if info.DropLabel != "" {
		return m.transientFooter(legendSection{title: "Move", pairs: [][2]string{
			{"release", "move " + info.DropLabel}, {"esc", "put back"},
		}})
	}
	return m.transientFooter(legendSection{title: "Reorder", pairs: [][2]string{
		{"drag / ↑↓ / wheel", "move"}, {"↵ / release", "drop"}, {"esc", "put back"},
	}})
}
