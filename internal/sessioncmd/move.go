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

// Move files a session or terminal under an existing group, the way the
// move key does: an agent takes its nested terminals along, and a nested
// terminal moved on its own leaves its session. Only the filing changes,
// so the pane keeps running.
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
	target.Group = group
	children, err := runtime.store.Children(target.ID)
	if err != nil {
		return Placement{}, err
	}
	for _, child := range children {
		placement.Terminals = append(placement.Terminals, child.ID)
	}
	if err := runtime.relabel(append([]store.Session{target}, children...)); err != nil {
		return Placement{}, fmt.Errorf("moved %s, but could not update its status bar: %w", target.Name, err)
	}
	return placement, nil
}

// fileable refuses a group a row cannot be filed under: one that does not
// exist, or one archived out of the list the row would vanish into.
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

// relabel rewrites the status bar of each running row whose group changed;
// a dead pane takes its label when it is revived.
func (r *runtime) relabel(sessions []store.Session) error {
	var failed error
	for _, sess := range sessions {
		if !r.driver.Exists(sess.ID) {
			continue
		}
		failed = errors.Join(failed, r.driver.SetLabel(sess.ID, SessionLabel(sess.Group, sess.Name)))
	}
	return failed
}
