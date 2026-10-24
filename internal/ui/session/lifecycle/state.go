// Package lifecycle is the runtime behind the session actions: archive,
// restore, delete, kill, restart, revive, after-turn endings and attach. It
// runs them through the session service off the event loop and keeps the
// small ledger a stale poll is reconciled against: which rows this run
// launched, removed, or is still waiting on a rename for. Its cards and its
// confirmation dialogs stay in the root.
package lifecycle

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

// State is what the session actions remember between passes, none of it
// persisted: a fresh manager run re-reads the store and tmux instead.
type State struct {
	// PickedRepos is the repo a human picked by hand per session, outranking
	// the agent's declaration for as long as this manager runs.
	PickedRepos map[string]string
	// AwaitedRenames holds what a spawned session launched with, for as long
	// as the agent it carries the rename directive to is still expected to
	// answer. A rename that has not landed by the time this manager run ends
	// is one that is never arriving.
	AwaitedRenames map[string]AwaitedRename
	// Launched is when this run recorded each session it spawned. A poll that
	// listed the store before that has nothing to say about the row.
	Launched map[string]time.Time
	// Gone is when this run took each session off the loaded list itself, by
	// deleting or archiving it, so a poll that listed the store before that
	// moment cannot put the row back on screen for a frame.
	Gone map[string]GoneMark
	// GoneGroups is the group-path counterpart of Gone: when this run
	// archived, restored, or deleted a group, a poll that listed the store
	// before that moment must not put the old state back on the tree.
	GoneGroups map[string]GoneMark
	// TerminalKeyAt is when the last T finished being handled. Held down it
	// autorepeats into a burst of keystrokes, and T is the only key that
	// spawns on the keystroke itself rather than opening a form that would
	// swallow them.
	TerminalKeyAt time.Time
}

// AwaitedRename is the prompt a spawned session carries and the name the
// manager generated for it before the agent picked one.
type AwaitedRename struct {
	Generated string
	Prompt    string
}

// GoneMark is the list state this run most recently gave a row, and when it
// gave it. Deleted means the row must not appear at all; otherwise Archived
// is the value the row's Archived flag should carry.
type GoneMark struct {
	At       time.Time
	Archived bool
	Deleted  bool
}

func (s *State) RecordLaunch(id string, at time.Time) {
	if s.Launched == nil {
		s.Launched = map[string]time.Time{}
	}
	s.Launched[id] = at
}

// ForgetLaunch drops a row from the pending set, so a session the user has
// since sent away is not carried back onto the tree by a poll that listed the
// store before it was launched.
func (s *State) ForgetLaunch(id string) { delete(s.Launched, id) }

func (s *State) AwaitRename(id, generated, prompt string) {
	if s.AwaitedRenames == nil {
		s.AwaitedRenames = map[string]AwaitedRename{}
	}
	s.AwaitedRenames[id] = AwaitedRename{Generated: generated, Prompt: prompt}
}

// ForgetSession drops everything this run held for a row it no longer has.
func (s *State) ForgetSession(id string) {
	delete(s.PickedRepos, id)
	delete(s.AwaitedRenames, id)
	s.ForgetLaunch(id)
}

// MarkSession records the loaded-list state this run just gave a session, so
// stale polls predating the change are reconciled on arrival instead of
// undoing it for a frame.
func (s *State) MarkSession(id string, mark GoneMark) {
	if s.Gone == nil {
		s.Gone = map[string]GoneMark{}
	}
	mark.At = time.Now()
	s.Gone[id] = mark
}

// MarkGroup records the archive state this run just gave a group path, so
// stale polls predating the change are reconciled on arrival instead of
// undoing it for a frame.
func (s *State) MarkGroup(path string, mark GoneMark) {
	if s.GoneGroups == nil {
		s.GoneGroups = map[string]GoneMark{}
	}
	mark.At = time.Now()
	s.GoneGroups[path] = mark
}

// KeepPendingLaunches carries over the rows this run spawned that the poll has
// not looked for yet. A poll lists its sessions and then spends the pass in
// tmux and ps calls, so the list the UI finally receives can predate a launch
// and would otherwise blink the new agent off screen. The first poll to list
// the store after a launch is the authority on it, whether it reports the row
// or its absence.
func (s *State) KeepPendingLaunches(polled, loaded []store.Session, listedAt time.Time) []store.Session {
	for id, at := range s.Launched {
		if listedAt.After(at) {
			delete(s.Launched, id)
			continue
		}
		if !sessionGone(polled, id) {
			continue
		}
		for _, sess := range loaded {
			if sess.ID == id {
				polled = append(polled, sess)
				break
			}
		}
	}
	return polled
}

// DropRecentlyRemoved reconciles the rows a poll lists against what this run
// has just done to them: without it, a pass in flight across a delete, an
// archive, or a restore delivers its pre-change listing afterwards and blinks
// the old state back for one more frame. A stale copy of a deleted row is
// dropped; a stale copy of an archive or restore has its flag corrected to
// what the store was just written to say. A listing that postdates every
// recorded change retires those records instead.
func (s *State) DropRecentlyRemoved(polled []store.Session, listedAt time.Time) []store.Session {
	if len(s.Gone) == 0 {
		return polled
	}
	kept := make([]store.Session, 0, len(polled))
	for _, sess := range polled {
		mark, known := s.Gone[sess.ID]
		switch {
		case !known || listedAt.After(mark.At):
			if known {
				delete(s.Gone, sess.ID)
			}
			kept = append(kept, sess)
		case mark.Deleted:
		default:
			sess.Archived = mark.Archived
			kept = append(kept, sess)
		}
	}
	for id, mark := range s.Gone {
		if listedAt.After(mark.At) {
			delete(s.Gone, id)
		}
	}
	return kept
}

// Listing is one poll's group rows, the metadata that hangs off them and the
// sessions it listed.
type Listing struct {
	Sessions       []store.Session
	ListedAt       time.Time
	Groups         []string
	GroupPaths     map[string]string
	GroupWorktrees map[string]string
	GroupBases     map[string]string
	ArchivedGroups map[string]bool
}

// ReconcileListing reconciles a stale listing's group rows, metadata, and
// archive flags against what this run has just done: a deleted group's header
// cannot hang back onto the tree, and an archive or restore of a group keeps
// its flag until a newer listing confirms it. A listing that postdates a
// change retires its marker instead.
func (s *State) ReconcileListing(listing Listing) Listing {
	listedAt := listing.ListedAt
	removed := make(map[string]bool, len(s.GoneGroups))
	for path, mark := range s.GoneGroups {
		switch {
		case listedAt.After(mark.At):
			delete(s.GoneGroups, path)
		case mark.Deleted:
			removed[path] = true
		default:
			if listing.ArchivedGroups == nil {
				listing.ArchivedGroups = map[string]bool{}
			}
			listing.ArchivedGroups[path] = mark.Archived
		}
	}
	if len(removed) == 0 {
		return listing
	}
	groups := make([]string, 0, len(listing.Groups))
	for _, group := range listing.Groups {
		if !removed[group] {
			groups = append(groups, group)
		}
	}
	listing.Groups = groups
	for path := range removed {
		delete(listing.GroupPaths, path)
		delete(listing.GroupWorktrees, path)
		delete(listing.GroupBases, path)
		delete(listing.ArchivedGroups, path)
	}
	return listing
}

// SessionGone reports whether a listing holds no row with this id.
func SessionGone(sessions []store.Session, id string) bool { return sessionGone(sessions, id) }

func sessionGone(sessions []store.Session, id string) bool {
	for _, sess := range sessions {
		if sess.ID == id {
			return false
		}
	}
	return true
}
