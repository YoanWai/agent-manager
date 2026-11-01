package sessioncmd

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
)

// Placement is where a move filed a row, and what went with it.
type Placement struct {
	ID           string   `json:"id" jsonschema:"id of the moved session or terminal"`
	Name         string   `json:"name" jsonschema:"its name in Agent Manager"`
	Kind         string   `json:"kind" jsonschema:"session for an agent, terminal for a managed terminal"`
	From         string   `json:"from" jsonschema:"group path it was in; empty is the root"`
	Group        string   `json:"group" jsonschema:"group path it is in now; empty is the root"`
	UnnestedFrom string   `json:"unnested_from,omitempty" jsonschema:"id of the session a nested terminal hung under before the move"`
	Terminals    []string `json:"terminals,omitempty" jsonschema:"ids of the terminals nested under it that moved with it"`
	Unchanged    bool     `json:"unchanged,omitempty" jsonschema:"true when it already sat un-nested in that group, so nothing changed"`
}

// Move files a row under an existing group as the move key does; only the filing changes.
func (s *Sessions) Move(sessionID, targetID, group string) (Placement, error) {
	group = strings.Trim(strings.TrimSpace(group), "/")
	runtime, err := s.open()
	if err != nil {
		return Placement{}, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return Placement{}, err
	}
	target, err := runtime.row(targetID)
	if err != nil {
		return Placement{}, err
	}
	if err := runtime.fileable(group); err != nil {
		return Placement{}, err
	}
	placement := Placement{ID: target.ID, Name: target.Name, Kind: "session", From: target.Group, Group: group, UnnestedFrom: target.ParentID}
	if runtime.cfg.Tools[target.Tool].Shell {
		placement.Kind = "terminal"
	}
	if target.Group == group && target.ParentID == "" {
		placement.Unchanged = true
		return placement, nil
	}
	err = runtime.store.FileSession(target.ID, group)
	if errors.Is(err, store.ErrGroupNotFound) {
		return Placement{}, runtime.missingGroup(group)
	}
	if err != nil {
		return Placement{}, err
	}
	children, err := runtime.store.Children(target.ID)
	if err != nil {
		return Placement{}, fmt.Errorf("moved %s, but could not list its terminals: %w", target.Name, err)
	}
	for _, child := range children {
		placement.Terminals = append(placement.Terminals, child.ID)
	}
	if err := runtime.relabel(append([]string{target.ID}, placement.Terminals...)); err != nil {
		return Placement{}, fmt.Errorf("moved %s, but could not update its status bar: %w", target.Name, err)
	}
	return placement, nil
}

// fileable refuses a missing group, and an archived one the row would vanish into.
func (r *runtime) fileable(group string) error {
	if group == "" {
		return nil
	}
	groups, err := r.store.Groups()
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(groups, func(candidate store.Group) bool { return candidate.Name == group }) {
		return r.missingGroup(group)
	}
	if store.EffectivelyArchived(store.ArchivedGroups(groups), group) {
		return archivedGroup(group)
	}
	return nil
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
