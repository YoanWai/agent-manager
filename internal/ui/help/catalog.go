package help

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// The key map is grouped the way its bindings are learned: by what is under
// the cursor or which screen is up, rather than listed alphabetically.
type helpSection struct {
	title string
	rows  [][2]string
}

type helpRows struct {
	list keybind.Table
	rows [][2]string
}

func (h *helpRows) action(does string, actions ...string) {
	var glyphs []string
	for _, action := range actions {
		if glyph := h.list.Binding(action).Glyph(" / "); glyph != "" {
			glyphs = append(glyphs, glyph)
		}
	}
	if len(glyphs) > 0 {
		h.rows = append(h.rows, [2]string{strings.Join(glyphs, " / "), does})
	}
}

func (h *helpRows) fixed(key, does string) {
	h.rows = append(h.rows, [2]string{key, does})
}

func listHelpRows(list keybind.Table, arrowStep bool, glyphs Glyphs) [][2]string {
	h := helpRows{list: list}
	h.fixed("", "Tell your agent to manage sessions and terminals in Agent Manager.")
	h.action("move the cursor up", keybind.Up)
	h.action("move the cursor down", keybind.Down)
	h.fixed("click", "focus a session in the split, select any other row")
	h.fixed("double click", "fold or unfold a group, focus a session full screen")
	h.fixed(glyphs.RowMenu, "the row's actions, or right click")
	h.fixed("drag "+glyphs.Reorder, "move the row, into another group too, or click it for ↑↓")
	if arrowStep {
		h.action("step in: focus the session, open the group", keybind.StepIn)
		h.action("step out: close the group", keybind.StepOut)
	}
	h.action("reorder the row up among its siblings", keybind.ReorderUp)
	h.action("reorder the row down among its siblings", keybind.ReorderDown)
	h.action("new session", keybind.NewSession)
	h.action("new terminal tab: a shell under the selected agent, or in the group", keybind.Terminal)
	h.action("new group (name, parent, default path, worktree)", keybind.NewGroup)
	h.action("search the list by name", keybind.Search)
	h.action("filter to what needs attention (waiting, finished, errored)", keybind.Filter)
	h.action("archived view", keybind.Archived)
	h.action("hide / show empty groups", keybind.EmptyGroups)
	h.action("fold / unfold every group", keybind.FoldAll)
	h.action("resize the split (←→ or hl, or drag; ↵ commits, esc cancels)", keybind.Resize)
	h.action("settings", keybind.Settings)
	h.action("messages", keybind.Messages)
	h.action("this key map", keybind.Help)
	h.action("quit (sessions keep running)", keybind.Quit)
	h.fixed("ctrl+c", "quit, from anywhere")
	return h.rows
}

func sessionRowHelpRows(list keybind.Table) [][2]string {
	h := helpRows{list: list}
	h.action("focus it: keys reach the agent, the list stays on screen", keybind.Open)
	h.action("attach it: the pane takes the whole terminal", keybind.Attach)
	h.fixed("", "settings swap what the two do")
	h.action("mark it idle when it is finished, without entering it", keybind.MarkIdle)
	h.action("quick prompt mode: answer the session without attaching", keybind.Prompt)
	h.action("copy its newest reply to the clipboard", keybind.CopyReply)
	h.action("review its diff", keybind.Review)
	h.action("fork it into a new session in the same group", keybind.Fork)
	h.action("rename it, and re-pick its tool", keybind.Rename)
	h.action("move it to a group, or a terminal into a session", keybind.Move)
	h.action("open its working directory in your editor", keybind.Editor)
	h.action("restart it on an empty context (same name, group, dir, tool)", keybind.Restart)
	h.action("kill it / kill every live session (frees their RAM)", keybind.Kill, keybind.KillAll)
	h.action("revive it, its pane included / revive every dead session", keybind.Revive, keybind.ReviveAll)
	h.action("archive / restore (archive kills, restore revives)", keybind.Archive, keybind.Restore)
	h.action("keep it once its turn ends: cancel the archive or kill it asked for", keybind.CancelEnd)
	h.action("delete it", keybind.Delete)
	return h.rows
}

func markHelpRows(list keybind.Table, glyphs Glyphs) [][2]string {
	rows := [][2]string{
		{"◐ working", "the agent is busy on a turn"},
		{"◆ waiting", "blocked on you: a dialog, a permission ask, a question"},
		{"● finished", "the turn ended; entering the session clears it to idle"},
		{"○ idle", "nothing running"},
		{"✕ errored", "the tool reported an error, or the session is dead"},
		{"◌ starting", "the pane is still launching"},
		{"✉N", "messages from another agent, held until this one is at rest"},
		{glyphs.AfterTurnArchive + " / " + glyphs.AfterTurnKill, "the agent asked to be archived / killed once this turn ends"},
	}
	if filter := list.Binding(keybind.Filter).Glyph(" / "); filter != "" {
		rows = append(rows, [2]string{"", filter + " filters the list down to the marks that need you"})
	}
	return rows
}

func groupRowHelpRows(list keybind.Table) [][2]string {
	h := helpRows{list: list}
	h.action("fold / unfold", keybind.Open)
	h.action("quick prompt mode: spawn a new agent in the group", keybind.Prompt)
	h.action("edit it: name, default path, worktree", keybind.Rename)
	h.action("move it, with its whole subtree, under another group", keybind.Move)
	h.action("open its default path in your editor", keybind.Editor)
	h.action("kill every live session in it / everywhere", keybind.Kill, keybind.KillAll)
	h.action("revive every dead session in it / everywhere", keybind.Revive, keybind.ReviveAll)
	h.action("archive / restore the subtree (archive kills, restore revives)", keybind.Archive, keybind.Restore)
	h.action("delete the group and its subtree", keybind.Delete)
	return h.rows
}

// helpSections is the catalog for one setting of the arrow-step pair: off,
// the rows that pair would answer are left out rather than named as dead.
func helpSections(ctx Context) []helpSection {
	list := ctx.ListKeys
	return []helpSection{
		{title: "list", rows: listHelpRows(list, ctx.ArrowStep, ctx.Glyphs)},
		{title: "session under the cursor", rows: sessionRowHelpRows(list)},
		{title: "the mark on a session row", rows: markHelpRows(list, ctx.Glyphs)},
		{title: "group under the cursor", rows: groupRowHelpRows(list)},
		{title: titledWith("quick prompt mode", list, keybind.Prompt), rows: [][2]string{
			{"↵", "send"},
			{"↑↓", "switch the target session, or step the caret in a taller prompt"},
			{"tab", "step the tool a spawn uses forward (alt+m too)"},
			{"shift+tab", "step the tool a spawn uses back one"},
			{ctx.QuickKeys.Model, "pick the spawn's model from the ones the tool lists; type to filter"},
			{ctx.QuickKeys.Effort, "step the spawn's reasoning effort through the model's levels"},
			{ctx.QuickKeys.Profile, "step the spawn's profile, for a tool that has them"},
			{"click", "step a spawn choice the way its key does, or pick a listed model"},
			{"ctrl+t", "toggle worktree for the spawned agent (alt+w too)"},
			{"ctrl+v", "paste an image as a chip at the cursor"},
			{"⌫", "next to a chip, delete the whole chip"},
			{"←→", "step over a chip as one token"},
			{"esc", "close"},
		}},
		{title: "inside a session (attached or focused)", rows: sessionHelpRows(ctx.SessionKeys, ctx.ArrowStep, [][2]string{
			{"wheel", "focused: scroll the pane's history, type to catch up"},
			{"drag", "focused: select pane text and copy it"},
			{"click the list", "focused: back to the manager (mouse back too)"},
			{"double click", "focused: copy the word"},
			{"triple click", "focused: copy the line"},
			{"click", "focused: open the link under it, else a tracking agent gets it"},
			{"alt+drag", "focused: pass a whole drag to that agent UI"},
		})},
		reviewHelpSection(list),
		{title: titledWith("messages", list, keybind.Messages), rows: [][2]string{
			{"click unread", "in the key legend, shown while any remain"},
			{"↑↓", "pick a message"},
			{"pgup / pgdn", "scroll its body"},
			{"wheel", "scroll its body"},
			{"home / end", "jump to its top or bottom"},
			{"↵", "open its link in the browser"},
			{"u", "on an update message: update and restart"},
			{"r", "refresh releases and messages"},
			{"x", "dismiss it for good"},
			{"esc", "close"},
		}},
		{title: titledWith("settings", list, keybind.Settings), rows: [][2]string{
			{"↑↓", "pick a field"},
			{"←→", "change the value"},
			{"↵", "run the field's action (docs, keybindings, CLIs, report, update)"},
			{"esc", "save and close"},
		}},
		{title: titledWith("dialogs", list, keybind.NewSession, keybind.NewGroup, keybind.Rename, keybind.Fork, keybind.Move), rows: [][2]string{
			{"tab", "next field"},
			{"↑↓", "next field, or step the caret in a taller New Session prompt"},
			{"ctrl+v", "in a prompt field, paste an image as a chip"},
			{"←→", "change a picker's value"},
			{"type", "in the model field, filter what the tool lists; tab fills one in"},
			{"click", "focus a field, click it again to change it, or pick a listed entry"},
			{"↵", "confirm"},
			{"esc", "cancel"},
		}},
	}
}

func sessionHelpRows(keys keybind.Table, arrowStep bool, mouseRows [][2]string) [][2]string {
	rows := [][2]string{{"typing", "goes straight to the agent, q included"}}
	detach := keys.Binding(keybind.Detach).Keys()
	back := "back to the manager"
	if len(detach) > 1 {
		back += " (" + keybind.Keys(detach[1:]...).Label() + " too)"
	}
	rows = append(rows, [2]string{detach[0].Tea(), back})
	if arrowStep {
		rows = append(rows, [2]string{"←", "focused: back to the manager, at the prompt's start"})
	}
	if label := keys.Binding(keybind.Review).Label(); label != "" {
		rows = append(rows, [2]string{label, "review the session's diff, esc returns"})
	}
	if label := keys.Binding(keybind.Editor).Label(); label != "" {
		rows = append(rows, [2]string{label, "open its directory in an editor"})
	}
	if label := keys.Binding(keybind.TmuxPrefix).Label(); label != "" {
		rows = append(rows, [2]string{label, "attached: tmux's prefix, in place of yours"})
	}
	rows = append(rows, [2]string{"pgup/pgdn", "focused: scroll the pane's history on its normal screen"})
	return append(rows, mouseRows...)
}

func titledWith(name string, list keybind.Table, actions ...string) string {
	var glyphs []string
	for _, action := range actions {
		if glyph := list.Binding(action).Glyph(" / "); glyph != "" {
			glyphs = append(glyphs, glyph)
		}
	}
	if len(glyphs) == 0 {
		return name
	}
	return name + " (" + strings.Join(glyphs, ", ") + ")"
}

func reviewHelpSection(list keybind.Table) helpSection {
	return helpSection{title: titledWith("review", list, keybind.Review), rows: [][2]string{
		{"", "Tell your agent what to review in Agent Manager; they set it up."},
		{"c", "comment on the line"},
		{"d", "remove a draft, or mark sent feedback handled / open"},
		{"C", "send drafts to the agent as the next review round"},
		{"space", "mark the file reviewed"},
		{"f", "code files only (hides images, assets, lock files)"},
		{"s", "cycle the scope (uncommitted / vs target / last commit / staged)"},
		{"r", "pick the repo when the session dir holds several"},
		{"b", "switch to another worktree, listed by branch"},
		{"B", "pick the target branch the branch diff compares against"},
		{"↑↓ / jk", "move a line"},
		{"ctrl+d / ctrl+u", "half a page"},
		{"pgup / pgdn", "page"},
		{"g / G", "top / bottom of the file"},
		{"tab / shift+tab", "next / previous file (J / K too)"},
		{"n / N", "next / previous change"},
		{"u", "unified / split layout"},
		{"o / f3", "open the current file in your editor"},
		{"?", "this key map"},
		{"esc / q", "close the review"},
	}}
}

func (s State) visibleSections(ctx Context) []helpSection {
	if s.scope == Review {
		return []helpSection{reviewHelpSection(ctx.ListKeys)}
	}
	return helpSections(ctx)
}

// matchHelp narrows the catalog to rows whose key or description contains
// the query. A matching section title keeps all of that section's rows.
func matchHelp(sections []helpSection, query string) []helpSection {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return sections
	}
	var kept []helpSection
	for _, section := range sections {
		if strings.Contains(strings.ToLower(section.title), query) {
			kept = append(kept, section)
			continue
		}
		var rows [][2]string
		for _, row := range section.rows {
			if strings.Contains(strings.ToLower(row[0]), query) ||
				strings.Contains(strings.ToLower(row[1]), query) {
				rows = append(rows, row)
			}
		}
		if len(rows) > 0 {
			kept = append(kept, helpSection{title: section.title, rows: rows})
		}
	}
	return kept
}

func helpRowCount(sections []helpSection) int {
	count := 0
	for _, section := range sections {
		for _, row := range section.rows {
			if row[0] != "" {
				count++
			}
		}
	}
	return count
}
