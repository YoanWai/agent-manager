// Package dialogs is what the session keys decide before a card opens: which
// rows an archive, restore, kill, delete, revive or restart covers, and the
// words the card asks with. The root captures the rows and paints the card;
// the choice made here travels back as an Intent.
package dialogs

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

// Selection names the rows a confirmation re-reads before acting, so a
// selection that changed since the card opened fails instead of acting on a
// set the user never saw.
type Selection struct {
	Kind         SelectionKind
	RootID       string
	ArchivedOnly bool
}

// SelectionKind is what a Selection names.
type SelectionKind uint8

const (
	SelectNone SelectionKind = iota
	SelectSession
	SelectGroup
)

// Card is one confirmation as the root opens it.
type Card struct {
	Group bool
	// ArchivedOnly marks a group delete issued from the archived view, which
	// clears the group's archive instead of the group itself.
	ArchivedOnly bool
	Path         string
	Label        string
	Sessions     []store.Session
	Action       uidialogs.ConfirmAction
	Batch        bool
	Selection    Selection
}

// Intent is what a key decided: the card to open, the batch to queue with no
// card first, or the message that says why it did nothing.
type Intent struct {
	Card Card
	// Run queues the batch itself; the card names the action and rows.
	Run bool
	// AllowLive admits rows a batch of the same action would refuse for
	// still running.
	AllowLive bool
	// EmptyNotice is what a run with nothing to do reports.
	EmptyNotice string
	// Notice is what a refusal reports instead of a card.
	Notice string
	// ClearsErr drops the stale error line a card replaces.
	ClearsErr bool
}

// Nothing reports a decision that changes nothing on screen.
func (i Intent) Nothing() bool { return i.Notice == "" && !i.Run && i.Card.Label == "" }

// RowKind is the shape of the selected row.
type RowKind uint8

const (
	// SessionRow is one agent or shell row.
	SessionRow RowKind = iota
	// GroupRow is a group.
	GroupRow
	// RootRow is the top level, which no session action applies to.
	RootRow
)

// Row is the selected row as the decisions read it.
type Row struct {
	Kind  RowKind
	Group string
	Sess  store.Session
}

// View is what the decisions read from the manager: the loaded rows, the rows
// the Rail lists, the group paths, the view on screen, and what the last poll,
// or a launch since, says about each row's window.
type View interface {
	Sessions() []store.Session
	Listed() []store.Session
	Groups() []string
	ArchivedView() bool
	Live(id string) bool
}

// Children is a session with the shells nested under it.
func Children(sessions []store.Session, sess store.Session) []store.Session {
	out := make([]store.Session, 0, 1)
	out = append(out, sess)
	for _, candidate := range sessions {
		if candidate.ParentID == sess.ID {
			out = append(out, candidate)
		}
	}
	return out
}

// Subtree is every loaded session at or below a group path.
func Subtree(sessions []store.Session, path string) []store.Session {
	var out []store.Session
	for _, sess := range sessions {
		if uirail.InGroupSubtree(sess.Group, path) {
			out = append(out, sess)
		}
	}
	return out
}

// InGroup is the rows a view lists at or below a group path.
func InGroup(listed []store.Session, path string) []store.Session {
	var out []store.Session
	for _, sess := range listed {
		if uirail.InGroupSubtree(sess.Group, path) {
			out = append(out, sess)
		}
	}
	return out
}

// Archived keeps the rows already in the archive.
func Archived(sessions []store.Session) []store.Session {
	var out []store.Session
	for _, sess := range sessions {
		if sess.Archived {
			out = append(out, sess)
		}
	}
	return out
}

// Dead keeps the rows whose agent is gone.
func Dead(sessions []store.Session) []store.Session {
	var out []store.Session
	for _, sess := range sessions {
		if sess.Status == status.Dead {
			out = append(out, sess)
		}
	}
	return out
}

// live keeps the rows whose window the manager still projects as up.
func live(view View, sessions []store.Session) []store.Session {
	var out []store.Session
	for _, sess := range sessions {
		if view.Live(sess.ID) {
			out = append(out, sess)
		}
	}
	return out
}

// Archive is what the archive key asks for.
func Archive(view View, row Row) Intent {
	if view.ArchivedView() {
		return Intent{}
	}
	if row.Kind == RootRow {
		return Intent{Notice: "root is the top level, not a group to archive"}
	}
	if row.Kind == GroupRow {
		subtree := Subtree(view.Sessions(), row.Group)
		return Intent{ClearsErr: true, Card: Card{
			Group:     true,
			Path:      row.Group,
			Action:    uidialogs.ConfirmArchive,
			Sessions:  subtree,
			Label:     fmt.Sprintf("archive group %s (%d sessions)? frees their RAM, t to find them.", row.Group, len(subtree)),
			Selection: Selection{Kind: SelectGroup},
		}}
	}
	sessions := Children(view.Sessions(), row.Sess)
	return Intent{ClearsErr: true, Card: Card{
		Action:   uidialogs.ConfirmArchive,
		Sessions: sessions,
		Label: uisession.FollowConfirmLabel("archive", row.Sess.Name, len(sessions)-1,
			"frees its RAM, t to find it.",
			"frees their RAM, t to find them."),
		Selection: Selection{Kind: SelectSession, RootID: row.Sess.ID},
	}}
}

// Restore is what the restore key asks for in the archived view.
func Restore(view View, row Row) Intent {
	if !view.ArchivedView() {
		return Intent{}
	}
	if row.Kind == RootRow {
		return Intent{Notice: "root is the top level, not a group to restore"}
	}
	if row.Kind == GroupRow {
		archived := Archived(Subtree(view.Sessions(), row.Group))
		return Intent{ClearsErr: true, Card: Card{
			Group:     true,
			Path:      row.Group,
			Action:    uidialogs.ConfirmRestore,
			Sessions:  archived,
			Label:     fmt.Sprintf("restore group %s (%d archived sessions)? brings them back.", row.Group, len(archived)),
			Selection: Selection{Kind: SelectGroup, ArchivedOnly: true},
		}}
	}
	sessions := Archived(Children(view.Sessions(), row.Sess))
	return Intent{ClearsErr: true, Card: Card{
		Action:   uidialogs.ConfirmRestore,
		Sessions: sessions,
		Label: uisession.FollowConfirmLabel("restore", row.Sess.Name, len(sessions)-1,
			"brings it back.",
			"brings them back."),
		Selection: Selection{Kind: SelectSession, RootID: row.Sess.ID, ArchivedOnly: true},
	}}
}

// Kill is what the kill key asks for: a group's live rows, or one session and
// the shells nested under it.
func Kill(view View, row Row) Intent {
	if row.Kind == GroupRow {
		live := live(view, InGroup(view.Listed(), row.Group))
		if len(live) == 0 {
			return Intent{Notice: "no live sessions to kill in " + row.Group}
		}
		return Intent{Card: Card{
			Group:    true,
			Path:     row.Group,
			Action:   uidialogs.ConfirmKill,
			Sessions: live,
			Label: fmt.Sprintf("kill group %s (%d live sessions)? frees their RAM, v revives them.",
				row.Group, len(live)),
		}}
	}
	sessions := Children(view.Sessions(), row.Sess)
	live := false
	for _, sess := range sessions {
		if view.Live(sess.ID) {
			live = true
			break
		}
	}
	if !live {
		return Intent{Notice: row.Sess.Name + " is already dead"}
	}
	return Intent{Card: Card{
		Action:   uidialogs.ConfirmKill,
		Sessions: sessions,
		Label: uisession.FollowConfirmLabel("kill", row.Sess.Name, len(sessions)-1,
			"frees its RAM, v revives it.",
			"frees their RAM, v revives them."),
		Selection: Selection{Kind: SelectSession, RootID: row.Sess.ID},
	}}
}

// KillAll is what the kill-everything key asks for over the rows a view lists.
func KillAll(view View) Intent {
	live := live(view, view.Listed())
	if len(live) == 0 {
		return Intent{Notice: "no live sessions to kill"}
	}
	return Intent{Card: Card{
		Action:   uidialogs.ConfirmKill,
		Sessions: live,
		Label:    fmt.Sprintf("kill every live session (%d)? frees their RAM, v revives them.", len(live)),
	}}
}

// Restart is what a restart asks for: never a group row.
func Restart(view View, row Row) Intent {
	if row.Kind == GroupRow {
		return Intent{Notice: "restart applies to a session; pick one under " + uiframe.DisplayGroup(row.Group)}
	}
	label := fmt.Sprintf("restart %s with an empty context? its current conversation is left behind.", row.Sess.Name)
	if view.Live(row.Sess.ID) {
		label = fmt.Sprintf("restart %s with an empty context? ends the running agent and leaves its conversation behind.", row.Sess.Name)
	}
	return Intent{Card: Card{
		Action:   uidialogs.ConfirmRestart,
		Sessions: []store.Session{row.Sess},
		Label:    label,
	}}
}

// Revive is what the revive key asks for on a session row, or over a group's
// subtree: one card for several dead rows, the run itself for one.
func Revive(view View, row Row) Intent {
	if row.Kind == GroupRow {
		sessions := InGroup(view.Listed(), row.Group)
		if dead := Dead(sessions); len(dead) > 1 {
			return Intent{Card: Card{
				Group:    true,
				Path:     row.Group,
				Action:   uidialogs.ConfirmRevive,
				Batch:    true,
				Sessions: dead,
				Label: fmt.Sprintf("revive group %s (%d dead sessions)? brings them back.",
					uiframe.DisplayGroup(row.Group), len(dead)),
			}}
		}
		return ReviveMany(sessions, "no dead sessions to revive in "+row.Group)
	}
	set := Children(view.Sessions(), row.Sess)
	dead := false
	for _, sess := range set {
		if !view.Live(sess.ID) {
			dead = true
			break
		}
	}
	if len(set) > 1 && dead {
		return Intent{Card: Card{
			Action:   uidialogs.ConfirmRevive,
			Sessions: set,
			Label: uisession.FollowConfirmLabel("revive", row.Sess.Name, len(set)-1,
				"brings it back.",
				"brings them back."),
			Selection: Selection{Kind: SelectSession, RootID: row.Sess.ID},
		}}
	}
	return Intent{Run: true, AllowLive: true,
		Card: Card{Action: uidialogs.ConfirmRevive, Sessions: []store.Session{row.Sess}}}
}

// ReviveAll is what the revive-everything key asks for over the rows a view
// lists.
func ReviveAll(listed []store.Session) Intent {
	if dead := Dead(listed); len(dead) > 1 {
		return Intent{Card: Card{
			Action:   uidialogs.ConfirmRevive,
			Batch:    true,
			Sessions: dead,
			Label:    fmt.Sprintf("revive every dead session (%d)? brings them back.", len(dead)),
		}}
	}
	return ReviveMany(listed, "no dead sessions to revive")
}

// ReviveMany is the run with no card to open first.
func ReviveMany(sessions []store.Session, emptyNotice string) Intent {
	return Intent{Run: true, EmptyNotice: emptyNotice,
		Card: Card{Action: uidialogs.ConfirmRevive, Batch: true, Sessions: Dead(sessions)}}
}

// Delete is what the delete key asks for: the row and the shells under it, or
// the whole group as the view showing it sees it.
func Delete(view View, row Row) Intent {
	if row.Kind == RootRow {
		return Intent{Notice: "root is the top level; delete the sessions under it instead"}
	}
	if row.Kind == SessionRow {
		sessions := Children(view.Sessions(), row.Sess)
		return Intent{Card: Card{
			Label: uisession.FollowConfirmLabel("delete", row.Sess.Name, len(sessions)-1,
				"kills its tmux session.",
				"kills their tmux sessions."),
			Sessions:  sessions,
			Selection: Selection{Kind: SelectSession, RootID: row.Sess.ID},
		}}
	}
	subtree := Subtree(view.Sessions(), row.Group)
	if view.ArchivedView() {
		return Intent{Card: archivedGroupDelete(row.Group, subtree)}
	}
	return Intent{Card: wholeGroupDelete(view.Groups(), row.Group, subtree)}
}

// CancelEnd is what the keep-past-its-turn key asks for.
func CancelEnd(row Row, has bool) (id, name string, ok bool) {
	if !has || row.Kind == GroupRow || row.Sess.AfterTurn == "" {
		return "", "", false
	}
	return row.Sess.ID, row.Sess.Name, true
}

// wholeGroupDelete targets the group as the active view shows it: the group
// ceases to exist, so its subtree goes with it, archived sessions included,
// leaving nothing stranded under a group that is gone.
func wholeGroupDelete(groups []string, path string, subtree []store.Session) Card {
	subgroups := 0
	for _, g := range groups {
		if strings.HasPrefix(g, path+"/") {
			subgroups++
		}
	}
	return Card{
		Group:    true,
		Path:     path,
		Sessions: subtree,
		Label: fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions.",
			path, subgroups, len(subtree)),
		Selection: Selection{Kind: SelectGroup},
	}
}

// archivedGroupDelete targets only what the archived view shows: the archived
// sessions under the group. The live sessions and the group itself belong to
// the active view and survive; the group row goes only once nothing is left
// beneath it.
func archivedGroupDelete(path string, subtree []store.Session) Card {
	archived := Archived(subtree)
	return Card{
		Group:        true,
		ArchivedOnly: true,
		Path:         path,
		Sessions:     archived,
		Label: fmt.Sprintf("delete %s from the archive (%d archived sessions)? kills their tmux sessions, live ones stay.",
			path, len(archived)),
		Selection: Selection{Kind: SelectGroup, ArchivedOnly: true},
	}
}
