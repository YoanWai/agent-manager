package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/update"
	tea "github.com/charmbracelet/bubbletea"
)

type mode int

const (
	modeList mode = iota
	modeForm
	modeConfirmDelete
	modeHelp
	modeRename
	modeFork
	modeMove
	modeRepoPick
	modeGroupForm
	modeSettings
	modeDiff
	modeNotices
	// modeLaunchHint holds a refused spawn's fix in a dialog: the launch
	// stays blocked, and the command that unblocks it is what the user sees.
	modeLaunchHint
	// modeFocus routes the keyboard into the selected session's pane while
	// the list and live preview stay on screen.
	modeFocus
)

type treeRow struct {
	isGroup bool
	group   string
	depth   int
	sess    store.Session
}

type Model struct {
	cfg      config.Config
	store    *store.Store
	tmux     *tmux.Driver
	hooks    *hooks.Manager
	gitDrv   *git.Driver
	engine   *status.Engine
	keys     keybind.Table
	listKeys keybind.Table
	// configDir is resolved once, at New, so the settings screen writes
	// keys back to the config.toml the manager loaded.
	configDir string

	// setSnapshot writes a session's pane capture before archive or kill
	// takes the window; a seam so snapshot failures can be exercised
	// without a broken store.
	setSnapshot func(id, snapshot string) error

	sessions []store.Session
	rows     []treeRow
	// tmuxSocket is the server the last poll read panes from; rows stamped
	// with another server belong to a manager running against it, and rows
	// stamped by none belong to whichever manager holds the store.
	tmuxSocket     string
	leadingManager bool

	groups         []string
	groupPaths     map[string]string
	groupWorktrees map[string]string
	// worktreeRepos memoizes which spawn directories sit inside a git
	// repo, so gating the worktree toggle does not shell out to git on
	// every frame. Entries expire, so a directory git-initialised while
	// the bar is open stops reading as unavailable.
	worktreeRepos  map[string]repoAnswer
	archivedGroups map[string]bool
	snap           sysstat.Snapshot
	proc           sysstat.ProcStat
	procFor        string
	preview        string
	agents         agentStats
	// queuedMessages is replaced whole on every refresh rather than merged,
	// so a delivered message's badge clears itself.
	queuedMessages map[string]int
	// paneLines holds each session's last meaningful pane line, which the
	// full screen row's second line quotes. Merged rather than replaced, so
	// a session that lost its window keeps its last words.
	paneLines map[string]string
	// panePrompts holds the last prompt each session's transcript echoes,
	// which the full screen row shows as the task it is on.
	panePrompts map[string]string
	// panes is the last pass's agent pane geometry, which the poller reads
	// off the UI loop alongside its liveness listing.
	panes map[string]tmux.Pane

	net netStats

	poller *poller
	focus  *focusWatch
	// sel is the focused-pane selection, written during paint so clicks
	// resolve against the current frame. copied is the size of the last
	// clipboard write, shown once in the status line and cleared on the
	// next selection. copyGen rises whenever the selection behind a write
	// stops being the one on screen, so a write that lands late is dropped
	// instead of re-arming the count under nothing.
	copied  int
	copyGen int
	sel     focusSelection
	// forwardingMouse holds an Alt-initiated in-pane click lifecycle until
	// its release. The button and last in-pane cell keep an X10 release
	// paired with its press when it reports MouseButtonNone outside the pane.
	forwardingMouse  bool
	forwardingButton int
	forwardingRow    int
	forwardingCol    int
	// pending is a press in a mouse-tracking pane awaiting its verdict:
	// selection drag or forwarded click.
	pending pendingClick
	// listClickAt and listClickKey remember the last rail press so two
	// presses on the same row inside multiClickWindow count as a double click.
	listClickAt  time.Time
	listClickKey string
	pane         paneMirror
	// cursorOn is the caret's blink phase while focused.
	cursorOn bool
	// imeCursor is shared with the terminal output writer so the host input
	// method can follow whichever software caret the UI rendered.
	imeCursor *cursorAnchor
	// focusScroll is how many lines the focused pane is scrolled back into
	// its history; zero is live at the bottom.
	focusScroll int
	// focusFetchInFlight guards the scroll-region pipeline: one capture
	// rides the control pipe at a time, and a wheel that moved the target
	// meanwhile is served by the reply's own follow-up fetch. Without it a
	// fast wheel queues a full history capture per notch plus a catch-up
	// per stale reply, and the pipe answers them for half a minute.
	focusFetchInFlight bool
	// focusOnEnter mirrors the persisted focus-key setting; the footer
	// reads it every frame, so it lives here instead of the store.
	focusOnEnter bool
	// arrowStep mirrors the persisted ←→ step-in/step-out setting, read
	// on every keypress.
	arrowStep bool
	// comfortableRows mirrors the persisted list density: entries paint
	// their meta on a second line instead of alongside the name. Every
	// rail frame reads it, so it lives here instead of the store.
	comfortableRows bool
	// fullLayout mirrors the persisted sessions layout: the rail owns the
	// whole width, with no preview column beside it. Every list frame
	// reads it, so it lives here instead of the store.
	fullLayout bool
	// Header and stats visibility stay cached because rendering and sizing
	// read them every frame.
	hideHeader bool
	hideStats  bool
	// mouseDisabled mirrors the persisted mouse-reporting setting: true gives
	// the rail and content column back to the terminal's own click-drag text
	// selection. Read on every Update via syncMouseCapture. Named for its off
	// polarity, like hideHeader/hideStats, so a bare Model{} in a test still
	// defaults to mouse reporting on.
	mouseDisabled bool
	// watchedGen is previewGen as of the last poll pass, so a selection
	// that has not moved since can be recognised as at rest.
	watchedGen        uint64
	previewBodyOffset int
	cursor            int
	// railTop is the entry the rail paints first, carried between frames.
	// Deriving it from the cursor alone cannot hold still: rows are of
	// uneven height, so every step would re-solve the window and slide the
	// list under a highlight that should have simply moved down.
	railTop int
	// railHits maps each line the rail painted this frame to the m.rows
	// index a click there selects, -1 for chrome (search field, badges,
	// padding, meters) a click cannot select. Recorded by recordRailHits
	// at paint time, the way m.pane.box is for the focused pane, so a
	// click handler never has to re-derive the rail's layout and drift
	// from it.
	railHits        []int
	noticeHit       noticeHit
	mode            mode
	showArchived    bool
	hideEmptyGroups bool
	statusFilter    statusFilter
	collapsed       map[string]bool
	search          string
	searching       bool

	diff      diffState
	form      form
	groupForm groupForm
	pathSugg  pathComplete
	confirm   confirmTarget
	launchFix launchFix
	// install is the setup-dialog install still running in a shell tab,
	// nil when none is.
	install *pendingInstall
	// mouseReleased is true while the setup dialog has handed the mouse
	// back to the terminal, so a drag selects its text.
	mouseReleased     bool
	rename            renameTarget
	fork              forkState
	quick             quickState
	lastSpawnTool     string
	lastSpawnWorktree bool
	// composerSeq numbers the prompt boxes this run has opened.
	composerSeq int
	settings    settingsState
	help        helpState
	moveID      string
	movePath    string
	repoPick    repoPickState
	// editorReturnID is the session an editor request detached from, so the
	// attach it cost can be resumed once the editor is up.
	editorReturnID string

	// Repo a human picked by hand per session, outranking the agent's
	// declaration for as long as this manager runs.
	pickedRepos map[string]string

	// awaitedRenames holds what a spawned session launched with, for as long
	// as the agent it carries the rename directive to is still expected to
	// answer. A rename that has not landed by the time this manager run ends
	// is one that is never arriving, so the set is deliberately not persisted.
	awaitedRenames map[string]awaitedRename

	width  int
	height int
	// sessionsSized flips after the first refresh shrinks sessions left
	// over from a previous manager run to the preview panel's width.
	sessionsSized bool
	errBar        errBar
	split         splitState
	// previewGen increments on every cursor move. In-flight captures and
	// settle timers with an older gen are dropped so key-repeat cannot
	// queue a second of tmux work after the user stops.
	previewGen uint64
	// launched is when this run recorded each session it spawned. A poll
	// that listed the store before that has nothing to say about the row.
	launched map[string]time.Time
	// gone is when this run took each session off the loaded list itself,
	// by deleting or archiving it, so a poll that listed the store before
	// that moment cannot put the row back on screen for a frame.
	gone map[string]goneMark
	// goneGroups is the group-path counterpart of gone: when this run
	// archived, restored, or deleted a group, a poll that listed the store
	// before that moment must not put the old state back on the tree.
	goneGroups map[string]goneMark
	// terminalKeyAt is when the last T finished being handled. Held down it
	// autorepeats into a burst of keystrokes, and T is the only key that
	// spawns on the keystroke itself rather than opening a form that would
	// swallow them.
	terminalKeyAt time.Time

	// bannerPhase advances the wordmark's current sweep and then rests, so
	// the frame is not repainted forever.
	bannerPhase int

	startupPhase     int
	startupAnimating bool
	booting          bool
	pendingTyped     *typedPromptCandidate

	update updateInfo

	// dismissed holds the notice ids the user closed for good; the set
	// persists in settings so a dismissed message never comes back.
	dismissed    map[string]bool
	noticeCursor int
	noticeScroll int
	// whatsNewVersion mirrors the persisted whats_new_version setting so
	// the notices list, rebuilt every frame, never reads the database.
	whatsNewVersion     string
	whatsNewFromVersion string
	// feedMessages is the remote message feed, refreshed on the update
	// tick and folded into the notices next to the built-in ones.
	feedMessages []feed.Message
	// pendingNotice is a new notice that arrived while the list was not
	// showing; flushPendingNotice opens it once the list is back.
	pendingNotice string
}

// paneMirror is the focused pane's state: mouse ownership, appetite for
// pointer moves, report encoding and history depth as the watcher last
// reported them, so the wheel routes without a tmux round trip mid-Update.
// box, columnX and cursor are hit-test geometry written during paint; geom
// is the last width×height told to tmux per session id, skipping no-op
// resize-window calls that otherwise stall the UI.
type paneMirror struct {
	// forID is the session whose pushed capture wrote the fields below;
	// a serving watcher alone does not prove them current, since its
	// first capture may still be in flight.
	forID   string
	mouse   bool
	motion  bool
	sgr     bool
	history int
	box     paneBox
	columnX int
	cursor  paneCursor
	geom    map[string][2]int
	// pids is the pane process each geom entry was measured on, so a pane
	// replaced by a relaunch can be told from the one the manager sized.
	pids map[string]int
	// published is the last box written to the store for the paneless
	// launch paths to read.
	published [2]int
}

type errBar struct {
	text  string
	done  string
	warn  string
	shown string
	age   int
}

// worked reports whether the message on the bar is an action that went
// through, so it can be styled as an outcome rather than a failure. Only
// reportDone fills done, and any later write to text alone leaves it
// behind, so a message says it worked or reads as a failure.
func (e errBar) worked() bool { return e.text != "" && e.text == e.done }

// warned reports whether the message is an action that went through with
// a caveat the reader has to see, which reads as neither outcome nor
// failure.
func (e errBar) warned() bool { return e.text != "" && e.text == e.warn }

// reportDone puts an action that went through on the status bar.
func (m *Model) reportDone(text string) {
	m.errBar.text, m.errBar.done = text, text
}

// reportWarn puts an action that went through with a caveat on the status
// bar.
func (m *Model) reportWarn(text string) {
	m.errBar.text, m.errBar.warn = text, text
}

type errMsg struct{ err error }

type attachDoneMsg struct {
	sessID string
	err    error
}

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, version string) *Model {
	statusSources := make(map[string]string, len(cfg.Tools))
	sessionStores := make(map[string]string, len(cfg.Tools))
	mcpStyles := make(map[string]string, len(cfg.Tools))
	shellTools := make(map[string]bool, len(cfg.Tools))
	for name, tool := range cfg.Tools {
		shellTools[name] = tool.Shell
		statusSources[name] = tool.StatusSource
		sessionStores[name] = tool.SessionStore
		mcpStyles[name] = mcpreg.Style(name, tool.MCP)
	}
	// A missing git binary only disables the diff view; everything else
	// works without it, so the error surfaces on first use instead.
	gitDriver, _ := git.New()
	applyTheme(themes[themeIndex(resolveStartupTheme(st))])
	driver.SetSessionKeys(cfg.SessionKeys)
	model := &Model{
		cfg:                 cfg,
		store:               st,
		tmux:                driver,
		keys:                cfg.SessionKeys,
		listKeys:            cfg.ListKeys,
		hooks:               hookManager,
		gitDrv:              gitDriver,
		engine:              engine,
		setSnapshot:         st.SetSnapshot,
		poller:              newPoller(st, driver, engine, hookManager, gitDriver, statusSources, sessionStores, mcpStyles, shellTools, newToolBinaries(cfg), cfg.PollInterval.Duration),
		collapsed:           loadCollapsed(st),
		split:               splitState{ratio: loadSplitRatio(st)},
		focusOnEnter:        storedFocusOnEnter(st),
		arrowStep:           storedArrowStep(st),
		comfortableRows:     storedComfortableRows(st),
		fullLayout:          storedFullLayout(st),
		hideHeader:          storedHideHeader(st),
		hideStats:           storedHideStats(st),
		mouseDisabled:       storedMouseDisabled(st),
		imeCursor:           &cursorAnchor{},
		mode:                modeList,
		booting:             true,
		update:              updateInfo{version: version},
		dismissed:           loadDismissed(st),
		whatsNewVersion:     loadWhatsNewVersion(st),
		whatsNewFromVersion: loadWhatsNewFromVersion(st),
	}
	if dir, err := config.Dir(); err == nil {
		model.configDir = dir
		cached := update.Cached(dir, version)
		model.update.latest = cached.Latest
		model.update.url = cached.URL
		model.update.releases = cached.Releases
		model.update.checked = len(cached.Releases) > 0
	}
	model.openStartupNotice()
	model.indexReleaseRanges()
	return model
}

func (m *Model) Init() tea.Cmd {
	m.syncPollInput()
	return tea.Batch(m.syncPaneTheme(), m.refreshExistingSessionUX, m.checkForUpdate, m.checkFeed, m.updateTick(), m.bannerTick(), m.previewTick(), m.startStartupTick(), m.sweepPastes, m.pasteSweepTick())
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

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.handleMsg(msg)
	if mm, ok := model.(*Model); ok {
		mm.flushPendingNotice()
		return mm, tea.Batch(cmd, mm.syncMouseCapture())
	}
	return model, tea.Batch(cmd, m.syncMouseCapture())
}

// syncMouseCapture hands the mouse to the terminal while the setup dialog
// is up, so a drag over it selects the install command, and takes it back
// when the dialog closes. The mouse-mode setting does the same for anyone
// who wants native click-drag selection back everywhere else, except in
// focus mode: that pane's own mouse forwarding predates the setting and
// stays on regardless, the way it always has.
func (m *Model) syncMouseCapture() tea.Cmd {
	release := m.mode == modeLaunchHint || (m.mouseDisabled && m.mode != modeFocus)
	if release == m.mouseReleased {
		return nil
	}
	m.mouseReleased = release
	if release {
		return tea.DisableMouse
	}
	return tea.EnableMouseCellMotion
}

func (m *Model) handleMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		// Re-assert the terminal backdrop: a reattach or a fresh outer
		// terminal delivers a size message and may carry stale colors.
		SyncTerminalBackground()
		m.publishPaneSize()
		m.resizeSessions()
		if m.fullFocus() {
			if sess, ok := m.selected(); ok {
				m.pinFullFocusPane(sess.ID)
			}
		}
		if m.mode == modeForm {
			m.syncFormFieldWidths()
		} else if m.mode == modeGroupForm {
			m.syncGroupFormFieldWidths()
		}
		return m, nil

	case bannerTickMsg:
		m.bannerPhase++
		return m, m.bannerTick()

	case bannerShimmerMsg:
		m.bannerPhase = 0
		return m, m.bannerTick()

	case browserOpenMsg:
		m.handleBrowserOpen(msg)
		return m, nil

	case startupTickMsg:
		if !m.needsLoaderTick() {
			m.startupAnimating = false
			return m, nil
		}
		m.startupPhase++
		return m, m.startupTick()

	case previewTickMsg:
		// Only the list keeps a live pane on screen; review and the modal
		// screens have no preview to feed, so they skip the capture and
		// just keep the timer alive.
		sess, ok := m.selected()
		if !ok || (m.mode != modeList && m.mode != modeRename && m.mode != modeFocus) {
			return m, m.previewTick()
		}
		// A session with a control client already pushes every frame; a
		// tick capture on top of that is work whose result is discarded.
		if m.focus != nil && m.focus.serving(sess.ID) {
			return m, m.previewTick()
		}
		return m, tea.Batch(m.previewCmd(sess, m.previewGen), m.previewTick())

	case refreshMsg:
		m.booting = false
		m.ageError()
		// The focused session can die or vanish under us; fall back to the
		// list rather than typing into nothing.
		sessions := m.dropRecentlyRemoved(m.keepPendingLaunches(msg.sessions, msg.listedAt), msg.listedAt)
		stripDeletedGroups(&msg, m.goneGroups)
		var focusExit tea.Cmd
		if m.mode == modeFocus {
			if sess, ok := m.selected(); !ok || sessionGone(sessions, sess.ID) {
				focusExit = m.leaveFocus()
			}
		}
		m.sessions = sessions
		m.tmuxSocket = msg.tmuxSocket
		m.leadingManager = msg.leadingManager
		m.panes = msg.panes
		m.groups = msg.groups
		m.groupPaths = msg.groupPaths
		m.groupWorktrees = msg.groupWorktrees
		m.archivedGroups = msg.archivedGroups
		m.agents = msg.agents
		m.queuedMessages = msg.queuedMessages
		if m.paneLines == nil {
			m.paneLines = map[string]string{}
		}
		for id, line := range msg.paneLines {
			m.paneLines[id] = line
		}
		if m.panePrompts == nil {
			m.panePrompts = map[string]string{}
		}
		for id, prompt := range msg.panePrompts {
			if prompt != "" {
				m.panePrompts[id] = prompt
			}
		}
		m.commitTypedPrompt()
		if msg.snapOK {
			m.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		// Sessions left from a previous run carry that run's window size,
		// which the cache knows nothing about; seedPaneGeom adopts their
		// real geometry on the first pass, so nothing resets the cache here.
		if !m.sessionsSized && m.width > 0 && len(m.sessions) > 0 {
			m.sessionsSized = true
		}
		m.publishPaneSize()
		// The preview box changes height for more reasons than a terminal
		// resize: the quick bar opening, the status line appearing, a new
		// badge in the header. A pane shorter than the box paints a dead
		// band under its output, so every pass grows what falls short.
		// The call is free when nothing moved: it diffs against paneGeom.
		if m.sessionsSized && m.width > 0 {
			m.resizeSessions()
		}
		m.settleInstall()
		m.rebuildRows()
		if msg.focusID != "" {
			focused, ok := m.selected()
			// Moving the cursor under a focused pane would leave the
			// keyboard pinned to the session the user was in while the
			// list claims another. The click steps back to the list, and
			// only once its session turns out to have a row to land on.
			if m.focusSession(msg.focusID) && m.mode == modeFocus && (!ok || focused.ID != msg.focusID) {
				focusExit = m.leaveFocus()
			}
		}
		reviewStatuses := m.reviewStatusesCmd()
		// A pass that ran with a stale selection (a session created this
		// tick, or one a notification click just chose) carries the wrong
		// preview; resync and fetch it directly.
		if sess, ok := m.selected(); ok && sess.ID != msg.procFor {
			m.syncPollInput()
			m.previewGen++
			return m, tea.Batch(focusExit, m.previewCmd(sess, m.previewGen), m.diffRefreshCmd(), reviewStatuses, m.startStartupTick())
		}
		m.proc = msg.proc
		m.procFor = msg.procFor
		m.setPreview(msg.procFor, msg.preview)
		// A selection that has not moved since the last pass is at rest,
		// so this covers the startup case where no settle ever fired.
		if m.previewGen == m.watchedGen {
			m.watchSelection()
		}
		m.watchedGen = m.previewGen
		return m, tea.Batch(focusExit, m.diffRefreshCmd(), reviewStatuses, m.startStartupTick())

	case updateMsg:
		if msg.manual {
			m.finishNoticeRefresh()
		}
		if msg.failed && len(msg.releases) == 0 {
			if msg.manual && msg.err != nil {
				m.errBar.text = "refresh failed: " + msg.err.Error()
			}
			return m, nil
		}
		m.applyNotices(func() {
			m.update.latest = msg.latest
			m.update.url = msg.url
			m.update.releases = msg.releases
			m.update.checked = true
			m.indexReleaseRanges()
		})
		if msg.manual && msg.err != nil {
			m.errBar.text = "refresh failed: " + msg.err.Error()
		}
		return m, nil

	case updateAppliedMsg:
		m.update.applying = false
		if len(msg.result.Releases) > 0 {
			m.keepNoticeSelection(func() {
				m.update.latest = msg.result.Latest
				m.update.url = msg.result.URL
				m.update.releases = msg.result.Releases
				m.update.checked = true
				m.indexReleaseRanges()
			})
		}
		if msg.err != nil {
			m.errBar.text = "update failed: " + msg.err.Error()
			return m, nil
		}
		if msg.upToDate {
			m.keepNoticeSelection(func() {
				m.update.latest = ""
				m.update.url = ""
				if len(msg.result.Releases) == 0 {
					m.update.releases = nil
					m.update.checked = true
				}
				m.indexReleaseRanges()
			})
			m.reportDone("already up to date")
			return m, nil
		}
		m.update.restartPath = msg.path
		return m, tea.Quit

	case updateTickMsg:
		return m, tea.Batch(m.checkForUpdate, m.checkFeed, m.updateTick())

	case feedMsg:
		if msg.manual {
			m.finishNoticeRefresh()
		}
		if !msg.failed || len(msg.messages) > 0 {
			m.applyNotices(func() { m.feedMessages = msg.messages })
		}
		if msg.manual && msg.err != nil {
			m.errBar.text = "refresh failed: " + msg.err.Error()
		}
		return m, nil

	case pasteSweepMsg:
		if msg.err != nil {
			m.errBar.text = "clearing old pasted images: " + msg.err.Error()
		}
		return m, nil

	case pasteSweepTickMsg:
		return m, tea.Batch(m.sweepPastes, m.pasteSweepTick())

	case replyCopiedMsg:
		if msg.unreadable {
			m.reportWarn(fmt.Sprintf("no reply to read in %s: a %s pane is not read that way", msg.name, msg.tool))
			return m, nil
		}
		if msg.chars == 0 {
			m.errBar.text = "nothing to copy from " + msg.name
			return m, nil
		}
		if msg.unbounded {
			m.reportWarn(fmt.Sprintf("copied %d chars from %s: %s marks no turn start here, so this is the whole pane",
				msg.chars, msg.name, msg.tool))
			return m, nil
		}
		m.reportDone(fmt.Sprintf("copied %d chars from %s", msg.chars, msg.name))
		return m, nil

	case previewSettleMsg:
		if msg.gen != m.previewGen {
			return m, nil
		}
		sess, ok := m.selected()
		if !ok {
			return m, nil
		}
		// The cursor has come to rest: this is where the control client is
		// worth opening.
		m.watchSelection()
		return m, m.previewCmd(sess, msg.gen)

	case cursorBlinkMsg:
		if m.mode != modeFocus {
			return m, nil
		}
		m.cursorOn = !m.cursorOn
		return m, m.cursorBlink()

	case linkOpenErrMsg:
		m.errBar.text = msg.err.Error()
		return m, nil

	case linkPageMsg:
		return m, showLinkPage(msg.url)

	case launchCommandCopiedMsg:
		m.handleLaunchCommandCopied(msg)
		return m, nil

	case focusCopiedMsg:
		// The clipboard writer runs off the update loop and can take
		// hundreds of milliseconds, long enough for a click elsewhere to
		// drop the highlight this count belongs to.
		if msg.gen != m.copyGen {
			return m, nil
		}
		m.errBar.text = ""
		m.copied = msg.chars
		return m, nil

	case focusScrollMsg:
		sess, ok := m.selected()
		if !ok || sess.ID != msg.sessID {
			m.focusFetchInFlight = false
			return m, nil
		}
		if msg.offset != m.focusScroll || msg.rows != m.focusPaneRows() {
			// The wheel or a resize moved the target while this capture was
			// in flight. Fetch just that final viewport; the in-flight guard
			// stays up so the notches that keep arriving ride this fetch.
			return m, m.focusRegionCmd(sess.ID, m.focusScroll)
		}
		m.focusFetchInFlight = false
		if msg.ok {
			m.preview = msg.preview
		}
		return m, nil

	case focusPreviewMsg:
		sess, ok := m.selected()
		if !ok || sess.ID != msg.sessID {
			return m, nil
		}
		m.pane.forID = msg.sessID
		m.pane.mouse = msg.paneMouse
		m.pane.motion = msg.paneMotion
		m.pane.sgr = msg.paneSGR
		m.pane.history = msg.historySize
		// Once the app owns the wheel, nothing can walk a leftover offset
		// back down, and holding it would freeze the view for good. A
		// mouse-tracking agent keeps no tmux history, though, so history
		// beside a wheel claim is the cached flag trailing an app that
		// just left mouse mode, and the offset stays reachable.
		if m.pane.mouse && m.focusScroll != 0 && m.pane.history == 0 {
			m.focusScroll = 0
		}
		// A scrolled-back pane holds still: live frames would yank the
		// view back to the bottom mid-read.
		if m.scrolledBack() {
			return m, nil
		}
		m.preview = msg.preview
		m.pane.cursor = paneCursor{
			x: msg.cursorX, y: msg.cursorY,
			ok: msg.cursorOK, positionOK: msg.paneStateOK,
		}
		return m, nil

	case previewMsg:
		if msg.gen != 0 && msg.gen != m.previewGen {
			return m, nil
		}
		if sess, ok := m.selected(); ok && sess.ID == msg.sessID {
			m.setPreview(msg.sessID, msg.preview)
			m.proc = msg.proc
			m.procFor = msg.sessID
		}
		return m, nil

	case diffLoadedMsg:
		return m, m.handleDiffLoaded(msg)

	case diffFileLoadedMsg:
		return m, m.handleDiffFileLoaded(msg)

	case diffFilesLoadedMsg:
		var cmds []tea.Cmd
		for _, loaded := range msg {
			if cmd := m.handleDiffFileLoaded(loaded); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		return m, tea.Batch(cmds...)

	case diffHLMsg:
		m.handleDiffHL(msg)
		return m, nil

	case diffProbeMsg:
		return m, m.handleDiffProbe(msg)

	case reviewStatusesLoadedMsg:
		m.handleReviewStatusesLoaded(msg)
		return m, nil

	case reviewStateSavedMsg:
		m.handleReviewStateSaved(msg)
		return m, nil

	case reviewCommentHandledMsg:
		m.handleReviewCommentHandled(msg)
		return m, nil

	case reviewSendFinishedMsg:
		m.handleReviewSendFinished(msg)
		return m, nil

	case errMsg:
		m.errBar.text = msg.err.Error()
		return m, nil

	case pasteImageMsg:
		return m.handlePasteImageMsg(msg)

	case pasteTextMsg:
		return m.handlePasteTextMsg(msg)

	case attachDoneMsg:
		// An agent that repainted the terminal background for itself leaves
		// it on ours; the resume's WindowSizeMsg skips its own sync when the
		// size is unchanged, so the detach restores the theme's here.
		SyncTerminalBackground()
		// The attach client sized the window to the full terminal and tmux
		// keeps that size on detach; pin it back to the current layout's
		// box so the capture is not clipped on the right.
		if m.pane.geom != nil {
			delete(m.pane.geom, msg.sessID)
		}
		width, height := m.paneTargetSize()
		m.poller.reflowSessions([]string{msg.sessID}, func() {
			_ = m.tmux.Resize(msg.sessID, width, height)
		})
		if m.pane.geom == nil {
			m.pane.geom = map[string][2]int{}
		}
		m.pane.geom[msg.sessID] = [2]int{width, height}
		if msg.err != nil {
			m.errBar.text = msg.err.Error()
			m.requestRefresh()
			return m, nil
		}
		// Ctrl+R and F3 inside the session leave a marker before
		// detaching; consume it here and carry it out for the session just
		// attached.
		request, err := m.tmux.PendingRequest()
		if err != nil {
			m.errBar.text = err.Error()
		} else if request != "" {
			// A failed clear leaves the marker set, which would replay the
			// request on every later detach, so surface it and stay in the
			// list rather than letting the request reset m.errBar.text and
			// hide it.
			if clearErr := m.tmux.ClearRequest(); clearErr != nil {
				m.errBar.text = clearErr.Error()
				m.requestRefresh()
				return m, nil
			}
			// Both requests act on the row under the cursor, and the cursor
			// is not where the request came from: a poll handled ahead of
			// this message rebuilds the rows, and a filter can drop the
			// session that detached out of the list entirely.
			m.focusSession(msg.sessID)
			sess, ok := m.selected()
			if !ok || sess.ID != msg.sessID {
				m.errBar.text = "the session that asked for it has left the list"
				m.requestRefresh()
				return m, nil
			}
			switch request {
			case tmux.RequestReview:
				cmd := m.openDiff()
				if m.mode == modeDiff {
					m.diff.reattachID = sess.ID
				}
				return m, cmd
			case tmux.RequestEditor:
				_, cmd := m.openEditor()
				// The request cost the session its client, so the manager
				// goes back into it once the editor is up, or once a
				// terminal editor closes. A refused launch returns no
				// command and stays in the list with its reason.
				if cmd != nil {
					m.editorReturnID = sess.ID
				}
				return m, cmd
			}
		}
		m.requestRefresh()
		return m, nil

	case diffFileCheckedMsg:
		return m.handleDiffFileChecked(msg)

	case editorDoneMsg:
		var resume tea.Cmd
		if msg.tookScreen {
			// The terminal comes back from an editor the way it comes back
			// from an attach: painted in the editor's background, and
			// without the mouse reporting focus mode armed on the way in.
			SyncTerminalBackground()
			if m.mode == modeFocus {
				resume = tea.EnableMouseCellMotion
			}
		}
		if msg.err != nil {
			// Going back into the session would hide the only account of
			// what went wrong, so a failed editor keeps the list.
			m.errBar.text = msg.err.Error()
			m.editorReturnID = ""
			return m, resume
		}
		if msg.name != "" {
			m.reportDone("opened " + msg.path + " in " + msg.name)
		}
		if id := m.editorReturnID; id != "" {
			m.editorReturnID = ""
			return m, tea.Batch(resume, m.reattach(id, m.diff.gen))
		}
		return m, resume

	case relaunchedMsg:
		if msg.err != nil {
			m.reportLaunchError(msg.err, nil)
			return m, nil
		}
		m.bindReviveLocally(msg.sessID, msg.launchedAt)
		m.rebuildRows()
		m.requestRefresh()
		return m, nil

	case reattachPreparedMsg:
		if msg.diffGen != m.diff.gen || m.diff.active {
			return m, nil
		}
		if msg.err != nil {
			m.errBar.text = msg.err.Error()
			return m, nil
		}
		m.errBar.text = msg.warn
		return m, execTerminalProcess(m.tmux.AttachCommand(msg.sessID), func(err error) tea.Msg {
			return attachDoneMsg{sessID: msg.sessID, err: err}
		})

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		model, cmd := m.handleKey(msg)
		m.syncPollInput()
		return model, cmd
	}
	return m, nil
}

// ageError clears a status message after it has survived a couple of poll
// ticks, so transient errors self-dismiss without any per-callsite timers.
func (m *Model) ageError() {
	if m.errBar.text == "" {
		m.errBar = errBar{}
		return
	}
	if m.errBar.text != m.errBar.shown {
		m.errBar.shown, m.errBar.age = m.errBar.text, 0
		return
	}
	m.errBar.age++
	if m.errBar.age >= 2 {
		m.errBar = errBar{}
	}
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func newID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405")))
	}
	return hex.EncodeToString(buf)
}
