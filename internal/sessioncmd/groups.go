package sessioncmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

type Group struct {
	Path      string `json:"path" jsonschema:"full group path, slash separated; pass this value as a group argument"`
	Directory string `json:"directory,omitempty" jsonschema:"group's default working directory, inherited by sessions created in it"`
	Worktree  string `json:"worktree,omitempty" jsonschema:"group's spawn-in-worktree choice: on, off, or empty to inherit"`
	Archived  bool   `json:"archived" jsonschema:"whether the group is archived"`
	Sessions  int    `json:"sessions" jsonschema:"number of active agent sessions directly in this group"`
}

func (s *Sessions) Groups(sessionID string) ([]Group, error) {
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
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
	counts := make(map[string]int, len(stored))
	for _, sess := range sessions {
		if runtime.cfg.Tools[sess.Tool].Shell || sess.Archived {
			continue
		}
		counts[sess.Group]++
	}
	groups := make([]Group, 0, len(stored))
	for _, group := range stored {
		groups = append(groups, Group{
			Path:      group.Name,
			Directory: group.Path,
			Worktree:  group.Worktree,
			Archived:  group.Archived,
			Sessions:  counts[group.Name],
		})
	}
	return groups, nil
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
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
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
	defer runtime.store.Close()
	if _, err := runtime.caller(sessionID); err != nil {
		return GroupRemoval{}, err
	}
	removed, moved, err := runtime.store.RemoveGroup(path)
	if errors.Is(err, store.ErrGroupNotFound) {
		return GroupRemoval{}, fmt.Errorf("group %q does not exist; call %s for current paths", path, runtime.words.ListGroups)
	}
	if err != nil {
		return GroupRemoval{}, err
	}
	return GroupRemoval{Removed: removed, Moved: moved}, nil
}
