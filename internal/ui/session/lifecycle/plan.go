package lifecycle

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
)

// Status is one row's status after a run.
type Status struct {
	ID     string
	Status string
}

// ArchiveEdit is the row and group edits an archive or restore makes to the
// loaded list, so the change shows on this frame instead of after the next
// poll. Rows and Groups carry the value each lands on.
type ArchiveEdit struct {
	Archive bool
	Group   string
	Rows    map[string]bool
	Groups  map[string]bool
}

// Change is what a finished run changes in the manager: the rows whose status
// moved, the rows that left or came back, the panes this run relaunched, and
// what the completion reports.
type Change struct {
	Statuses []Status
	// Forget lists rows whose window is gone: the manager drops their pane,
	// their pending launch and what it held for them.
	Forget []string
	// Edit is the archive or restore the run made; nil for any other action.
	Edit          *ArchiveEdit
	Deleted       []store.Session
	RemovedGroups []string
	Launches      []Relaunch
	// Notices are the lines a completion reports, in the order it reports
	// them.
	Notices []string
	Warning error
	// Survivor is whether the watcher a run stopped may be restarted.
	Survivor bool
	// Refusal is the failure that stopped the run.
	Refusal *Refusal
}

// Refusal is the failure a completion reports, in the shape reporting needs.
type Refusal struct {
	Err error
	// Batch reports a run of several rows, which names its failure only when
	// the run said nothing else and never retries.
	Batch bool
	// ViaLaunchHint routes a single-row failure to the fix card instead of
	// the error bar.
	ViaLaunchHint bool
	// Retry is the request to queue again, or nil to keep the rows as they
	// are.
	Retry *Request
}

// Changes reads a finished run as the edits and reports the manager applies.
func Changes(request Request, result Result, err error) Change {
	target := request.Target
	change := Change{Survivor: result.Survivor}

	for _, sess := range append(append([]store.Session(nil), result.Changed...), result.Archive.Sessions...) {
		change.Statuses = append(change.Statuses, Status{ID: sess.ID, Status: sess.Status})
	}
	if target.Action == Kill {
		for _, sess := range result.Changed {
			change.Forget = append(change.Forget, sess.ID)
		}
	}

	group := ""
	if result.Archive.GroupChanged {
		group = result.Archive.GroupPath
	}
	switch target.Action {
	case Archive, Restore:
		var durable []store.Session
		for _, sess := range result.Archive.Sessions {
			if sess.Archived == (target.Action == Archive) {
				durable = append(durable, sess)
			}
		}
		change.Edit = archiveEdit(target.Action == Archive, durable, group)
		if target.Action == Archive {
			for _, sess := range durable {
				change.Forget = append(change.Forget, sess.ID)
			}
		}
	case Remove:
		change.Deleted = result.Deleted.Deleted
		change.RemovedGroups = result.Deleted.RemovedGroups
	}
	change.Launches = result.Launches

	if result.Notice != "" {
		change.Notices = append(change.Notices, result.Notice)
	}
	if result.Deleted.Notice != "" {
		change.Notices = append(change.Notices, result.Deleted.Notice)
	}
	if result.Warning != nil && err == nil {
		change.Warning = result.Warning
	}
	change.Refusal = refusal(request, result, err)
	return change
}

// refusal is the failure a completion reports, if any.
func refusal(request Request, result Result, err error) *Refusal {
	if err == nil {
		return nil
	}
	target := request.Target
	if target.Batch {
		if result.Notice != "" {
			return nil
		}
		return &Refusal{Err: err, Batch: true}
	}
	if target.Action != Restart && target.Action != Revive && target.Action != Restore {
		return &Refusal{Err: err}
	}
	retry := request
	if len(result.Failed) > 0 {
		retry.Target.Sessions = append([]store.Session(nil), result.Failed...)
	}
	if target.Action == Restore {
		done := make(map[string]bool, len(result.Archive.Sessions))
		for _, sess := range result.Archive.Sessions {
			if !sess.Archived {
				done[sess.ID] = true
			}
		}
		retry.Target.Sessions = nil
		for _, sess := range target.Sessions {
			if !done[sess.ID] {
				retry.Target.Sessions = append(retry.Target.Sessions, sess)
			}
		}
	}
	return &Refusal{Err: err, ViaLaunchHint: true, Retry: &retry}
}

// archiveEdit is the row and group edit an archive or restore makes: the rows
// it wrote, the group path it covered whole, and the loaded groups under it.
func archiveEdit(archive bool, rows []store.Session, group string) *ArchiveEdit {
	edit := &ArchiveEdit{Archive: archive, Group: group, Rows: map[string]bool{}, Groups: map[string]bool{}}
	for _, sess := range rows {
		edit.Rows[sess.ID] = true
	}
	if group != "" {
		edit.Groups[group] = true
	}
	return edit
}

// Subgroups is every loaded group path under path.
func Subgroups(groups []string, path string) []string {
	var out []string
	prefix := path + "/"
	for _, group := range groups {
		if strings.HasPrefix(group, prefix) {
			out = append(out, group)
		}
	}
	return out
}

// RemoveSession takes a row out of the loaded list.
func RemoveSession(sessions []store.Session, id string) []store.Session {
	for i := range sessions {
		if sessions[i].ID == id {
			return append(sessions[:i:i], sessions[i+1:]...)
		}
	}
	return sessions
}

// DropGroups takes the removed group paths out of the loaded list.
func DropGroups(groups []string, removed []string) []string {
	gone := make(map[string]bool, len(removed))
	for _, path := range removed {
		gone[path] = true
	}
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		if !gone[group] {
			out = append(out, group)
		}
	}
	return out
}

// AfterTurnApply is what a finished after-turn run changes: the lifecycle
// change it carries when it archived or killed, the row whose request it
// cleared, and the line the clear reports.
type AfterTurnApply struct {
	Change *Change
	// ChangeRequest is the lifecycle request the change was made against. Its
	// rows are the ones the run touched.
	ChangeRequest Request
	ClearID       string
	Done          string
	Survivor      bool
	Err           error
}

// AfterTurn reads a finished after-turn run as the edits and reports the
// manager applies. A run that archived or killed carries a lifecycle change;
// one that only cleared a request carries the row to clear.
func AfterTurn(request AfterTurnRequest, result AfterTurnResult, err error) AfterTurnApply {
	apply := AfterTurnApply{Survivor: result.Lifecycle.Survivor, Done: request.Name, Err: err}
	if result.Target.Action != None {
		change := Changes(Request{Target: result.Target, Watched: request.Watched}, result.Lifecycle, err)
		apply.Change = &change
	}
	if result.Cleared {
		apply.ClearID = request.ID
		if !request.Cancel {
			apply.Done = ""
		}
	}
	return apply
}

// Acknowledge decides whether the mark-idle key applies: archived rows keep
// their preserved status, since the poller never re-derives it for them, so an
// ack would stick forever.
func Acknowledge(sess store.Session, ok bool) (string, bool) {
	if !ok || sess.Archived || sess.Status != status.Finished {
		return "", false
	}
	return sess.ID, true
}
