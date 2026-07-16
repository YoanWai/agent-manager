package ui

import (
	"crypto/rand"
	"encoding/hex"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
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

	pollTick     int
	paneHashes   map[string]uint64
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
}

// agentStats aggregates process-tree usage across all live sessions.
type agentStats struct {
	count int
	cpu   float64
	rss   uint64
}

type tickMsg time.Time

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
	paneHashes map[string]uint64
}

type previewMsg struct {
	sessID  string
	preview string
	proc    sysstat.ProcStat
}

type errMsg struct{ err error }

type attachDoneMsg struct{ err error }

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine) *Model {
	return &Model{
		cfg:       cfg,
		store:     st,
		tmux:      driver,
		engine:    engine,
		collapsed: map[string]bool{},
		mode:      modeList,
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(m.cfg.PollInterval.Duration))
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
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

// derivePaneStatus turns one captured pane into a session status. The
// capture carries ANSI escapes for the preview; rules match against the
// stripped text. Streaming output often renders without any spinner, so
// when no rule matches but the content region above the input box changed
// since the previous poll, the session counts as working. Finished is an
// alert: entering the session acknowledges it (acked), and the pane keeps
// deriving finished until the next turn, so acked maps it back to idle.
func (m *Model) derivePaneStatus(sess store.Session, pane string, previousHashes, paneHashes map[string]uint64) string {
	text := ansi.Strip(pane)
	newStatus, matched := m.engine.Match(sess.Tool, text)
	if region, ok := m.engine.ActivityRegion(sess.Tool, text); ok {
		hash := hashString(region)
		paneHashes[sess.ID] = hash
		if !matched {
			if previous, seen := previousHashes[sess.ID]; seen && previous != hash {
				newStatus = status.Working
			}
		}
	}
	if newStatus == status.Finished && sess.Acked {
		newStatus = status.Idle
	}
	return newStatus
}

// refreshCmd polls every live session's pane once, deriving status and
// grabbing the selected session's pane text as the preview, then samples
// system stats. It runs off the render loop as a tea command. Liveness
// and pane pids come from one tmux call, and every process tree from one
// ps call, so the poll cost stays flat as sessions are added.
func (m *Model) refreshCmd() tea.Cmd {
	includeArchived := m.showArchived
	selectedID := ""
	if sess, ok := m.selected(); ok {
		selectedID = sess.ID
	}
	// Machine gauges change slowly; sample them every other poll.
	sampleStats := m.pollTick%2 == 0
	m.pollTick++
	previousHashes := m.paneHashes
	return func() tea.Msg {
		sessions, err := m.store.ListSessions(includeArchived)
		if err != nil {
			return errMsg{err}
		}

		panes, err := m.tmux.Panes()
		if err != nil {
			return errMsg{err}
		}
		var livePIDs []int
		for _, sess := range sessions {
			if !sess.Archived && panes[sess.ID] > 0 {
				livePIDs = append(livePIDs, panes[sess.ID])
			}
		}
		trees := sysstat.Trees(livePIDs)

		preview := ""
		var proc sysstat.ProcStat
		var agents agentStats
		paneHashes := make(map[string]uint64, len(sessions))
		for i, sess := range sessions {
			if sess.Archived {
				continue
			}
			newStatus := status.Dead
			if pid := panes[sess.ID]; pid > 0 {
				if stat := trees[pid]; stat.OK {
					agents.count++
					agents.cpu += stat.CPUPercent
					agents.rss += stat.RSS
					if sess.ID == selectedID {
						proc = stat
					}
				}
				if pane, err := m.tmux.CapturePane(sess.ID); err == nil {
					newStatus = m.derivePaneStatus(sess, pane, previousHashes, paneHashes)
					// Any real transition re-arms the finished alert.
					if sess.Acked && newStatus != status.Idle && newStatus != status.Finished {
						if err := m.store.SetAcked(sess.ID, false); err != nil {
							return errMsg{err}
						}
						sessions[i].Acked = false
					}
					if sess.ID == selectedID {
						preview = pane
					}
				}
			}
			if newStatus != sess.Status {
				if err := m.store.UpdateStatus(sess.ID, newStatus); err != nil {
					return errMsg{err}
				}
				sessions[i].Status = newStatus
			}
		}

		groups, err := m.store.Groups()
		if err != nil {
			return errMsg{err}
		}
		names := make([]string, len(groups))
		paths := make(map[string]string, len(groups))
		for i, g := range groups {
			names[i] = g.Name
			paths[g.Name] = g.Path
		}

		msg := refreshMsg{
			sessions:   sessions,
			groups:     names,
			groupPaths: paths,
			proc:       proc,
			procFor:    selectedID,
			preview:    preview,
			agents:     agents,
			paneHashes: paneHashes,
		}
		if sampleStats {
			msg.snap = sysstat.Sample("/")
			msg.snapOK = true
		}
		return msg
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.ageError()
		return m, tea.Batch(m.refreshCmd(), tickCmd(m.cfg.PollInterval.Duration))

	case refreshMsg:
		m.sessions = msg.sessions
		m.groups = msg.groups
		m.groupPaths = msg.groupPaths
		m.proc = msg.proc
		m.procFor = msg.procFor
		m.preview = msg.preview
		m.agents = msg.agents
		m.paneHashes = msg.paneHashes
		if msg.snapOK {
			m.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		m.rebuildRows()
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
		return m, m.refreshCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
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
	for _, sess := range m.sessions {
		if query != "" && !matchesSearch(sess, query) {
			continue
		}
		sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
	}

	paths := groupClosure(m.groups, m.sessions)
	if query != "" {
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
