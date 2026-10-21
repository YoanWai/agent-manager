package rail

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

// ReviewModeHint is the row tier's reminder that an agent can be asked to
// open Review itself.
const ReviewModeHint = `tell your agent "set review mode"`

// Legend is one footer tier Rail decides: its title, its bindings, and
// whether it recedes behind the tier above it. Root paints it with the
// app's legend bar.
type Legend struct {
	Title  string
	Leads  [][2]string
	Alerts [][2]string
	Hint   string
	Pairs  [][2]string
	Quiet  bool
}

// LegendContext carries the root facts a legend reads. Row and Session are
// root's current record of the row under the cursor, set only when RowOK;
// Session is filled for session rows. Unread is the messages alert, empty
// when nothing is unread.
type LegendContext struct {
	ListKeys      keybind.Table
	EnterFocuses  bool
	MouseDisabled bool
	ArrowStep     bool
	FullLayout    bool
	Row           Selection
	RowOK         bool
	Session       Session
	Unread        [2]string
}

// RowLegend is the tier for the entry under the cursor: what this session or
// this group can be told to do.
func (m Model) RowLegend(ctx LegendContext) Legend {
	k := ctx.glyph
	enterHint, attachHint := "focus / fold", "attach"
	if !ctx.EnterFocuses {
		enterHint, attachHint = "attach / fold", "focus"
	}
	if !ctx.RowOK {
		return Legend{}
	}
	if ctx.Row.Kind == GroupRow {
		foldAction := "fold"
		if m.IsCollapsed(ctx.Row.Group) {
			foldAction = "unfold"
		}
		pairs := [][2]string{{k(keybind.Open), foldAction}}
		if !ctx.MouseDisabled {
			pairs = append(pairs, [2]string{"double click", foldAction})
		}
		if ctx.ArrowStep {
			pairs = append(pairs, ctx.pair(keybind.StepOut, "close", keybind.StepIn, "open"))
		}
		pairs = append(pairs, [][2]string{
			{k(keybind.Editor), "editor"}, {k(keybind.Rename), "rename"}, {k(keybind.Move), "move"},
			ctx.pair(keybind.Kill, "kill", keybind.KillAll, "all"), ctx.pair(keybind.Revive, "revive", keybind.ReviveAll, "all"),
			m.archiveRestoreLegend(ctx), {k(keybind.Delete), "delete"},
		}...)
		return Legend{Title: "Group", Leads: [][2]string{ctx.quickModeLead()}, Pairs: boundPairs(pairs)}
	}
	session := ctx.Session
	title := "Session"
	leads := [][2]string{ctx.quickModeLead(), {k(keybind.Review), "review mode"}}
	hint := ""
	if k(keybind.Review) != "" {
		hint = ReviewModeHint
	}
	conversation := [][2]string{{k(keybind.CopyReply), "copy"}, {k(keybind.Fork), "fork"}}
	if session.IsShell {
		// A shell has no conversation, so the keys that would prompt,
		// review or fork one are left off rather than offered and refused.
		title, leads, hint, conversation = "Shell", nil, "", nil
	}
	pairs := [][2]string{{k(keybind.Open), enterHint}, {k(keybind.Attach), attachHint}}
	if !ctx.MouseDisabled {
		gesture := "double click"
		if !ctx.FullLayout {
			gesture = "click"
		}
		pairs = append(pairs, [2]string{gesture, "focus"})
	}
	if ctx.ArrowStep {
		pairs = append(pairs, [2]string{k(keybind.StepIn), "focus"})
	}
	if session.Status == "finished" && !session.Archived {
		pairs = append(pairs, [2]string{k(keybind.MarkIdle), "mark idle"})
	}
	if session.AfterTurn != "" {
		pairs = append(pairs, [2]string{k(keybind.CancelEnd), "cancel " + session.AfterTurn})
	}
	pairs = append(pairs, conversation...)
	// o sits outside the conversation keys: a shell's directory is worth
	// opening as much as an agent's.
	pairs = append(pairs, [][2]string{
		{k(keybind.Editor), "editor"}, {k(keybind.Rename), "rename"}, {k(keybind.Move), "move"},
		ctx.pair(keybind.Kill, "kill", keybind.KillAll, "all"), ctx.pair(keybind.Revive, "revive", keybind.ReviveAll, "all"), {k(keybind.Restart), "restart"},
		m.archiveRestoreLegend(ctx), {k(keybind.Delete), "delete"},
	}...)
	return Legend{Title: title, Leads: leads, Hint: hint, Pairs: boundPairs(pairs)}
}

// ViewLegend is the tier that never changes with the cursor: moving around
// the list, filtering it, and leaving.
func (m Model) ViewLegend(ctx LegendContext) Legend {
	emptyGroupsAction := "hide empty"
	if m.HideEmptyGroups() {
		emptyGroupsAction = "show empty"
	}
	statusFilterAction := "attention"
	if m.FilteringAttention() {
		statusFilterAction = "show all"
	}
	archivedAction := "archived"
	if m.ShowArchived() {
		archivedAction = "back to active"
	}
	foldAllAction := "fold all"
	if m.AllGroupsCollapsed() {
		foldAllAction = "unfold all"
	}
	// Ordered by what a narrow terminal must keep: unread messages, which
	// the footer is the only mouse path to, moving around, making
	// something, the filters, then the keys a user already knows to look for.
	k := ctx.glyph
	emptyGroupsKey := k(keybind.EmptyGroups)
	if m.ShowArchived() {
		emptyGroupsKey = ""
	}
	var alerts [][2]string
	if ctx.Unread[0] != "" {
		alerts = append(alerts, ctx.Unread)
	}
	pairs := [][2]string{
		{strings.TrimSpace(k(keybind.Up) + " " + k(keybind.Down)), "navigate"},
		{k(keybind.NewSession), "new"}, {k(keybind.Terminal), "terminal"}, {k(keybind.NewGroup), "group"}, {k(keybind.Search), "search"},
		{k(keybind.Archived), archivedAction}, {k(keybind.Filter), statusFilterAction}, {emptyGroupsKey, emptyGroupsAction},
		{k(keybind.Help), "keys"}, {k(keybind.Quit), "quit"}, {k(keybind.NewConnection), "connection"},
		{k(keybind.ReorderUp, keybind.ReorderDown), "reorder"}, {k(keybind.FoldAll), foldAllAction}, {k(keybind.Resize), "resize"}, {k(keybind.Settings), "settings"},
	}
	return Legend{Title: "View", Quiet: true, Alerts: alerts, Pairs: boundPairs(pairs)}
}

// ReorderLegend is the tier a lifted row shows while it moves.
func (m Model) ReorderLegend() Legend {
	info := m.ReorderInfo()
	if info.DropLabel != "" {
		return Legend{Title: "Move", Pairs: [][2]string{
			{"release", "move " + info.DropLabel}, {"esc", "put back"},
		}}
	}
	return Legend{Title: "Reorder", Pairs: [][2]string{
		{"drag / ↑↓ / wheel", "move"}, {"↵ / release", "drop"}, {"esc", "put back"},
	}}
}

func (ctx LegendContext) quickModeLead() [2]string {
	return [2]string{ctx.glyph(keybind.Prompt), "quick prompt mode"}
}

// archiveRestoreLegend leaves out the key of the pair that no-ops in this view.
func (m Model) archiveRestoreLegend(ctx LegendContext) [2]string {
	if m.ShowArchived() {
		return [2]string{ctx.glyph(keybind.Restore), "restore"}
	}
	return [2]string{ctx.glyph(keybind.Archive), "archive"}
}

func (ctx LegendContext) pair(first, firstLabel, second, secondLabel string) [2]string {
	switch {
	case ctx.glyph(first) == "":
		return [2]string{ctx.glyph(second), secondLabel}
	case ctx.glyph(second) == "":
		return [2]string{ctx.glyph(first), firstLabel}
	}
	return [2]string{ctx.glyph(first, second), firstLabel + " / " + secondLabel}
}

func boundPairs(pairs [][2]string) [][2]string {
	kept := pairs[:0]
	for _, pair := range pairs {
		if pair[0] != "" {
			kept = append(kept, pair)
		}
	}
	return kept
}

func (ctx LegendContext) glyph(actions ...string) string {
	joined := ""
	for _, action := range actions {
		glyph := ctx.ListKeys.Binding(action).Glyph("/")
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

// RemoteRow is what the footer needs to know about a row behind an SSH
// connection.
type RemoteRow struct {
	Kind     RowKind
	Host     string
	Terminal bool
	Dead     bool
}

// RemoteLegend is the tier for a connection's rows: only the keys the
// connection carries out.
func (m Model) RemoteLegend(ctx LegendContext, row RemoteRow, collapsed bool) Legend {
	k := ctx.glyph
	fold := "fold"
	if collapsed {
		fold = "unfold"
	}
	leads := [][2]string{ctx.quickModeLead()}
	switch row.Kind {
	case ConnectionRow:
		return Legend{Title: "SSH connection", Leads: leads, Pairs: boundPairs([][2]string{
			{k(keybind.Open), fold}, {k(keybind.Terminal), "terminal"}, {k(keybind.NewGroup), "group"}, {k(keybind.Rename), "edit"}, {k(keybind.Delete), "remove"},
		})}
	case GroupRow:
		return Legend{Title: "Group", Leads: leads, Pairs: boundPairs([][2]string{
			{k(keybind.Open), fold}, {k(keybind.Terminal), "terminal"}, {k(keybind.NewGroup), "group"},
		})}
	}
	title, kill := "Session", "kill"
	if row.Terminal {
		title, kill, leads = "Shell", "close", nil
	}
	pairs := [][2]string{{k(keybind.Open, keybind.Attach), "attach over SSH"}, {k(keybind.Kill), kill}}
	if row.Dead {
		pairs = append(pairs, [2]string{k(keybind.Revive), "revive"})
	}
	pairs = append(pairs, m.archiveRestoreLegend(ctx))
	return Legend{Title: title + " on " + row.Host, Leads: leads, Pairs: boundPairs(pairs)}
}
