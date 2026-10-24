package lifecycle

import (
	"errors"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

// Action is what a confirmed session action does. A run that found nothing to
// do reports None; the rest are the confirm cards' actions.
type Action string

const (
	None    Action = ""
	Archive Action = "archive"
	Restore Action = "restore"
	Kill    Action = "kill"
	Restart Action = "restart"
	Revive  Action = "revive"
	// Remove deletes the row; the confirm card spells it "delete".
	Remove Action = "delete"
)

// Selection names the rows a confirmation re-reads before acting, so a
// selection that changed since the card opened fails instead of acting on a
// set the user never saw.
type SelectionKind uint8

const (
	SelectNone SelectionKind = iota
	SelectSession
	SelectGroup
)

// Selection is the row or group a confirmation was opened on.
type Selection struct {
	Kind         SelectionKind
	RootID       string
	ArchivedOnly bool
}

// Target is a confirmed action: what it does, the rows it holds and the
// group it covers.
type Target struct {
	Action       Action
	Sessions     []store.Session
	Path         string
	Batch        bool
	ArchivedOnly bool
	Selection    Selection
}

// Relaunch is one session a restart, revive or restore brought back.
type Relaunch struct {
	ID     string
	Result sessioncmd.RelaunchResult
}

// Result is what a lifecycle run did: the archive or delete outcome, the rows
// whose status changed, the sessions it relaunched, and what a confirmation
// should report.
type Result struct {
	Archive  sessioncmd.ArchiveResult
	Deleted  sessioncmd.DeleteResult
	Changed  []store.Session
	Launches []Relaunch
	Failed   []store.Session
	Survivor bool
	Notice   string
	Warning  error
}

// Watcher stops watching a pane the action is about to end, so the client
// going away reads as the plan instead of a loss.
type Watcher interface {
	Unwatch(id string)
}

// Services is what a lifecycle run asks of the manager.
type Services struct {
	Lifecycle *sessioncmd.Lifecycle
	Store     *store.Store
	Driver    *tmux.Driver
	Watch     Watcher
	// Reflow holds the panes a run touches so no input job writes into a
	// session it is about to kill or relaunch; nil runs the work directly.
	Reflow func(ids []string, work func()) error
}

// Run carries out a confirmed session action off the event loop.
func Run(services Services, request Request) (Result, error) {
	result := Result{}
	target, err := validateSelection(services.Store, request.Target)
	if err != nil {
		return result, err
	}
	ids := make([]string, 0, len(target.Sessions))
	for _, sess := range target.Sessions {
		ids = append(ids, sess.ID)
	}
	var operationErr error
	reflowErr := reflow(services, ids, func() {
		if services.Watch != nil && (target.Action == Archive || target.Action == Remove || target.Action == Kill || target.Action == Restart) {
			for _, id := range ids {
				services.Watch.Unwatch(id)
			}
		}
		switch target.Action {
		case Archive:
			result.Archive, operationErr = services.Lifecycle.ArchiveForHuman(sessioncmd.ArchiveSelection{Sessions: target.Sessions, GroupPath: target.Path})
		case Restore:
			wasDead := map[string]bool{}
			for _, sess := range target.Sessions {
				wasDead[sess.ID] = !services.Driver.Exists(sess.ID)
			}
			result.Archive, operationErr = services.Lifecycle.RestoreForHuman(sessioncmd.ArchiveSelection{Sessions: target.Sessions, GroupPath: target.Path}, request.Pane)
			for _, sess := range result.Archive.Sessions {
				if wasDead[sess.ID] {
					result.Launches = append(result.Launches, Relaunch{sess.ID, sessioncmd.RelaunchResult{LaunchedAt: sess.AgentLaunchedAt}})
				}
			}
			result.Warning = result.Archive.LabelError
		case Remove, None:
			result.Deleted, operationErr = services.Lifecycle.DeleteForHuman(sessioncmd.DeleteSelection{Sessions: target.Sessions, GroupPath: target.Path, ArchivedOnly: target.ArchivedOnly})
		case Kill, Restart, Revive:
			for i, sess := range target.Sessions {
				var err error
				switch target.Action {
				case Kill:
					if !services.Driver.Exists(sess.ID) {
						continue
					}
					var killed store.Session
					killed, err = services.Lifecycle.Kill(sess)
					if err == nil {
						result.Changed = append(result.Changed, killed)
					}
				case Restart, Revive:
					if target.Action == Revive && !request.AllowLive && services.Driver.Exists(sess.ID) {
						continue
					}
					var launched sessioncmd.RelaunchResult
					if target.Action == Restart {
						launched, err = services.Lifecycle.Restart(sess, request.Pane)
					} else {
						launched, err = services.Lifecycle.Revive(sess, request.Pane)
					}
					if err == nil {
						result.Launches = append(result.Launches, Relaunch{sess.ID, launched})
						if launched.LabelError != nil {
							result.Warning = launched.LabelError
						}
					}
				}
				if err != nil {
					if operationErr == nil {
						operationErr = err
					}
					if target.Batch {
						result.Failed = append(result.Failed, sess)
						continue
					}
					result.Failed = append(result.Failed, target.Sessions[i:]...)
					break
				}
			}
		default:
			operationErr = fmt.Errorf("unknown confirm action %d", target.Action)
		}
		if request.Watched != "" {
			result.Survivor = services.Driver.Exists(request.Watched)
		}
	})
	if reflowErr != nil {
		return result, reflowErr
	}
	// The notice reads the revived row: the listed copy can predate the
	// conversation the agent last reported.
	if target.Action == Revive && !target.Batch && len(target.Sessions) == 1 && len(result.Launches) == 1 && result.Launches[0].Result.Degraded {
		result.Notice = DegradedResumeText(result.Launches[0].Result.Session.Name)
	}
	if target.Action == Revive && target.Batch {
		switch {
		case operationErr != nil:
			result.Notice = fmt.Sprintf("revived %d, first error: %s", len(result.Launches), operationErr)
		case len(result.Launches) == 0:
			result.Notice = request.EmptyNotice
		default:
			degraded := 0
			for _, each := range result.Launches {
				if each.Result.Degraded {
					degraded++
				}
			}
			if degraded > 0 {
				result.Notice = fmt.Sprintf("revived %d, %d without a captured id (used --continue)", len(result.Launches), degraded)
			}
		}
	}
	return result, operationErr
}

// validateSelection re-reads the rows a confirmation held, so an action never
// runs on a selection that changed under the card.
func validateSelection(st *store.Store, target Target) (Target, error) {
	selection := target.Selection
	if selection.Kind == SelectNone {
		return target, nil
	}
	var (
		current []store.Session
		err     error
	)
	switch selection.Kind {
	case SelectSession:
		root, getErr := st.Get(selection.RootID)
		if getErr != nil {
			return target, getErr
		}
		children, childrenErr := st.Children(selection.RootID)
		if childrenErr != nil {
			return target, childrenErr
		}
		current = append([]store.Session{root}, children...)
	case SelectGroup:
		current, err = st.SessionsInSubtree(target.Path)
		if err != nil {
			return target, err
		}
	default:
		return target, fmt.Errorf("unknown lifecycle selection kind %d", selection.Kind)
	}
	if selection.ArchivedOnly {
		current = archivedSessions(current)
	}
	if !sameSessionIDs(current, target.Sessions) {
		return target, errors.New("sessions changed since confirmation; review the selection and try again")
	}
	target.Sessions = current
	return target, nil
}

func sameSessionIDs(left, right []store.Session) bool {
	if len(left) != len(right) {
		return false
	}
	ids := make(map[string]bool, len(left))
	for _, sess := range left {
		ids[sess.ID] = true
	}
	for _, sess := range right {
		if !ids[sess.ID] {
			return false
		}
		delete(ids, sess.ID)
	}
	return len(ids) == 0
}

func archivedSessions(sessions []store.Session) []store.Session {
	var archived []store.Session
	for _, sess := range sessions {
		if sess.Archived {
			archived = append(archived, sess)
		}
	}
	return archived
}

func reflow(services Services, ids []string, work func()) error {
	if services.Reflow == nil {
		work()
		return nil
	}
	return services.Reflow(ids, work)
}

// Request is one queued session action: the target, the pane size a relaunch
// draws at, the session whose watcher a quit cares about, and the notice a
// batch revive with nothing to do reports.
type Request struct {
	Target      Target
	Pane        sessioncmd.PaneSize
	Watched     string
	AllowLive   bool
	EmptyNotice string
}

// AfterTurnRequest is the archive or kill a session asked for once its turn
// ends, or the withdrawal of that ask.
type AfterTurnRequest struct {
	ID      string
	Name    string
	Cancel  bool
	Watched string
}

// AfterTurnResult is what an after-turn run did: the action it took, its
// lifecycle result, and whether the ask was cleared.
type AfterTurnResult struct {
	Target    Target
	Lifecycle Result
	Cleared   bool
}

// RunAfterTurn carries out, or withdraws, the archive or kill a session asked
// for once its turn ended. EndAfterTurn rereads the row, because the poll
// report that queued it can trail a newer turn or an earlier pass's work.
func RunAfterTurn(services Services, request AfterTurnRequest) (AfterTurnResult, error) {
	result := AfterTurnResult{}
	if request.Cancel {
		err := services.Store.ClearAfterTurn(request.ID)
		result.Cleared = err == nil
		return result, err
	}
	children, err := services.Store.Children(request.ID)
	if err != nil {
		return result, err
	}
	ids := []string{request.ID}
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	var ended sessioncmd.EndAfterTurnResult
	var endErr error
	reflowErr := reflow(services, ids, func() {
		// Stop watching first, so the watcher does not report a kill it asked
		// for as a lost client.
		if services.Watch != nil {
			for _, id := range ids {
				services.Watch.Unwatch(id)
			}
		}
		ended, endErr = services.Lifecycle.EndAfterTurn(request.ID)
		if request.Watched != "" {
			result.Lifecycle.Survivor = services.Driver.Exists(request.Watched)
		}
	})
	if reflowErr != nil {
		return result, reflowErr
	}
	switch ended.Action {
	case store.AfterTurnArchive:
		result.Target = Target{Action: Archive, Sessions: ended.Sessions}
		result.Lifecycle.Archive.Sessions = ended.Sessions
	case store.AfterTurnKill:
		result.Target = Target{Action: Kill, Sessions: ended.Sessions}
		result.Lifecycle.Changed = ended.Sessions
	}
	result.Cleared = ended.Action != ""
	return result, endErr
}

// AttachRequest is a session to enter, with the fences that say whether the
// screen it was asked from is still the screen it lands on.
type AttachRequest struct {
	ID       string
	Reattach bool
}

// AttachResult is what preparing an entry found: a warning that does not stop
// the attach, and the failure that does.
type AttachResult struct {
	Warning error
	Fatal   error
}

// DeadSessionHint is what an attach reports when the pane is already gone.
const DeadSessionHint = uisession.DeadHint

// RunAttach probes the pane, acknowledges a finished row and prepares the
// attach, all before the terminal is handed over.
func RunAttach(services Services, request AttachRequest) (AttachResult, error) {
	result := AttachResult{}
	err := reflow(services, []string{request.ID}, func() {
		exists, probeErr := services.Driver.SessionExists(request.ID)
		if probeErr != nil {
			result.Fatal = probeErr
			return
		}
		if !exists {
			result.Fatal = errors.New(DeadSessionHint)
			return
		}
		sess, readErr := services.Store.Get(request.ID)
		if readErr != nil {
			result.Fatal = readErr
			return
		}
		if sess.Status == status.Finished {
			if ackErr := services.Store.AcknowledgeFinished(request.ID); ackErr != nil {
				result.Fatal = ackErr
				return
			}
		}
		result.Warning = services.Driver.PrepareAttach(request.ID)
	})
	if err != nil {
		return result, err
	}
	return result, result.Fatal
}

// DegradedResumeText says a revive fell back to the working directory's most
// recent conversation because its own conversation id was never captured.
func DegradedResumeText(name string) string { return uisession.DegradedResumeText(name) }
