package ui

import (
	"crypto/rand"
	"encoding/hex"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type mode int

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeHelp
	modeRename
	modeMove
	modeGroupForm
	modeSettings
)

type treeRow struct {
	isGroup bool
	group   string
	depth   int
	sess    store.Session
}

type Model struct {
	cfg    config.Config
	store  *store.Store
	tmux   *tmux.Driver
	engine *status.Engine
	hooks  *hooks.Manager

	sessions   []store.Session
	rows       []treeRow
	groups     []string
	groupPaths map[string]string
	snap       sysstat.Snapshot
	proc       sysstat.ProcStat
	procFor    string
	preview    string
	agents     agentStats

	netUp       uint64
	netDown     uint64
	netRates    bool
	prevNetSent uint64
	prevNetRecv uint64
	prevNetAt   time.Time
	prevNetOK   bool

	poller       *poller
	cursor       int
	mode         mode
	showArchived bool
	collapsed    map[string]bool
	search       string
	searching    bool

	form      form
	groupForm groupForm
	pathSugg  pathComplete
	confirm   confirmTarget
	rename    renameTarget
	quick     quickState
	settings  settingsState
	moveID    string

	width    int
	height   int
	err      string
	errShown string
	errAge   int
}

type confirmTarget struct {
	isGroup  bool
	path     string
	label    string
	sessions []store.Session
}

type renameTarget struct {
	isGroup bool
	path    string
	sessID  string
	input   textinput.Model
	dir     textinput.Model
	focus   int
}

// quickState is the inline prompt bar docked under the preview: active
// across cursor moves, so the target follows the selection. The tool is
// the spawn CLI for group targets, cycled with tab.
type quickState struct {
	active    bool
	input     textarea.Model
	toolNames []string
	toolIndex int
}

type settingsState struct {
	toolNames []string
	toolIndex int
}

// agentStats aggregates process-tree usage across all live sessions.
type agentStats struct {
	count int
	cpu   float64
	rss   uint64
}

type refreshMsg struct {
	sessions   []store.Session
	groups     []string
	groupPaths map[string]string
	snap       sysstat.Snapshot
	snapOK     bool
	proc       sysstat.ProcStat
	procFor    string
	preview    string
	agents     agentStats
}

type previewMsg struct {
	sessID  string
	preview string
	proc    sysstat.ProcStat
}

type errMsg struct{ err error }

type attachDoneMsg struct{ err error }

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager) *Model {
	statusSources := make(map[string]string, len(cfg.Tools))
	for name, tool := range cfg.Tools {
		statusSources[name] = tool.StatusSource
	}
	return &Model{
		cfg:       cfg,
		store:     st,
		tmux:      driver,
		engine:    engine,
		hooks:     hookManager,
		poller:    newPoller(st, driver, engine, hookManager, statusSources, cfg.PollInterval.Duration),
		collapsed: map[string]bool{},
		mode:      modeList,
	}
}

// StartPoller launches the background polling loop. It runs outside the
// bubbletea event loop so statuses keep updating while the TUI is
// suspended inside a tmux attach.
func (m *Model) StartPoller(send func(tea.Msg)) {
	m.syncPollInput()
	go m.poller.run(send)
}

func (m *Model) syncPollInput() {
	selectedID := ""
	if sess, ok := m.selected(); ok {
		selectedID = sess.ID
	}
	m.poller.setInput(m.showArchived, selectedID)
}

// requestRefresh publishes the current UI state to the poller and asks
// for an immediate pass.
func (m *Model) requestRefresh() {
	m.syncPollInput()
	m.poller.requestRefresh()
}

func (m *Model) Init() tea.Cmd {
	m.syncPollInput()
	return nil
}

// visibleSessions filters to the sessions the current view scope shows:
// active ones normally, archived ones in the archived view. It also
// covers the frames between a scope toggle and the next refresh, when
// m.sessions still carries the other scope's list.
func (m *Model) visibleSessions() []store.Session {
	visible := make([]store.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		if sess.Archived == m.showArchived {
			visible = append(visible, sess)
		}
	}
	return visible
}

func (m *Model) selected() (store.Session, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].isGroup {
		return store.Session{}, false
	}
	return m.rows[m.cursor].sess, true
}

func (m *Model) selectedRow() (treeRow, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return treeRow{}, false
	}
	return m.rows[m.cursor], true
}

// previewCmd captures one session's pane and process stats right away,
// off the render loop, for instant sidebar updates on cursor moves.
func (m *Model) previewCmd(sessID string) tea.Cmd {
	return func() tea.Msg {
		msg := previewMsg{sessID: sessID}
		if m.tmux.Exists(sessID) {
			if pane, err := m.tmux.CapturePane(sessID); err == nil {
				msg.preview = pane
			}
			if pid, err := m.tmux.PanePID(sessID); err == nil {
				msg.proc = sysstat.Trees([]int{pid})[pid]
			}
		}
		return msg
	}
}

// refreshCmd runs one synchronous polling pass; the background poller
// covers normal operation, this exists for tests and explicit refreshes.
func (m *Model) refreshCmd() tea.Cmd {
	m.syncPollInput()
	return func() tea.Msg {
		return m.poller.refreshOnce()
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case refreshMsg:
		m.ageError()
		m.sessions = msg.sessions
		m.groups = msg.groups
		m.groupPaths = msg.groupPaths
		m.agents = msg.agents
		if msg.snapOK {
			m.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		m.rebuildRows()
		// A pass that ran with a stale selection (a session created this
		// tick) carries the wrong preview; resync and fetch it directly.
		if sess, ok := m.selected(); ok && sess.ID != msg.procFor {
			m.syncPollInput()
			return m, m.previewCmd(sess.ID)
		}
		m.proc = msg.proc
		m.procFor = msg.procFor
		m.preview = msg.preview
		return m, nil

	case previewMsg:
		if sess, ok := m.selected(); ok && sess.ID == msg.sessID {
			m.preview = msg.preview
			m.proc = msg.proc
			m.procFor = msg.sessID
		}
		return m, nil

	case errMsg:
		m.err = msg.err.Error()
		return m, nil

	case attachDoneMsg:
		if msg.err != nil {
			m.err = msg.err.Error()
		}
		m.requestRefresh()
		return m, nil

	case tea.KeyMsg:
		model, cmd := m.handleKey(msg)
		m.syncPollInput()
		return model, cmd
	}
	return m, nil
}

// updateNetRates diffs cumulative interface counters between polls into
// bytes-per-second rates. Counters can reset (sleep, interface changes),
// so a backwards jump just reseeds the baseline.
func (m *Model) updateNetRates(snap sysstat.Snapshot) {
	now := time.Now()
	if m.prevNetOK && snap.NetOK &&
		snap.NetSent >= m.prevNetSent && snap.NetRecv >= m.prevNetRecv {
		if dt := now.Sub(m.prevNetAt).Seconds(); dt > 0 {
			m.netUp = uint64(float64(snap.NetSent-m.prevNetSent) / dt)
			m.netDown = uint64(float64(snap.NetRecv-m.prevNetRecv) / dt)
			m.netRates = true
		}
	}
	m.prevNetSent = snap.NetSent
	m.prevNetRecv = snap.NetRecv
	m.prevNetAt = now
	m.prevNetOK = snap.NetOK
}

// ageError clears a status message after it has survived a couple of poll
// ticks, so transient errors self-dismiss without any per-callsite timers.
func (m *Model) ageError() {
	if m.err == "" {
		m.errShown, m.errAge = "", 0
		return
	}
	if m.err != m.errShown {
		m.errShown, m.errAge = m.err, 0
		return
	}
	m.errAge++
	if m.errAge >= 2 {
		m.err, m.errShown, m.errAge = "", "", 0
	}
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func rowKey(entry treeRow) string {
	if entry.isGroup {
		return "g:" + entry.group
	}
	return "s:" + entry.sess.ID
}

// rebuildRows walks the group tree depth-first and emits one row per
// group node and per session, honoring collapse state and search.
// The cursor follows the previously selected row's identity, so list
// changes from the 2s poll never yank the selection around.
func (m *Model) rebuildRows() {
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	query := strings.ToLower(strings.TrimSpace(m.search))

	// m.sessions arrives ordered by the store (group, sort_order), so
	// per-group slices inherit the user's manual order.
	sessionsByGroup := map[string][]store.Session{}
	for _, sess := range m.visibleSessions() {
		if query != "" && !matchesSearch(sess, query) {
			continue
		}
		sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
	}

	paths := groupClosure(m.groups, m.sessions)
	// The archived view keeps only groups that hold archived sessions,
	// instead of the full (mostly empty) tree skeleton.
	if query != "" || m.showArchived {
		paths = pathsWithSessions(paths, sessionsByGroup)
	}
	children := childIndex(paths, m.groups)

	rows := make([]treeRow, 0, len(m.sessions)+len(paths))
	for _, sess := range sessionsByGroup[""] {
		rows = append(rows, treeRow{sess: sess})
	}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		rows = append(rows, treeRow{isGroup: true, group: path, depth: depth})
		if m.collapsed[path] {
			return
		}
		for _, sess := range sessionsByGroup[path] {
			rows = append(rows, treeRow{sess: sess, depth: depth + 1})
		}
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 0)
	}

	m.rows = rows
	if previousKey != "" {
		for i, entry := range rows {
			if rowKey(entry) == previousKey {
				m.cursor = i
				break
			}
		}
	}
	if m.cursor >= len(rows) {
		m.cursor = len(rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// groupClosure unions stored groups with groups referenced by sessions,
// then adds every ancestor so partial paths always render.
func groupClosure(groups []string, sessions []store.Session) map[string]bool {
	paths := map[string]bool{}
	add := func(path string) {
		for path != "" {
			paths[path] = true
			idx := strings.LastIndex(path, "/")
			if idx < 0 {
				break
			}
			path = path[:idx]
		}
	}
	for _, g := range groups {
		add(g)
	}
	for _, sess := range sessions {
		add(sess.Group)
	}
	return paths
}

func pathsWithSessions(paths map[string]bool, sessionsByGroup map[string][]store.Session) map[string]bool {
	kept := map[string]bool{}
	for path := range paths {
		for group := range sessionsByGroup {
			if group == path || strings.HasPrefix(group, path+"/") {
				kept[path] = true
				break
			}
		}
	}
	return kept
}

// childIndex maps each group to its ordered children: stored groups in
// the user's manual order, synthesized ancestors alphabetically after.
func childIndex(paths map[string]bool, ordered []string) map[string][]string {
	rank := make(map[string]int, len(ordered))
	for i, name := range ordered {
		rank[name] = i
	}
	children := map[string][]string{}
	for path := range paths {
		parent := ""
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			parent = path[:idx]
		}
		children[parent] = append(children[parent], path)
	}
	for _, siblings := range children {
		sort.SliceStable(siblings, func(i, j int) bool {
			ri, oki := rank[siblings[i]]
			rj, okj := rank[siblings[j]]
			if oki && okj {
				return ri < rj
			}
			if oki != okj {
				return oki
			}
			return siblings[i] < siblings[j]
		})
	}
	return children
}

func baseName(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

func matchesSearch(sess store.Session, query string) bool {
	return strings.Contains(strings.ToLower(sess.Name), query) ||
		strings.Contains(strings.ToLower(sess.Tool), query) ||
		strings.Contains(strings.ToLower(sess.Group), query) ||
		strings.Contains(strings.ToLower(sess.Status), query)
}

func newID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405")))
	}
	return hex.EncodeToString(buf)
}
