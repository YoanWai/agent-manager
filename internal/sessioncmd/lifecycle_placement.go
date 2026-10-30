package sessioncmd

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/store"
)

func (l *Lifecycle) ValidatePlacement(sess store.Session) error {
	if sess.Archived {
		return fmt.Errorf("session %q is archived; restore it in Agent Manager first", sess.Name)
	}
	groups, err := l.runtime.Store.Groups()
	if err != nil {
		return err
	}
	archived := store.ArchivedGroups(groups)
	if store.EffectivelyArchived(archived, sess.Group) {
		return fmt.Errorf("group %q is archived; restore it in Agent Manager first", sess.Group)
	}
	if sess.ParentID != "" {
		parent, err := l.runtime.Store.Get(sess.ParentID)
		if err != nil {
			return err
		}
		if parent.Archived || store.EffectivelyArchived(archived, parent.Group) {
			return fmt.Errorf("session %q is archived; restore it in Agent Manager first", parent.Name)
		}
	}
	return nil
}
