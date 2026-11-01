package sessioncmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

type Group struct {
	Path        string `json:"path" jsonschema:"full group path, slash separated; pass this value as a group argument"`
	Directory   string `json:"directory,omitempty" jsonschema:"group's default working directory, inherited by sessions created in it"`
	Worktree    string `json:"worktree,omitempty" jsonschema:"group's spawn-in-worktree choice: on, off, or empty to inherit"`
	Environment string `json:"environment,omitempty" jsonschema:"environment profile sessions created in this group launch under, or empty to inherit"`
	Archived    bool   `json:"archived" jsonschema:"whether the group is archived"`
	Sessions    int    `json:"sessions" jsonschema:"number of active agent sessions directly in this group"`
}

func (s *Sessions) Groups(sessionID string) ([]Group, error) {
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return nil, err
	}
	stored, err := runtime.store.Groups()
	if err != nil {
		return nil, err
	}
	sessions, err := runtime.store.ListSessions(true)
	if err != nil {
		return nil, err
	}
	return runtime.groupRows(stored, sessions), nil
}

func (r *runtime) groupRows(stored []store.Group, sessions []store.Session) []Group {
	counts := make(map[string]int, len(stored))
	for _, sess := range sessions {
		if r.cfg.Tools[sess.Tool].Shell || sess.Archived {
			continue
		}
		counts[sess.Group]++
	}
	groups := make([]Group, 0, len(stored))
	for _, group := range stored {
		groups = append(groups, Group{
			Path:        group.Name,
			Directory:   group.Path,
			Worktree:    group.Worktree,
			Environment: group.Environment,
			Archived:    group.Archived,
			Sessions:    counts[group.Name],
		})
	}
	return groups
}

// CreateGroup adds a group under an existing parent, so sessions spawned
// later can be filed into it. Directory becomes the group's inherited
// default working directory.
func (s *Sessions) CreateGroup(sessionID, path, directory string) (Group, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return Group{}, errors.New("group path is empty")
	}
	runtime, err := s.open()
	if err != nil {
		return Group{}, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return Group{}, err
	}
	existing, err := runtime.store.Groups()
	if err != nil {
		return Group{}, err
	}
	for _, group := range existing {
		if group.Name == path {
			return Group{}, fmt.Errorf("group %q already exists", path)
		}
	}
	if parent := parentGroup(path); parent != "" {
		known := false
		for _, group := range existing {
			if group.Name == parent {
				known = true
				break
			}
		}
		if !known {
			return Group{}, fmt.Errorf("parent group %q does not exist; create it first", parent)
		}
	}
	resolved := ""
	if strings.TrimSpace(directory) != "" {
		resolved, err = resolveTerminalDirectory(directory)
		if err != nil {
			return Group{}, err
		}
	}
	if err := runtime.store.CreateGroup(path, resolved); err != nil {
		return Group{}, err
	}
	return Group{Path: path, Directory: resolved}, nil
}

// GroupRemoval is what deleting a group did, since a fleet that filed work
// under one wants to know where its sessions went.
type GroupRemoval struct {
	Removed []string `json:"removed" jsonschema:"group paths that no longer exist, the named group and anything nested under it"`
	Moved   []string `json:"moved,omitempty" jsonschema:"ids of sessions that were filed under those groups and now sit in the root group"`
}

// DeleteGroup removes a group and the groups nested under it. Sessions
// filed there move to the root rather than going with it: an agent
// tidying up the group it opened for a finished fleet is not asking to
// destroy whatever is still running in it.
func (s *Sessions) DeleteGroup(sessionID, path string) (GroupRemoval, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return GroupRemoval{}, errors.New("group path is empty; the root group cannot be deleted")
	}
	runtime, err := s.open()
	if err != nil {
		return GroupRemoval{}, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return GroupRemoval{}, err
	}
	removed, moved, err := runtime.store.RemoveGroup(path)
	if errors.Is(err, store.ErrGroupNotFound) {
		return GroupRemoval{}, fmt.Errorf("group %q does not exist; call %s for current paths", path, runtime.words.ListGroups)
	}
	if err != nil {
		return GroupRemoval{}, err
	}
	if err := runtime.relabel(moved); err != nil {
		return GroupRemoval{}, fmt.Errorf("deleted %s, but could not update the status bar of the sessions moved to the root: %w", path, err)
	}
	return GroupRemoval{Removed: removed, Moved: moved}, nil
}

// Move files an agent session under another existing group, the way the
// manager's move key does; its nested terminals go with it. An empty group
// is the root.
func (s *Sessions) Move(sessionID, targetID, group string) (Session, error) {
	group = strings.Trim(strings.TrimSpace(group), "/")
	runtime, err := s.open()
	if err != nil {
		return Session{}, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return Session{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return Session{}, err
	}
	if group != "" {
		groups, err := runtime.store.Groups()
		if err != nil {
			return Session{}, err
		}
		known := false
		for _, candidate := range groups {
			known = known || candidate.Name == group
		}
		if !known {
			return Session{}, fmt.Errorf("group %q does not exist; call %s for the existing ones or %s to add it", group, runtime.words.ListGroups, runtime.words.CreateGroup)
		}
		if store.EffectivelyArchived(store.ArchivedGroups(groups), group) {
			return Session{}, fmt.Errorf("group %q is archived; restore it in Agent Manager first", group)
		}
	}
	// The move key leaves a row already filed there in its place.
	if target.Group == group && target.ParentID == "" {
		return runtime.sessionInfo(target, runtime.driver.Exists(target.ID), target.ID == sessionID), nil
	}
	if err := runtime.store.MoveSession(target.ID, group); err != nil {
		return Session{}, err
	}
	ids := []string{target.ID}
	children, err := runtime.store.Children(target.ID)
	if err != nil {
		return Session{}, fmt.Errorf("moved %s, but could not read its terminals to update their status bars: %w", target.ID, err)
	}
	for _, child := range children {
		ids = append(ids, child.ID)
	}
	if err := runtime.relabel(ids); err != nil {
		return Session{}, fmt.Errorf("moved %s, but could not update its status bar: %w", target.ID, err)
	}
	moved, err := runtime.store.Get(target.ID)
	if err != nil {
		return Session{}, fmt.Errorf("moved %s, but could not read it back: %w", target.ID, err)
	}
	return runtime.sessionInfo(moved, runtime.driver.Exists(moved.ID), moved.ID == sessionID), nil
}

// relabel skips a dead pane, which takes its label when it is revived.
func (r *runtime) relabel(ids []string) error {
	var failed error
	for _, id := range ids {
		if !r.driver.Exists(id) {
			continue
		}
		sess, err := r.store.Get(id)
		if err != nil {
			failed = errors.Join(failed, err)
			continue
		}
		failed = errors.Join(failed, r.driver.SetLabel(id, SessionLabel(sess.Group, sess.Name)))
	}
	return failed
}

func parentGroup(group string) string {
	if index := strings.LastIndex(group, "/"); index >= 0 {
		return group[:index]
	}
	return ""
}
