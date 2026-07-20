package ui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
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
	modeRepoPick
	modeGroupForm
	modeSettings
	modeDiff
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
	hooks  *hooks.Manager
	gitDrv *git.Driver

	// setSnapshot writes a session's pane capture; a seam so archival
	// snapshot failures can be exercised without a broken store.
	setSnapshot func(id, snapshot string) error

	sessions       []store.Session
	rows           []treeRow
	groups         []string
	groupPaths     map[string]string
	archivedGroups map[string]bool
	snap           sysstat.Snapshot
	proc           sysstat.ProcStat
	procFor        string
	preview        string
	agents         agentStats

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

	diff      diffState
	form      form
	groupForm groupForm
	pathSugg  pathComplete
	confirm   confirmTarget
	rename    renameTarget
	quick     quickState
	settings  settingsState
	moveID    string
	repoPick  repoPickState

	// Repo a human picked by hand per session, outranking the agent's
	// declaration for as long as this manager runs.
	pickedRepos map[string]string

	width  int
	height int
	// sessionsSized flips after the first refresh shrinks sessions left
	// over from a previous manager run to the preview panel's width.
	sessionsSized bool
	err           string
	errShown      string
	errAge        int
}

// confirmTarget.action values; the zero value means delete.
const (
	actionDelete  = ""
	actionArchive = "archive"
	actionRestore = "restore"
)

type confirmTarget struct {
	isGroup bool
	// archivedOnly marks a group delete issued from the archived view,
	// which clears the group's archive instead of the group itself.
	archivedOnly bool
	path         string
	label        string
	sessions     []store.Session
	action       string
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
	active      bool
	input       textarea.Model
	toolNames   []string
	toolIndex   int
	attachments []string
}

type settingsState struct {
	toolNames   []string
	toolIndex   int
	field       int
	layoutSplit bool
}

const (
	settingsFieldTool = iota
	settingsFieldLayout
	settingsFieldCount
)

// agentStats aggregates process-tree usage across all live sessions.
type agentStats struct {
	count int
	cpu   float64
	rss   uint64
}

type refreshMsg struct {
	sessions       []store.Session
	groups         []string
	groupPaths     map[string]string
	archivedGroups map[string]bool
	snap           sysstat.Snapshot
	snapOK         bool
	proc           sysstat.ProcStat
	procFor        string
	preview        string
	agents         agentStats
}

type previewMsg struct {
	sessID  string
	preview string
	proc    sysstat.ProcStat
}

type errMsg struct{ err error }

type attachDoneMsg struct {
	sessID string
	err    error
}

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager) *Model {
	statusSources := make(map[string]string, len(cfg.Tools))
	sessionStores := make(map[string]string, len(cfg.Tools))
	for name, tool := range cfg.Tools {
		statusSources[name] = tool.StatusSource
		sessionStores[name] = tool.SessionStore
	}
	// A missing git binary only disables the diff view; everything else
	// works without it, so the error surfaces on first use instead.
	gitDriver, _ := git.New()
	return &Model{
		cfg:         cfg,
		store:       st,
		tmux:        driver,
		hooks:       hookManager,
		gitDrv:      gitDriver,
		setSnapshot: st.SetSnapshot,
		poller:      newPoller(st, driver, engine, hookManager, statusSources, sessionStores, cfg.PollInterval.Duration),
		collapsed:   loadCollapsed(st),
		mode:        modeList,
	}
}

const collapsedSetting = "collapsed_groups"

// loadCollapsed restores the set of folded group paths persisted from a
// previous run so the tree opens in the same shape the user left it.
func loadCollapsed(st *store.Store) map[string]bool {
	collapsed := map[string]bool{}
	raw, err := st.Setting(collapsedSetting)
	if err != nil || raw == "" {
		return collapsed
	}
	var paths []string
	if err := json.Unmarshal([]byte(raw), &paths); err != nil {
		return collapsed
	}
	for _, path := range paths {
		collapsed[path] = true
	}
	return collapsed
}

// persistCollapsed saves the currently folded group paths so the state
// survives across launches.
func (m *Model) persistCollapsed() {
	paths := make([]string, 0, len(m.collapsed))
	for path, folded := range m.collapsed {
		if folded {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	raw, err := json.Marshal(paths)
	if err != nil {
		m.err = err.Error()
		return
	}
	if err := m.store.SetSetting(collapsedSetting, string(raw)); err != nil {
		m.err = err.Error()
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
	return m.refreshExistingSessionUX
}

// refreshExistingSessionUX re-applies the tmux bindings and status bar to
// sessions that were already running when the manager started, so a session
// created before an update still gets the current key bindings (the
// server-global Ctrl+R review key) and footer.
func (m *Model) refreshExistingSessionUX() tea.Msg {
	if err := m.tmux.EnsureBindings(); err != nil {
		return errMsg{err}
	}
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		return errMsg{err}
	}
	for _, sess := range sessions {
		if !m.tmux.Exists(sess.ID) {
			continue
		}
		// Best-effort per session: one that dies between the check and here
		// errors harmlessly and must not abort the rest, and the bindings that
		// matter are already installed above.
		_ = m.tmux.RefreshChrome(sess.ID)
		_ = m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name))
	}
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
func (m *Model) previewCmd(sess store.Session) tea.Cmd {
	return func() tea.Msg {
		msg := previewMsg{sessID: sess.ID}
		if sess.Archived {
			snapshot, err := archivedPreview(m.store, m.tmux, sess.ID)
			if err != nil {
				return errMsg{err}
			}
			msg.preview = snapshot
			return msg
		}
		if m.tmux.Exists(sess.ID) {
			if pane, err := m.tmux.CapturePane(sess.ID); err == nil {
				msg.preview = pane
			}
			if pid, err := m.tmux.PanePID(sess.ID); err == nil {
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

// resizeSessions syncs every live session's tmux window to the preview
// panel's width, so a detached session's capture fits the panel instead of
// getting clipped on the right. Dead sessions error harmlessly and skip.
func (m *Model) resizeSessions() {
	for _, sess := range m.sessions {
		if sess.Archived {
			continue
		}
		_ = m.tmux.Resize(sess.ID, m.previewPaneWidth(), m.height)
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Resuming from a tmux attach re-sends the current size unchanged; only
		// a real resize needs the per-session tmux resize calls, so an
		// unchanged size skips them and keeps detach latency flat.
		if msg.Width == m.width && msg.Height == m.height {
			return m, nil
		}
		m.width = msg.Width
		m.height = msg.Height
		m.resizeSessions()
		return m, nil

	case refreshMsg:
		m.ageError()
		m.sessions = msg.sessions
		m.groups = msg.groups
		m.groupPaths = msg.groupPaths
		m.archivedGroups = msg.archivedGroups
		m.agents = msg.agents
		if msg.snapOK {
			m.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		if !m.sessionsSized && m.width > 0 && len(m.sessions) > 0 {
			m.sessionsSized = true
			m.resizeSessions()
		}
		m.rebuildRows()
		// A pass that ran with a stale selection (a session created this
		// tick) carries the wrong preview; resync and fetch it directly.
		if sess, ok := m.selected(); ok && sess.ID != msg.procFor {
			m.syncPollInput()
			return m, tea.Batch(m.previewCmd(sess), m.diffRefreshCmd())
		}
		m.proc = msg.proc
		m.procFor = msg.procFor
		m.preview = msg.preview
		return m, m.diffRefreshCmd()

	case previewMsg:
		if sess, ok := m.selected(); ok && sess.ID == msg.sessID {
			m.preview = msg.preview
			m.proc = msg.proc
			m.procFor = msg.sessID
		}
		return m, nil

	case diffLoadedMsg:
		return m, m.handleDiffLoaded(msg)

	case diffHLMsg:
		m.handleDiffHL(msg)
		return m, nil

	case diffProbeMsg:
		return m, m.handleDiffProbe(msg)

	case errMsg:
		m.err = msg.err.Error()
		return m, nil

	case attachDoneMsg:
		// The attach client sized the window to the full terminal and tmux
		// keeps that size on detach; shrink it back to the preview panel so
		// the capture is not clipped on the right.
		_ = m.tmux.Resize(msg.sessID, m.previewPaneWidth(), m.height)
		if msg.err != nil {
			m.err = msg.err.Error()
			m.requestRefresh()
			return m, nil
		}
		// Ctrl+R inside the session sets a marker before detaching; consume
		// it here and jump straight to review for the session just attached.
		requested, err := m.tmux.ReviewRequested()
		if err != nil {
			m.err = err.Error()
		} else if requested {
			// A failed clear leaves the marker set, which would reopen review
			// on every later detach, so surface it and stay in the list rather
			// than letting openDiff reset m.err and hide it.
			if clearErr := m.tmux.ClearReviewRequest(); clearErr != nil {
				m.err = clearErr.Error()
				m.requestRefresh()
				return m, nil
			}
			sess, ok := m.selected()
			cmd := m.openDiff()
			if ok && m.mode == modeDiff {
				m.diff.reattachID = sess.ID
			}
			return m, cmd
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
	if m.showArchived {
		// The archived view keeps groups that hold archived sessions plus any
		// group whose subtree was archived as a whole (even with no sessions),
		// instead of the full tree skeleton.
		kept := pathsWithSessions(paths, sessionsByGroup)
		for path := range paths {
			if m.groupEffectivelyArchived(path) {
				addWithAncestors(kept, path)
			}
		}
		paths = kept
	} else {
		// The active view hides any archived group and its whole subtree.
		for path := range paths {
			if m.groupEffectivelyArchived(path) {
				delete(paths, path)
			}
		}
		if query != "" {
			paths = pathsWithSessions(paths, sessionsByGroup)
		}
	}
	children := childIndex(paths, m.groups)

	// Folds are a browsing convenience for the active tree; the archived
	// and search views already prune to matching groups, so honoring folds
	// there would hide the very sessions the user came to act on.
	honorFolds := query == "" && !m.showArchived

	rows := make([]treeRow, 0, len(m.sessions)+len(paths))
	for _, sess := range sessionsByGroup[""] {
		rows = append(rows, treeRow{sess: sess})
	}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		rows = append(rows, treeRow{isGroup: true, group: path, depth: depth})
		if honorFolds && m.collapsed[path] {
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

func (m *Model) groupEffectivelyArchived(path string) bool {
	return store.EffectivelyArchived(m.archivedGroups, path)
}

func addWithAncestors(set map[string]bool, path string) {
	for path != "" {
		set[path] = true
		idx := strings.LastIndex(path, "/")
		if idx < 0 {
			break
		}
		path = path[:idx]
	}
}

func pathsWithSessions(paths map[string]bool, sessionsByGroup map[string][]store.Session) map[string]bool {
	kept := map[string]bool{}
	for path := range paths {
		for group := range sessionsByGroup {
			if inGroupSubtree(group, path) {
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
		parent := parentGroup(path)
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
