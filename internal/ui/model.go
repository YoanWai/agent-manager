package ui

import (
	"crypto/rand"
	"encoding/hex"
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
)

type mode int

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeHelp
	modeRename
	modeMove
)

type row struct {
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

	sessions []store.Session
	rows     []row
	groups   []string
	snap     sysstat.Snapshot
	proc     sysstat.ProcStat
	procFor  string
	preview  string

	cursor       int
	mode         mode
	showArchived bool
	collapsed    map[string]bool
	search       string
	searching    bool

	form    form
	confirm confirmTarget
	rename  renameTarget
	moveID  string

	width  int
	height int
	err    string
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

type tickMsg time.Time

type refreshMsg struct {
	sessions []store.Session
	groups   []string
	snap     sysstat.Snapshot
	proc     sysstat.ProcStat
	procFor  string
	preview  string
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

func (m *Model) selectedRow() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

// refreshCmd polls every live session's pane once, deriving status and
// grabbing the selected session's pane text as the preview, then samples
// system stats. It runs off the render loop as a tea command.
func (m *Model) refreshCmd() tea.Cmd {
	includeArchived := m.showArchived
	selectedID := ""
	if sess, ok := m.selected(); ok {
		selectedID = sess.ID
	}
	return func() tea.Msg {
		sessions, err := m.store.ListSessions(includeArchived)
		if err != nil {
			return errMsg{err}
		}

		preview := ""
		diskPath := "/"
		var proc sysstat.ProcStat
		for i, sess := range sessions {
			if sess.ID == selectedID {
				diskPath = sess.Cwd
				if pid, err := m.tmux.PanePID(sess.ID); err == nil {
					proc = sysstat.Proc(pid)
				}
			}
			if sess.Archived {
				continue
			}
			newStatus := status.Dead
			if m.tmux.Exists(sess.ID) {
				if pane, err := m.tmux.CapturePane(sess.ID); err == nil {
					newStatus = m.engine.Derive(sess.Tool, pane)
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

		return refreshMsg{
			sessions: sessions,
			groups:   groups,
			snap:     sysstat.Sample(diskPath),
			proc:     proc,
			procFor:  selectedID,
			preview:  preview,
		}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refreshCmd(), tickCmd(m.cfg.PollInterval.Duration))

	case refreshMsg:
		m.sessions = msg.sessions
		m.groups = msg.groups
		m.snap = msg.snap
		m.proc = msg.proc
		m.procFor = msg.procFor
		m.preview = msg.preview
		m.rebuildRows()
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

func rowKey(r row) string {
	if r.isGroup {
		return "g:" + r.group
	}
	return "s:" + r.sess.ID
}

// rebuildRows walks the group tree depth-first and emits one row per
// group node and per session, honoring collapse state and search.
// The cursor follows the previously selected row's identity, so list
// changes from the 2s poll never yank the selection around.
func (m *Model) rebuildRows() {
	previousKey := ""
	if r, ok := m.selectedRow(); ok {
		previousKey = rowKey(r)
	}
	query := strings.ToLower(strings.TrimSpace(m.search))

	sessionsByGroup := map[string][]store.Session{}
	for _, sess := range m.sessions {
		if query != "" && !matchesSearch(sess, query) {
			continue
		}
		sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
	}
	for _, group := range sessionsByGroup {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].CreatedAt.Before(group[j].CreatedAt)
		})
	}

	paths := groupClosure(m.groups, m.sessions)
	if query != "" {
		paths = pathsWithSessions(paths, sessionsByGroup)
	}
	children := childIndex(paths)

	rows := make([]row, 0, len(m.sessions)+len(paths))
	for _, sess := range sessionsByGroup[""] {
		rows = append(rows, row{sess: sess})
	}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		rows = append(rows, row{isGroup: true, group: path, depth: depth})
		if m.collapsed[path] {
			return
		}
		for _, sess := range sessionsByGroup[path] {
			rows = append(rows, row{sess: sess, depth: depth + 1})
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
		for i, r := range rows {
			if rowKey(r) == previousKey {
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

func childIndex(paths map[string]bool) map[string][]string {
	children := map[string][]string{}
	for path := range paths {
		parent := ""
		if idx := strings.LastIndex(path, "/"); idx >= 0 {
			parent = path[:idx]
		}
		children[parent] = append(children[parent], path)
	}
	for _, siblings := range children {
		sort.Strings(siblings)
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
