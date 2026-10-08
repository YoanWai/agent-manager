package rail

import (
	"maps"
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
	// Hosts are the SSH connections, listed after the local tree in this
	// order.
	Hosts []Host
}

type RowKind uint8

const (
	SessionRow RowKind = iota
	GroupRow
	ConnectionRow
)

// Selection names a row without exposing Rail's cursor or row storage.
// Host names the connection a remote row belongs to and is empty on every
// local row.
type Selection struct {
	Kind      RowKind
	SessionID string
	Group     string
	Host      string
}

// Remote reports whether the row lives on an SSH connection, the
// connection's own row included.
func (s Selection) Remote() bool { return s.Host != "" }

func (s Selection) key() string {
	switch s.Kind {
	case ConnectionRow:
		return "c:" + s.Host
	case GroupRow:
		return "g:" + foldKey(s.Host, s.Group)
	}
	if s.Host != "" {
		return "s:" + s.Host + hostSeparator + s.SessionID
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
	host  string
}

func (r treeRow) selection() Selection {
	switch r.kind {
	case ConnectionRow:
		return Selection{Kind: ConnectionRow, Host: r.host}
	case GroupRow:
		return Selection{Kind: GroupRow, Group: r.group, Host: r.host}
	}
	return Selection{Kind: SessionRow, SessionID: r.sess.ID, Group: r.sess.Group, Host: r.host}
}

func (r treeRow) key() string { return r.selection().key() }

func (r treeRow) isRoot() bool { return r.kind == GroupRow && r.group == "" && r.host == "" }

// folds reports whether the row opens and closes: a group or a connection.
func (r treeRow) folds() bool { return r.kind == GroupRow || r.kind == ConnectionRow }

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
		Hosts:          make([]Host, len(snapshot.Hosts)),
	}
	for path, archived := range snapshot.ArchivedGroups {
		copyOf.ArchivedGroups[path] = archived
	}
	for i, host := range snapshot.Hosts {
		host.Sessions = append([]Session(nil), host.Sessions...)
		host.Groups = append([]string(nil), host.Groups...)
		host.ArchivedGroups = maps.Clone(host.ArchivedGroups)
		copyOf.Hosts[i] = host
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

// SelectedSession is the selected local session; a remote row is none.
func (m Model) SelectedSession() (string, bool) {
	selected, ok := m.Selected()
	return selected.SessionID, ok && selected.Kind == SessionRow && !selected.Remote()
}

// SelectedGroup is the selected local group; a remote row is none.
func (m Model) SelectedGroup() (string, bool) {
	selected, ok := m.Selected()
	return selected.Group, ok && selected.Kind == GroupRow && !selected.Remote()
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
		switch {
		case row.kind == ConnectionRow:
			rows[i].Name = row.host
		case row.isRoot():
			rows[i].Name = "root"
		case row.kind == GroupRow:
			rows[i].Name = baseName(row.group)
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
		if row.kind == SessionRow && row.host == "" && row.sess.ID == id {
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

func (m Model) listedSessions() []Session {
	return m.listed("", m.snapshot.Sessions)
}

// listed is what the view and the status filter keep of one host's
// sessions; the selected session stays while the filter is on.
func (m Model) listed(host string, sessions []Session) []Session {
	visible := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if session.Archived == m.showArchived {
			visible = append(visible, session)
		}
	}
	if m.filter == statusFilterAll {
		return visible
	}
	held, _ := m.Selected()
	listed := make([]Session, 0, len(visible))
	for _, session := range visible {
		if attentionStatus(session.Status) || held.Kind == SessionRow && held.Host == host && held.SessionID == session.ID {
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
	rows := []treeRow{{kind: GroupRow}}
	rows = append(rows, m.treeRows("", m.snapshot.Sessions, m.snapshot.Groups, m.snapshot.ArchivedGroups, 0)...)
	for _, host := range m.snapshot.Hosts {
		rows = append(rows, treeRow{kind: ConnectionRow, host: host.Name})
		if m.honorFolds() && m.collapsed[foldKey(host.Name, "")] {
			continue
		}
		rows = append(rows, m.treeRows(host.Name, host.Sessions, host.Groups, host.ArchivedGroups, 1)...)
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

func (m Model) pruned() bool {
	return strings.TrimSpace(m.search) != "" || m.filter != statusFilterAll
}

func (m Model) honorFolds() bool { return !m.pruned() && !m.showArchived }

// treeRows lays out one host's groups and sessions from depth on: the
// local host's under the root row, a connection's under its own row.
func (m Model) treeRows(host string, all []Session, groups []string, archivedGroups map[string]bool, depth int) []treeRow {
	query := strings.ToLower(strings.TrimSpace(m.search))
	listed := m.listed(host, all)
	listedIDs := make(map[string]bool, len(listed))
	for _, session := range listed {
		listedIDs[session.ID] = true
	}
	byID := make(map[string]Session, len(all))
	for _, session := range all {
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

	paths := groupClosure(groups, all)
	if m.showArchived {
		kept := pathsWithSessions(paths, sessionsByGroup)
		for path := range paths {
			if effectivelyArchived(archivedGroups, path) {
				addWithAncestors(kept, path)
			}
		}
		paths = kept
	} else {
		for path := range paths {
			if effectivelyArchived(archivedGroups, path) {
				delete(paths, path)
			}
		}
		if m.pruned() {
			paths = pathsWithSessions(paths, sessionsByGroup)
		}
	}
	if m.hideEmptyGroups && !m.showArchived {
		paths = pathsWithSessions(paths, sessionsByGroup)
	}
	children := childIndex(paths, groups)
	honorFolds := m.honorFolds()

	rows := make([]treeRow, 0, len(all)+len(paths))
	appendSession := func(session Session, depth int) {
		rows = append(rows, treeRow{kind: SessionRow, sess: session, depth: depth, host: host})
		for _, child := range childrenByParent[session.ID] {
			rows = append(rows, treeRow{kind: SessionRow, sess: child, depth: depth + 1, host: host})
		}
	}
	for _, session := range sessionsByGroup[""] {
		appendSession(session, depth)
	}
	var walk func(string, int)
	walk = func(path string, depth int) {
		rows = append(rows, treeRow{kind: GroupRow, group: path, depth: depth, host: host})
		if honorFolds && m.collapsed[foldKey(host, path)] {
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
		walk(root, depth)
	}
	return rows
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
