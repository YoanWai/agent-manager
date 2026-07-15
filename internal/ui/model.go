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
	tea "github.com/charmbracelet/bubbletea"
)

type mode int

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeHelp
)

type Model struct {
	cfg    config.Config
	store  *store.Store
	tmux   *tmux.Driver
	engine *status.Engine

	sessions []store.Session
	nav      []store.Session
	groups   []string
	snap     sysstat.Snapshot
	proc     sysstat.ProcStat
	procFor  string

	cursor       int
	mode         mode
	showArchived bool
	collapsed    map[string]bool
	search       string
	searching    bool

	form form

	width  int
	height int
	err    string
}

type tickMsg time.Time

type refreshMsg struct {
	sessions []store.Session
	groups   []string
	snap     sysstat.Snapshot
	proc     sysstat.ProcStat
	procFor  string
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
	if m.cursor < 0 || m.cursor >= len(m.nav) {
		return store.Session{}, false
	}
	return m.nav[m.cursor], true
}

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
		for _, sess := range sessions {
			if sess.Archived {
				continue
			}
			newStatus := m.deriveStatus(sess)
			if newStatus != sess.Status {
				if err := m.store.UpdateStatus(sess.ID, newStatus); err != nil {
					return errMsg{err}
				}
			}
		}
		sessions, err = m.store.ListSessions(includeArchived)
		if err != nil {
			return errMsg{err}
		}
		groups, err := m.store.Groups()
		if err != nil {
			return errMsg{err}
		}

		diskPath := "/"
		var proc sysstat.ProcStat
		for _, sess := range sessions {
			if sess.ID == selectedID {
				diskPath = sess.Cwd
				if pid, err := m.tmux.PanePID(sess.ID); err == nil {
					proc = sysstat.Proc(pid)
				}
			}
		}
		snap := sysstat.Sample(diskPath)

		return refreshMsg{
			sessions: sessions,
			groups:   groups,
			snap:     snap,
			proc:     proc,
			procFor:  selectedID,
		}
	}
}

func (m *Model) deriveStatus(sess store.Session) string {
	if !m.tmux.Exists(sess.ID) {
		return status.Dead
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		return status.Dead
	}
	return m.engine.Derive(sess.Tool, pane)
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
		m.rebuildNav()
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

func (m *Model) rebuildNav() {
	nav := make([]store.Session, 0, len(m.sessions))
	query := strings.ToLower(strings.TrimSpace(m.search))
	for _, sess := range m.orderedSessions() {
		if query != "" && !matchesSearch(sess, query) {
			continue
		}
		if m.collapsed[sess.Group] {
			continue
		}
		nav = append(nav, sess)
	}
	m.nav = nav
	if m.cursor >= len(nav) {
		m.cursor = len(nav) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) orderedSessions() []store.Session {
	groupOrder := map[string]int{}
	for i, g := range m.groups {
		groupOrder[g] = i
	}
	ordered := make([]store.Session, len(m.sessions))
	copy(ordered, m.sessions)
	sort.SliceStable(ordered, func(i, j int) bool {
		gi, gj := groupOrder[ordered[i].Group], groupOrder[ordered[j].Group]
		if gi != gj {
			return gi < gj
		}
		return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
	})
	return ordered
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
