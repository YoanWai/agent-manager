package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

type sessionOrderKey struct {
	group    string
	parentID string
}

// orderMark carries only the rows this run swapped in one sibling set.
// Other managers can reorder independent rows without this snapshot hiding it.
type orderMark struct {
	at       time.Time
	affected map[string]bool
	order    []string
}

func (m *Model) markReorder(entry, target treeRow) {
	if entry.isGroup {
		if m.pendingGroupOrder == nil {
			m.pendingGroupOrder = make(map[string]orderMark)
		}
		parent := parentGroup(entry.group)
		mark := m.pendingGroupOrder[parent]
		if mark.affected == nil {
			mark.affected = make(map[string]bool)
		}
		mark.affected[entry.group] = true
		mark.affected[target.group] = true
		mark.order = mark.order[:0]
		for _, group := range m.groups {
			if mark.affected[group] && parentGroup(group) == parent {
				mark.order = append(mark.order, group)
			}
		}
		mark.at = time.Now()
		m.pendingGroupOrder[parent] = mark
		return
	}
	if m.pendingSessionOrder == nil {
		m.pendingSessionOrder = make(map[sessionOrderKey]orderMark)
	}
	key := sessionOrderKey{group: entry.sess.Group, parentID: entry.sess.ParentID}
	mark := m.pendingSessionOrder[key]
	if mark.affected == nil {
		mark.affected = make(map[string]bool)
	}
	mark.affected[entry.sess.ID] = true
	mark.affected[target.sess.ID] = true
	mark.order = mark.order[:0]
	for _, sess := range m.sessions {
		if mark.affected[sess.ID] && sess.Group == key.group && sess.ParentID == key.parentID {
			mark.order = append(mark.order, sess.ID)
		}
	}
	mark.at = time.Now()
	m.pendingSessionOrder[key] = mark
}

func (m *Model) reconcileReorder(sessions []store.Session, msg *refreshMsg) []store.Session {
	for key, mark := range m.pendingSessionOrder {
		if msg.listedAt.After(mark.at) {
			delete(m.pendingSessionOrder, key)
			continue
		}
		sessions = reorderKnown(sessions, mark.order, func(sess store.Session) string { return sess.ID }, func(sess store.Session) bool {
			return sess.Group == key.group && sess.ParentID == key.parentID
		})
	}
	for parent, mark := range m.pendingGroupOrder {
		if msg.listedAt.After(mark.at) {
			delete(m.pendingGroupOrder, parent)
			continue
		}
		msg.groups = reorderKnown(msg.groups, mark.order, func(group string) string { return group }, func(group string) bool {
			return parentGroup(group) == parent
		})
	}
	return sessions
}

// reorderKnown changes only the slots occupied by locally swapped rows in
// their original sibling set. Polled values and other rows remain intact.
func reorderKnown[T any](incoming []T, local []string, key func(T) string, matches func(T) bool) []T {
	byKey := make(map[string]T, len(local))
	known := make(map[string]bool, len(local))
	for _, id := range local {
		known[id] = true
	}
	var slots []int
	for i, row := range incoming {
		id := key(row)
		if known[id] && matches(row) {
			byKey[id] = row
			slots = append(slots, i)
		}
	}
	ordered := make([]T, 0, len(slots))
	for _, id := range local {
		if row, ok := byKey[id]; ok {
			ordered = append(ordered, row)
		}
	}
	for i, slot := range slots {
		incoming[slot] = ordered[i]
	}
	return incoming
}
