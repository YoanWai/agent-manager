package rail

import (
	"sort"
	"strings"
	"time"
)

// Session is the copied subset of a workspace session that Rail uses for
// tree policy and presentation. Root keeps the concrete store record.
type Session struct {
	ID             string
	Name           string
	DisplayName    string
	Tool           string
	Cwd            string
	Group          string
	Status         string
	Archived       bool
	Acked          bool
	CreatedAt      time.Time
	LastStatusAt   time.Time
	LaunchAt       time.Time
	AgentSessionID string
	ParentID       string
	Prompt         string
	PaneLine       string
	PanePrompt     string
	Queued         int
	IsShell        bool
	Elsewhere      bool
	// AfterTurn is the archive or kill the agent asked for once its turn
	// ends; AfterTurnGlyph is root's mark for it.
	AfterTurn      string
	AfterTurnGlyph string
}

// Snapshot is an immutable-by-convention inventory copied at the root
// observation boundary. Reconcile copies every slice and map it retains.
type Snapshot struct {
	Sessions       []Session
	Groups         []string
	ArchivedGroups map[string]bool
}

type RowKind uint8

const (
	SessionRow RowKind = iota
	GroupRow
)

// Selection names a row without exposing Rail's cursor or row storage.
type Selection struct {
	Kind      RowKind
	SessionID string
	Group     string
}

func (s Selection) key() string {
	if s.Kind == GroupRow {
		return "g:" + s.Group
	}
	return "s:" + s.SessionID
}

// Row is a copied diagnostic and presentation value. Mutating a returned
// Row never changes the model.
type Row struct {
	Selection
	Depth  int
	Name   string
	Tool   string
	Status string
}

type SelectionChange struct {
	Changed  bool
	Previous Selection
	Current  Selection
}

// SelectionEffect tells root which concrete selection side effects the Rail
// policy requires. Search editing deliberately reports identity changes
// without requesting either effect; the old list rebuilt silently while the
// field was active.
type SelectionEffect uint8

const (
	SelectionEffectNone SelectionEffect = iota
	SelectionEffectPreview
	SelectionEffectFilter
)

type Decision struct {
	Consumed         bool
	SelectionChanged bool
	SelectionEffect  SelectionEffect
	Selection        Selection
	Intent           Intent
	Mutations        []Mutation
	Refresh          bool
	Error            string
	AutoScroll       *AutoScrollRequest
}

type treeRow struct {
	kind  RowKind
	group string
	depth int
	sess  Session
}

func (r treeRow) selection() Selection {
	if r.kind == GroupRow {
		return Selection{Kind: GroupRow, Group: r.group}
	}
	return Selection{Kind: SessionRow, SessionID: r.sess.ID, Group: r.sess.Group}
}

func (r treeRow) key() string { return r.selection().key() }

func (r treeRow) isRoot() bool { return r.kind == GroupRow && r.group == "" }

type statusFilter uint8

const (
	statusFilterAll statusFilter = iota
	statusFilterAttention
)

type Model struct {
	snapshot        Snapshot
	rows            []treeRow
	cursor          int
	reorder         reorderState
	menu            rowMenu
	lifts           int
	listClickAt     time.Time
	listClickKey    string
	clickFocusKey   string
	showArchived    bool
	hideEmptyGroups bool
	filter          statusFilter
	collapsed       map[string]bool
	search          string
	searching       bool
}

func New(collapsed []string) Model {
	m := Model{collapsed: make(map[string]bool, len(collapsed))}
	for _, path := range collapsed {
		m.collapsed[path] = true
	}
	return m
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	copyOf := Snapshot{
		Sessions:       append([]Session(nil), snapshot.Sessions...),
		Groups:         append([]string(nil), snapshot.Groups...),
		ArchivedGroups: make(map[string]bool, len(snapshot.ArchivedGroups)),
	}
	for path, archived := range snapshot.ArchivedGroups {
		copyOf.ArchivedGroups[path] = archived
	}
	return copyOf
}

func (m *Model) Reconcile(snapshot Snapshot) SelectionChange {
	previous, previousOK := m.Selected()
	m.snapshot = cloneSnapshot(snapshot)
	m.rebuildRows()
	current, currentOK := m.Selected()
	return SelectionChange{
		Changed:  previousOK != currentOK || previous != current,
		Previous: previous,
		Current:  current,
	}
}

func (m Model) Selected() (Selection, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return Selection{}, false
	}
	return m.rows[m.cursor].selection(), true
}

func (m Model) SelectedSession() (string, bool) {
	selected, ok := m.Selected()
	return selected.SessionID, ok && selected.Kind == SessionRow
}

func (m Model) SelectedGroup() (string, bool) {
	selected, ok := m.Selected()
	return selected.Group, ok && selected.Kind == GroupRow
}

func (m Model) Rows() []Row {
	rows := make([]Row, len(m.rows))
	for i, row := range m.rows {
		rows[i] = Row{
			Selection: row.selection(),
			Depth:     row.depth,
			Name:      row.sess.Name,
			Tool:      row.sess.Tool,
			Status:    row.sess.Status,
		}
		if row.kind == GroupRow {
			rows[i].Name = baseName(row.group)
			if row.isRoot() {
				rows[i].Name = "root"
			}
		}
	}
	return rows
}

func (m *Model) Move(delta int, wrap bool) Decision {
	if len(m.rows) == 0 || delta == 0 {
		return Decision{Consumed: true}
	}
	previous := m.cursor
	next := m.cursor + delta
	if wrap {
		if next < 0 {
			next = len(m.rows) - 1
		}
		if next >= len(m.rows) {
			next = 0
		}
	} else {
		next = min(max(next, 0), len(m.rows)-1)
	}
	m.cursor = next
	decision := Decision{Consumed: true, SelectionChanged: previous != next}
	if decision.SelectionChanged {
		decision.SelectionEffect = SelectionEffectPreview
	}
	return decision
}

func (m *Model) FocusSession(id string) bool {
	for i, row := range m.rows {
		if row.kind == SessionRow && row.sess.ID == id {
			m.cursor = i
			return true
		}
	}
	return false
}

func (m *Model) Focus(selection Selection) bool {
	for i, row := range m.rows {
		if row.selection() == selection {
			m.cursor = i
			return true
		}
	}
	return false
}

func (m Model) row() (treeRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return treeRow{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) visibleSessions() []Session {
	visible := make([]Session, 0, len(m.snapshot.Sessions))
	for _, session := range m.snapshot.Sessions {
		if session.Archived == m.showArchived {
			visible = append(visible, session)
		}
	}
	return visible
}

func (m Model) listedSessions() []Session {
	visible := m.visibleSessions()
	if m.filter == statusFilterAll {
		return visible
	}
	held := ""
	if id, ok := m.SelectedSession(); ok {
		held = id
	}
	listed := make([]Session, 0, len(visible))
	for _, session := range visible {
		if attentionStatus(session.Status) || session.ID == held {
			listed = append(listed, session)
		}
	}
	return listed
}

func attentionStatus(state string) bool {
	return state == "waiting" || state == "finished" || state == "errored"
}

func (m *Model) rebuildRows() {
	previous := ""
	if row, ok := m.row(); ok {
		previous = row.key()
	}
	query := strings.ToLower(strings.TrimSpace(m.search))
	pruned := query != "" || m.filter != statusFilterAll
	listed := m.listedSessions()
	listedIDs := make(map[string]bool, len(listed))
	for _, session := range listed {
		listedIDs[session.ID] = true
	}
	byID := make(map[string]Session, len(m.snapshot.Sessions))
	for _, session := range m.snapshot.Sessions {
		byID[session.ID] = session
	}
	matched := make(map[string]bool, len(listed))
	for _, session := range listed {
		if query == "" || matchesSearch(session, query) {
			matched[session.ID] = true
		}
	}
	carried := map[string]bool{}
	for _, session := range listed {
		if matched[session.ID] && session.ParentID != "" && listedIDs[session.ParentID] {
			carried[session.ParentID] = true
		}
	}
	sessionsByGroup := map[string][]Session{}
	childrenByParent := map[string][]Session{}
	for _, session := range listed {
		if session.ParentID != "" {
			if _, ok := byID[session.ParentID]; ok {
				if matched[session.ID] {
					childrenByParent[session.ParentID] = append(childrenByParent[session.ParentID], session)
				}
				continue
			}
		}
		if matched[session.ID] || carried[session.ID] {
			sessionsByGroup[session.Group] = append(sessionsByGroup[session.Group], session)
		}
	}
	walked := map[string]bool{}
	for _, sessions := range sessionsByGroup {
		for _, session := range sessions {
			walked[session.ID] = true
		}
	}
	orphaned := map[string]bool{}
	for _, session := range listed {
		if _, nested := childrenByParent[session.ParentID]; !nested || walked[session.ParentID] || !matched[session.ID] {
			continue
		}
		sessionsByGroup[session.Group] = append(sessionsByGroup[session.Group], session)
		orphaned[session.ParentID] = true
	}
	for parentID := range orphaned {
		delete(childrenByParent, parentID)
	}

	paths := groupClosure(m.snapshot.Groups, m.snapshot.Sessions)
	if m.showArchived {
		kept := pathsWithSessions(paths, sessionsByGroup)
		for path := range paths {
			if effectivelyArchived(m.snapshot.ArchivedGroups, path) {
				addWithAncestors(kept, path)
			}
		}
		paths = kept
	} else {
		for path := range paths {
			if effectivelyArchived(m.snapshot.ArchivedGroups, path) {
				delete(paths, path)
			}
		}
		if pruned {
			paths = pathsWithSessions(paths, sessionsByGroup)
		}
	}
	if m.hideEmptyGroups && !m.showArchived {
		paths = pathsWithSessions(paths, sessionsByGroup)
	}
	children := childIndex(paths, m.snapshot.Groups)
	honorFolds := !pruned && !m.showArchived

	rows := make([]treeRow, 0, len(m.snapshot.Sessions)+len(paths)+1)
	appendSession := func(session Session, depth int) {
		rows = append(rows, treeRow{kind: SessionRow, sess: session, depth: depth})
		for _, child := range childrenByParent[session.ID] {
			rows = append(rows, treeRow{kind: SessionRow, sess: child, depth: depth + 1})
		}
	}
	rows = append(rows, treeRow{kind: GroupRow})
	for _, session := range sessionsByGroup[""] {
		appendSession(session, 0)
	}
	var walk func(string, int)
	walk = func(path string, depth int) {
		rows = append(rows, treeRow{kind: GroupRow, group: path, depth: depth})
		if honorFolds && m.collapsed[path] {
			return
		}
		for _, session := range sessionsByGroup[path] {
			appendSession(session, depth+1)
		}
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 0)
	}

	m.rows = rows
	if previous != "" {
		for i, row := range rows {
			if row.key() == previous {
				m.cursor = i
				break
			}
		}
	} else if m.cursor == 0 && len(rows) > 1 && rows[0].isRoot() {
		m.cursor = 1
	}
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func groupClosure(groups []string, sessions []Session) map[string]bool {
	paths := map[string]bool{}
	add := func(path string) {
		for path != "" {
			paths[path] = true
			path = parentGroup(path)
		}
	}
	for _, group := range groups {
		add(group)
	}
	for _, session := range sessions {
		add(session.Group)
	}
	return paths
}

func effectivelyArchived(archived map[string]bool, path string) bool {
	for path != "" {
		if archived[path] {
			return true
		}
		path = parentGroup(path)
	}
	return false
}

func addWithAncestors(set map[string]bool, path string) {
	for path != "" {
		set[path] = true
		path = parentGroup(path)
	}
}

func pathsWithSessions(paths map[string]bool, sessions map[string][]Session) map[string]bool {
	kept := map[string]bool{}
	for path := range paths {
		for group := range sessions {
			if inGroupSubtree(group, path) {
				kept[path] = true
				break
			}
		}
	}
	return kept
}

func childIndex(paths map[string]bool, ordered []string) map[string][]string {
	rank := make(map[string]int, len(ordered))
	for i, path := range ordered {
		rank[path] = i
	}
	children := map[string][]string{}
	for path := range paths {
		parent := parentGroup(path)
		children[parent] = append(children[parent], path)
	}
	for _, siblings := range children {
		sort.SliceStable(siblings, func(i, j int) bool {
			ri, iKnown := rank[siblings[i]]
			rj, jKnown := rank[siblings[j]]
			if iKnown && jKnown {
				return ri < rj
			}
			if iKnown != jKnown {
				return iKnown
			}
			return siblings[i] < siblings[j]
		})
	}
	return children
}

func parentGroup(group string) string {
	if index := strings.LastIndex(group, "/"); index >= 0 {
		return group[:index]
	}
	return ""
}

func baseName(path string) string {
	if index := strings.LastIndex(path, "/"); index >= 0 {
		return path[index+1:]
	}
	return path
}

func inGroupSubtree(group, parent string) bool {
	return group == parent || strings.HasPrefix(group, parent+"/")
}

func matchesSearch(session Session, query string) bool {
	return strings.Contains(strings.ToLower(session.Name), query) ||
		strings.Contains(strings.ToLower(session.Tool), query) ||
		strings.Contains(strings.ToLower(session.Group), query) ||
		strings.Contains(strings.ToLower(session.Status), query)
}
