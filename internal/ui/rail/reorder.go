package rail

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) requestReorder(delta int) Decision {
	row, ok := m.row()
	if !ok {
		return Decision{Consumed: true}
	}
	if row.isRoot() {
		return Decision{Consumed: true, Error: "root stays at the top of the list"}
	}
	if row.host != "" {
		return Decision{Consumed: true, Error: RemoteRefusal("reorder")}
	}
	request, ok := m.swapRequest(row, delta)
	if !ok {
		edge, what := "top", "group"
		if delta > 0 {
			edge = "bottom"
		}
		if row.kind == SessionRow {
			what = "session"
		}
		return Decision{Consumed: true, Error: fmt.Sprintf("%s already at the %s of its level", what, edge)}
	}
	return Decision{Consumed: true, Mutations: []Mutation{request}}
}

func (m Model) swapRequest(row treeRow, delta int) (Mutation, bool) {
	target, ok := m.visibleReorderTarget(row, delta)
	if !ok {
		return Mutation{}, false
	}
	if row.kind == GroupRow {
		return Mutation{
			Kind:          SwapGroup,
			Path:          row.group,
			TargetPath:    target.group,
			GroupSiblings: m.knownGroupSiblings(parentGroup(row.group)),
			OffsetDelta:   delta,
			Refresh:       true,
		}, true
	}
	return Mutation{
		Kind:        SwapSession,
		SessionID:   row.sess.ID,
		TargetID:    target.sess.ID,
		OffsetDelta: delta,
		Refresh:     true,
	}, true
}

func (m Model) visibleReorderTarget(row treeRow, delta int) (treeRow, bool) {
	step := 1
	if delta < 0 {
		step = -1
	}
	for index := m.cursor + step; index >= 0 && index < len(m.rows); index += step {
		candidate := m.rows[index]
		if candidate.isRoot() || candidate.host != row.host {
			continue
		}
		if row.kind == GroupRow {
			if candidate.kind == GroupRow && parentGroup(candidate.group) == parentGroup(row.group) {
				return candidate, true
			}
			continue
		}
		if candidate.kind == SessionRow && candidate.sess.Group == row.sess.Group && candidate.sess.ParentID == row.sess.ParentID {
			return candidate, true
		}
	}
	return treeRow{}, false
}

func (m Model) knownGroupSiblings(parent string) []string {
	return append([]string(nil), childIndex(groupClosure(m.snapshot.Groups, m.snapshot.Sessions), m.snapshot.Groups)[parent]...)
}

func (m *Model) ApplyMutation(request Mutation, mutationErr error) Decision {
	if mutationErr != nil {
		return Decision{Consumed: true, Error: mutationErr.Error()}
	}
	switch request.Kind {
	case SaveCollapsed:
		return Decision{Consumed: true}
	case SwapSession:
		m.swapSessionLocal(request.SessionID, request.TargetID)
	case SwapGroup:
		m.materializeGroupsLocal(request.GroupSiblings)
		m.swapGroupLocal(request.Path, request.TargetPath)
	case PlaceSession, PlaceSessionBefore, MoveGroup:
		if request.Group != "" && m.collapsed[request.Group] {
			m.collapsed[request.Group] = false
			if request.FinishReorder {
				m.finishReorder()
			}
			m.rebuildRows()
			return Decision{Consumed: true, Refresh: request.Refresh, Selection: selectedOf(m), Mutations: []Mutation{{Kind: SaveCollapsed, Collapsed: m.Collapsed()}}}
		}
	}
	if request.OffsetDelta != 0 && m.reorder.active {
		m.reorder.offset += request.OffsetDelta
		m.reorder.moved = true
	}
	if request.FinishReorder {
		m.finishReorder()
	}
	m.rebuildRows()
	return Decision{Consumed: true, Refresh: request.Refresh, Selection: selectedOf(m)}
}

func (m *Model) swapSessionLocal(id, targetID string) {
	current, target := -1, -1
	for index, session := range m.snapshot.Sessions {
		switch session.ID {
		case id:
			current = index
		case targetID:
			target = index
		}
	}
	if current >= 0 && target >= 0 {
		m.snapshot.Sessions[current], m.snapshot.Sessions[target] = m.snapshot.Sessions[target], m.snapshot.Sessions[current]
	}
}

func (m *Model) materializeGroupsLocal(paths []string) {
	known := make(map[string]bool, len(m.snapshot.Groups))
	for _, group := range m.snapshot.Groups {
		known[group] = true
	}
	for _, path := range paths {
		if !known[path] {
			m.snapshot.Groups = append(m.snapshot.Groups, path)
			known[path] = true
		}
	}
}

func (m *Model) swapGroupLocal(path, targetPath string) {
	current, target := -1, -1
	for index, group := range m.snapshot.Groups {
		switch group {
		case path:
			current = index
		case targetPath:
			target = index
		}
	}
	if current >= 0 && target >= 0 {
		m.snapshot.Groups[current], m.snapshot.Groups[target] = m.snapshot.Groups[target], m.snapshot.Groups[current]
	}
}

func (m *Model) enterReorder(key string) {
	m.lifts++
	m.menu = rowMenu{}
	m.reorder = reorderState{active: true, lift: m.lifts, key: key, dragging: true}
}

func (m *Model) finishReorder() { m.reorder = reorderState{} }

func (m *Model) reorderKey(msg tea.KeyMsg, ctx KeyContext) Decision {
	key := keybind.Normalize(msg.String())
	action, _ := ctx.ListKeys.ActionFor(key)
	switch {
	case key == "ctrl+c" || action == keybind.Quit:
		m.finishReorder()
		return m.intent(Quit)
	case key == "enter" || key == "space":
		m.finishReorder()
		return Decision{Consumed: true}
	case key == "esc":
		requests := m.planUndo()
		if len(requests) == 0 {
			m.finishReorder()
		}
		return Decision{Consumed: true, Mutations: requests}
	case key == "up" || action == keybind.Up || action == keybind.ReorderUp:
		return m.stepLifted(-1)
	case key == "down" || action == keybind.Down || action == keybind.ReorderDown:
		return m.stepLifted(1)
	default:
		return Decision{Consumed: true}
	}
}

func (m *Model) stepLifted(delta int) Decision {
	m.reorder.drop = nil
	m.reorder.autoscroll.anchored = false
	row, ok := m.row()
	if !ok || row.key() != m.reorder.key {
		return Decision{Consumed: true}
	}
	request, ok := m.swapRequest(row, delta)
	if !ok {
		return Decision{Consumed: true}
	}
	return Decision{Consumed: true, Mutations: []Mutation{request}}
}

func (m Model) planUndo() []Mutation {
	scratch := m
	scratch.snapshot = cloneSnapshot(m.snapshot)
	scratch.rows = append([]treeRow(nil), m.rows...)
	var requests []Mutation
	for scratch.reorder.offset != 0 {
		delta := -1
		if scratch.reorder.offset < 0 {
			delta = 1
		}
		row, ok := scratch.row()
		if !ok {
			break
		}
		request, ok := scratch.swapRequest(row, delta)
		if !ok {
			break
		}
		requests = append(requests, request)
		scratch.applyPlannedSwap(request)
		scratch.reorder.offset += delta
	}
	if len(requests) > 0 {
		requests[len(requests)-1].FinishReorder = true
	}
	return requests
}

func (m *Model) applyPlannedSwap(request Mutation) {
	if request.Kind == SwapSession {
		m.swapSessionLocal(request.SessionID, request.TargetID)
	} else {
		m.materializeGroupsLocal(request.GroupSiblings)
		m.swapGroupLocal(request.Path, request.TargetPath)
	}
	m.rebuildRows()
}

func (m Model) planDrag(target Selection) []Mutation {
	scratch := m
	scratch.snapshot = cloneSnapshot(m.snapshot)
	scratch.rows = append([]treeRow(nil), m.rows...)
	targetIndex := scratch.rowIndex(target.key())
	var requests []Mutation
	for scratch.cursor != targetIndex && len(requests) < len(scratch.rows) {
		row, ok := scratch.row()
		if !ok {
			break
		}
		delta := 1
		if targetIndex < scratch.cursor {
			delta = -1
		}
		next, ok := scratch.visibleReorderTarget(row, delta)
		if !ok {
			break
		}
		nextIndex := scratch.rowIndex(next.key())
		if (delta > 0 && nextIndex > targetIndex) || (delta < 0 && nextIndex < targetIndex) {
			break
		}
		request, _ := scratch.swapRequest(row, delta)
		requests = append(requests, request)
		scratch.applyPlannedSwap(request)
	}
	return requests
}

func (m Model) rowIndex(key string) int {
	for index, row := range m.rows {
		if row.key() == key {
			return index
		}
	}
	return -1
}

func (m Model) resolveDrop(target Selection) (dropTarget, bool) {
	if m.showArchived || m.filter != statusFilterAll || m.search != "" {
		return dropTarget{}, false
	}
	liftedIndex, targetIndex := m.rowIndex(m.reorder.key), m.rowIndex(target.key())
	if liftedIndex < 0 || targetIndex < 0 || liftedIndex == targetIndex {
		return dropTarget{}, false
	}
	lifted, targetRow := m.rows[liftedIndex], m.rows[targetIndex]
	if lifted.host != "" || targetRow.host != "" {
		return dropTarget{}, false
	}
	if lifted.kind == GroupRow {
		return resolveGroupDrop(lifted.group, targetRow)
	}
	return m.resolveSessionDrop(lifted.sess, targetRow)
}

func (m Model) resolveSessionDrop(session Session, target treeRow) (dropTarget, bool) {
	if target.kind == GroupRow {
		if target.group == session.Group && session.ParentID == "" {
			return dropTarget{}, false
		}
		return dropTarget{kind: dropInto, key: target.key(), group: target.group, label: "into " + groupLabel(target.group)}, true
	}
	other := target.sess
	if other.ID == session.ParentID || other.ParentID == session.ID {
		return dropTarget{}, false
	}
	if session.IsShell && !other.IsShell && other.ParentID == "" {
		return dropTarget{kind: dropUnder, key: target.key(), group: other.Group, id: other.ID, label: "under " + sessionName(other)}, true
	}
	if !session.IsShell && other.ParentID != "" {
		parent, ok := m.session(other.ParentID)
		if !ok {
			return dropTarget{}, false
		}
		other = parent
	}
	if other.Group == session.Group && other.ParentID == session.ParentID {
		return dropTarget{}, false
	}
	return dropTarget{
		kind: dropBefore, key: Selection{Kind: SessionRow, SessionID: other.ID, Group: other.Group}.key(),
		group: other.Group, id: other.ID, label: "before " + sessionName(other) + " in " + groupLabel(other.Group),
	}, true
}

func resolveGroupDrop(path string, target treeRow) (dropTarget, bool) {
	destination := target.group
	if target.kind == SessionRow {
		destination = target.sess.Group
	} else if !target.isRoot() && parentGroup(target.group) == parentGroup(path) {
		return dropTarget{}, false
	}
	if destination == parentGroup(path) || inGroupSubtree(destination, path) {
		return dropTarget{}, false
	}
	key := Selection{Kind: GroupRow, Group: destination}.key()
	return dropTarget{kind: dropInto, key: key, group: destination, label: "into " + groupLabel(destination)}, true
}

func groupLabel(path string) string {
	if path == "" {
		return "root"
	}
	return baseName(path)
}

func sessionName(session Session) string {
	if session.DisplayName != "" {
		return session.DisplayName
	}
	return session.Name
}

func (m Model) session(id string) (Session, bool) {
	for _, session := range m.snapshot.Sessions {
		if session.ID == id {
			return session, true
		}
	}
	return Session{}, false
}

func (m *Model) dropMutation(drop dropTarget) (Mutation, bool) {
	index := m.rowIndex(m.reorder.key)
	if index < 0 {
		return Mutation{}, false
	}
	lifted := m.rows[index]
	switch {
	case lifted.kind == GroupRow:
		return Mutation{Kind: MoveGroup, Path: lifted.group, Group: drop.group, TargetPath: drop.group, Refresh: true, FinishReorder: true}, true
	case drop.kind == dropInto:
		return Mutation{Kind: PlaceSession, SessionID: lifted.sess.ID, Group: drop.group, Refresh: true, FinishReorder: true}, true
	case drop.kind == dropUnder:
		return Mutation{Kind: PlaceSession, SessionID: lifted.sess.ID, Group: drop.group, ParentID: drop.id, Refresh: true, FinishReorder: true}, true
	case drop.kind == dropBefore:
		return Mutation{Kind: PlaceSessionBefore, SessionID: lifted.sess.ID, TargetID: drop.id, Group: drop.group, Refresh: true, FinishReorder: true}, true
	default:
		return Mutation{}, false
	}
}
