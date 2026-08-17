package ui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/systheme"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/update"
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
	cfg    config.Config
	store  *store.Store
	tmux   *tmux.Driver
	hooks  *hooks.Manager
	gitDrv *git.Driver
	engine *status.Engine

	// setSnapshot writes a session's pane capture before archive or kill
	// takes the window; a seam so snapshot failures can be exercised
	// without a broken store.
	setSnapshot func(id, snapshot string) error

	sessions []store.Session
	rows     []treeRow

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

	net netStats

	poller *poller
	focus  *focusWatch
	// sel is the focused-pane selection, written during paint so clicks
	// resolve against the current frame. copied is the size of the last
	// clipboard write, shown once in the status line and cleared on the
	// next selection.
	copied int
	sel    focusSelection
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
	pane    paneMirror
	// cursorOn is the caret's blink phase while focused.
	cursorOn bool
	// focusScroll is how many lines the focused pane is scrolled back into
	// its history; zero is live at the bottom.
	focusScroll int
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
	// watchedGen is previewGen as of the last poll pass, so a selection
	// that has not moved since can be recognised as at rest.
	watchedGen        uint64
	previewBodyOffset int
	cursor            int
	mode              mode
	showArchived      bool
	hideEmptyGroups   bool
	statusFilter      statusFilter
	collapsed         map[string]bool
	search            string
	searching         bool

	diff       diffState
	form       form
	groupForm  groupForm
	pathSugg   pathComplete
	confirm    confirmTarget
	launchHint string
	rename     renameTarget
	fork       forkState
	quick      quickState
	settings   settingsState
	help       helpState
	moveID     string
	movePath   string
	repoPick   repoPickState
	// editorReturnID is the session an editor request detached from, so the
	// attach it cost can be resumed once the editor is up.
	editorReturnID string

	// Repo a human picked by hand per session, outranking the agent's
	// declaration for as long as this manager runs.
	pickedRepos map[string]string

	// awaitedRenames holds the generated name a spawned session launched
	// under, for as long as the agent it carries the rename directive to is
	// still expected to answer. A rename that has not landed by the time
	// this manager run ends is one that is never arriving, so the set is
	// deliberately not persisted.
	awaitedRenames map[string]string

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
	// terminalKeyAt is when the last T finished being handled. Held down it
	// autorepeats into a burst of keystrokes, and T is the only key that
	// spawns on the keystroke itself rather than opening a form that would
	// swallow them.
	terminalKeyAt time.Time

	// bannerPhase advances the wordmark's current sweep and then rests, so
	// the frame is not repainted forever.
	bannerPhase int

	// startupPhase advances the preview's launch loader. It rides the
	// preview tick, which already runs at its fast cadence while a session
	// is starting, rather than adding a timer of its own.
	startupPhase int

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
}

type netStats struct {
	up       uint64
	down     uint64
	rates    bool
	prevSent uint64
	prevRecv uint64
	prevAt   time.Time
	prevOK   bool
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
}

type errBar struct {
	text  string
	done  string
	shown string
	age   int
}

// worked reports whether the message on the bar is an action that went
// through, so it can be styled as an outcome rather than a failure. Only
// reportDone fills done, and any later write to text alone leaves it
// behind, so a message says it worked or reads as a failure.
func (e errBar) worked() bool { return e.text != "" && e.text == e.done }

// reportDone puts an action that went through on the status bar.
func (m *Model) reportDone(text string) {
	m.errBar.text, m.errBar.done = text, text
}

// splitState is the horizontal sessions/sidebar split. ratio is the left
// panel's share of the terminal width; resizeMode arms keyboard divider
// nudging.
type splitState struct {
	ratio       float64
	ratioBefore float64
	resizeMode  bool
	dragging    bool
}

// updateInfo is this build's release tag plus a newer release found on
// GitHub, so the header can badge it. applying marks an in-flight
// self-update; restartPath, once set, tells main to exec the freshly
// swapped binary after the program exits.
type updateInfo struct {
	version        string
	latest         string
	url            string
	releases       []update.Release
	available      update.ReleaseRange
	installed      update.ReleaseRange
	checked        bool
	refreshing     bool
	refreshPending int
	applying       bool
	restartPath    string
}

// updateAppliedMsg reports the self-update download-and-swap: on success
// path is the binary to restart into.
type updateAppliedMsg struct {
	path     string
	result   update.Result
	upToDate bool
	err      error
}

// confirmTarget.action values; the zero value means delete.
const (
	actionDelete  = ""
	actionArchive = "archive"
	actionRestore = "restore"
	actionKill    = "kill"
	actionRestart = "restart"
	actionRevive  = "revive"
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
	isGroup       bool
	path          string
	sessID        string
	input         textinput.Model
	dir           textinput.Model
	worktreeIndex int
	focus         int
	toolNames     []string
	toolIndex     int
}

// quickState is the inline prompt bar docked under the preview: active
// across cursor moves, so the target follows the selection. The tool is
// the spawn CLI for group targets, cycled with tab. A pasted image lands
// at the caret as an "[Image #N]" token that renders as a chip and steps,
// deletes, and wraps as one unit; on submit each token becomes its path.
type quickState struct {
	active bool
	composer
	toolNames      []string
	toolIndex      int
	closeAfterSend bool
	worktree       bool
	// worktreeTouched marks an explicit toggle this run; until then the
	// hint and spawn follow the target group's default.
	worktreeTouched bool
}

// repoAnswer is one directory's git-repo verdict and when it was taken.
type repoAnswer struct {
	capable bool
	at      time.Time
}

type settingsState struct {
	toolNames       []string
	toolIndex       int
	themeIndex      int
	field           int
	layoutSplit     bool
	quickCloseSend  bool
	enterFocuses    bool
	arrowStep       bool
	comfortableRows bool
	worktreeDefault bool
	notifications   bool
	notifyFinished  bool
	themeAuto       bool
	// manualTheme is the persisted choice the theme key keeps while
	// auto-detect drives the live palette, so turning auto off returns
	// to it.
	manualTheme string
	// cliPicker is the sub-panel for which CLIs appear when creating sessions.
	cliPicker bool
	cliNames  []string
	cliHidden map[string]bool
	cliCursor int
}

const (
	settingsFieldTool = iota
	settingsFieldTheme
	settingsFieldThemeAuto
	settingsFieldDensity
	settingsFieldLayout
	settingsFieldQuickClose
	settingsFieldFocusKey
	settingsFieldArrowStep
	settingsFieldWorktree
	settingsFieldNotify
	settingsFieldNotifyFinish
	settingsFieldCLIs
	settingsFieldBugReport
	settingsFieldFeatureRequest
	settingsFieldUpdate
	settingsFieldCount
)

// agentStats aggregates process-tree usage across all live sessions.
// cpu and ram are shares of this machine (0–100); rss is absolute bytes.
type agentStats struct {
	count int
	cpu   float64
	ram   float64
	rss   uint64
}

type refreshMsg struct {
	sessions       []store.Session
	groups         []string
	groupPaths     map[string]string
	groupWorktrees map[string]string
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
	// gen is the previewGen the capture was scheduled for; mismatched
	// gens are discarded so a hold-j burst cannot paint a stale session.
	gen uint64
}

// previewSettleMsg fires after the cursor has stopped moving so one
// capture runs instead of one per key-repeat tick.
type previewSettleMsg struct {
	gen uint64
}

// previewSettle is how long we wait after the last cursor move before
// talking to tmux. Short enough to feel instant, long enough to collapse
// a held j/k burst into a single capture.
const previewSettle = 50 * time.Millisecond

// The selected session's pane is re-captured on its own timer. The full
// poll is deliberately slow (it lists panes, samples every process tree and
// writes the store), which left the preview refreshing on the poll cadence
// and reading as a still image of a live agent. One capture of one pane is
// cheap, but it is still a tmux exec, so the rate follows the session: an
// agent that is producing output earns a fast cadence, one that is waiting
// on a human does not.
const (
	previewIntervalLive = 300 * time.Millisecond
	previewIntervalCalm = 1200 * time.Millisecond
	updateTickInterval  = 10 * time.Minute
)

// cursorBlinkMsg toggles the focused pane's caret.
type cursorBlinkMsg struct{}

// cursorBlinkInterval is the caret's half period, matching the rate most
// terminals blink their own.
const cursorBlinkInterval = 530 * time.Millisecond

// cursorBlink re-arms the caret timer. It runs only while a session is
// focused; every other mode lets the timer die.
func (m *Model) cursorBlink() tea.Cmd {
	return tea.Tick(cursorBlinkInterval, func(time.Time) tea.Msg { return cursorBlinkMsg{} })
}

// previewTickMsg drives that timer.
type previewTickMsg struct{}

// previewTick re-arms the preview timer at the cadence the selection earns.
func (m *Model) previewTick() tea.Cmd {
	interval := previewIntervalCalm
	if sess, ok := m.selected(); ok {
		switch sess.Status {
		case status.Working, status.Starting:
			interval = previewIntervalLive
		}
	}
	return tea.Tick(interval, func(time.Time) tea.Msg { return previewTickMsg{} })
}

type errMsg struct{ err error }

type attachDoneMsg struct {
	sessID string
	err    error
}

func New(cfg config.Config, st *store.Store, driver *tmux.Driver, engine *status.Engine, hookManager *hooks.Manager, version string) *Model {
	statusSources := make(map[string]string, len(cfg.Tools))
	sessionStores := make(map[string]string, len(cfg.Tools))
	for name, tool := range cfg.Tools {
		statusSources[name] = tool.StatusSource
		sessionStores[name] = tool.SessionStore
	}
	// A missing git binary only disables the diff view; everything else
	// works without it, so the error surfaces on first use instead.
	gitDriver, _ := git.New()
	applyTheme(themes[themeIndex(resolveStartupTheme(st))])
	model := &Model{
		cfg:                 cfg,
		store:               st,
		tmux:                driver,
		hooks:               hookManager,
		gitDrv:              gitDriver,
		engine:              engine,
		setSnapshot:         st.SetSnapshot,
		poller:              newPoller(st, driver, engine, hookManager, gitDriver, statusSources, sessionStores, cfg.PollInterval.Duration),
		collapsed:           loadCollapsed(st),
		split:               splitState{ratio: loadSplitRatio(st)},
		focusOnEnter:        storedFocusOnEnter(st),
		arrowStep:           storedArrowStep(st),
		comfortableRows:     storedComfortableRows(st),
		mode:                modeList,
		update:              updateInfo{version: version},
		dismissed:           loadDismissed(st),
		whatsNewVersion:     loadWhatsNewVersion(st),
		whatsNewFromVersion: loadWhatsNewFromVersion(st),
	}
	if dir, err := config.Dir(); err == nil {
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

// storedTheme reads the persisted theme name. A read failure falls back to
// the default theme: the UI still paints, just not in the chosen palette.
func storedTheme(st *store.Store) string {
	name, err := st.Setting(themeSetting)
	if err != nil {
		return ""
	}
	return name
}

func themeAutoEnabled(st *store.Store) bool {
	value, err := st.Setting(themeAutoSetting)
	return err == nil && value == "on"
}

// resolveStartupTheme picks the boot theme: the stored choice, unless
// auto-detect is on and the environment's scheme disagrees with that
// choice's polarity — then the default theme of the detected side takes
// over, and an undetectable scheme changes nothing.
func resolveStartupTheme(st *store.Store) string {
	stored := storedTheme(st)
	if !themeAutoEnabled(st) {
		return stored
	}
	return autoThemeName(stored, systheme.Detect())
}

// autoThemeName keeps the stored theme whenever it already sits on the
// detected side, so auto-detect corrects polarity without discarding a
// palette the user picked.
func autoThemeName(stored string, scheme systheme.Scheme) string {
	if scheme == systheme.SchemeUnknown {
		return stored
	}
	wantLight := scheme == systheme.SchemeLight
	if themes[themeIndex(stored)].lightBackdrop() == wantLight {
		return stored
	}
	if wantLight {
		return "solarized light"
	}
	return "classic"
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
		m.errBar.text = err.Error()
		return
	}
	if err := m.store.SetSetting(collapsedSetting, string(raw)); err != nil {
		m.errBar.text = err.Error()
	}
}

// StartPoller launches the background polling loop. It runs outside the
// bubbletea event loop so statuses keep updating while the TUI is
// suspended inside a tmux attach.
func (m *Model) StartPoller(send func(tea.Msg)) {
	m.focus = newFocusWatch(m.tmux, send)
	m.syncPollInput()
	go m.poller.run(send)
}

func (m *Model) syncPollInput() {
	selectedID := ""
	focusID := ""
	if sess, ok := m.selected(); ok {
		selectedID = sess.ID
		if !sess.Archived {
			focusID = sess.ID
		}
	}
	m.poller.setInput(m.showArchived, selectedID)
	// Only ever stop the watcher here. Opening a control client costs a
	// process and a tmux attach, so holding j through twenty rows would
	// pay that twenty times; the client is opened once the cursor settles
	// instead. The watcher only exists once StartPoller has a send
	// function; tests drive Update without one.
	if m.focus != nil && m.focus.watching() != focusID {
		m.focus.setFocus("")
	}
}

// watchSelection points the control client at the current selection. Call
// it where the selection has come to rest, never on every cursor move.
func (m *Model) watchSelection() {
	if m.focus == nil {
		return
	}
	sess, ok := m.selected()
	if !ok || sess.Archived {
		m.focus.setFocus("")
		return
	}
	m.focus.setFocus(sess.ID)
}

// requestRefresh publishes the current UI state to the poller and asks
// for an immediate pass.
func (m *Model) requestRefresh() {
	m.syncPollInput()
	m.poller.requestRefresh()
}

func (m *Model) Init() tea.Cmd {
	m.syncPollInput()
	return tea.Batch(m.syncPaneTheme(), m.refreshExistingSessionUX, m.checkForUpdate, m.checkFeed, m.updateTick(), m.bannerTick(), m.previewTick(), m.sweepPastes, m.pasteSweepTick())
}

// updateMsg carries the result of a GitHub release check. A failed check may
// still carry a stale catalog; an empty failure leaves the screen unchanged.
type updateMsg struct {
	latest   string
	url      string
	releases []update.Release
	failed   bool
	manual   bool
	err      error
}

// feedMsg carries the editorial message feed and any refresh failure.
type feedMsg struct {
	messages []feed.Message
	failed   bool
	manual   bool
	err      error
}

// pasteSweepMsg carries the result of one pass over the pastes directory.
type pasteSweepMsg struct{ err error }

type pasteSweepTickMsg struct{}

// pasteSweepInterval keeps clearing old pasted images while the manager
// stays open. A manager left running for weeks would otherwise sweep once
// at startup and then let screenshots pile up in temp until the next
// restart.
const pasteSweepInterval = 24 * time.Hour

// sweepStalePastes is the seam tests swap for a fake sweep.
var sweepStalePastes = func() error { return clipboard.SweepStale(clipboard.StaleAfter) }

// sweepPastes clears images pasted long enough ago that no agent will open
// them again. It runs off the event loop: the pastes directory lives in
// temp, where a slow disk must not stall a keystroke.
func (m *Model) sweepPastes() tea.Msg {
	return pasteSweepMsg{err: sweepStalePastes()}
}

func (m *Model) pasteSweepTick() tea.Cmd {
	return tea.Tick(pasteSweepInterval, func(time.Time) tea.Msg { return pasteSweepTickMsg{} })
}

type updateTickMsg struct{}

// updateTick re-runs the release check while the manager stays open for
// days at a time. update.Check serves most ticks from its on-disk cache,
// so this only reaches GitHub once the cache goes stale.
func (m *Model) updateTick() tea.Cmd {
	return tea.Tick(updateTickInterval, func(time.Time) tea.Msg { return updateTickMsg{} })
}

// checkForUpdate hits GitHub Releases (throttled by update.Check via an
// on-disk cache) off the event loop and reports a newer release. Any
// failure resolves to a failed message so the TUI simply shows no badge.
func (m *Model) checkForUpdate() tea.Msg {
	return m.fetchUpdates(false)
}

func (m *Model) refreshUpdates() tea.Msg {
	return m.fetchUpdates(true)
}

func (m *Model) fetchUpdates(force bool) tea.Msg {
	dir, err := config.Dir()
	if err != nil {
		return updateMsg{failed: true, manual: force, err: err}
	}
	var result update.Result
	if force {
		result, err = update.Refresh(context.Background(), dir, m.update.version)
	} else {
		result, err = update.Check(context.Background(), dir, m.update.version)
	}
	if err != nil {
		return updateMsg{
			latest: result.Latest, url: result.URL, releases: result.Releases,
			failed: true, manual: force, err: err,
		}
	}
	return updateMsg{latest: result.Latest, url: result.URL, releases: result.Releases, manual: force}
}

// checkFeed pulls the remote message feed off the event loop. Like the
// release check it is cache-backed, so most ticks cost one disk read.
func (m *Model) checkFeed() tea.Msg {
	return m.fetchFeed(false)
}

func (m *Model) refreshFeed() tea.Msg {
	return m.fetchFeed(true)
}

func (m *Model) fetchFeed(force bool) tea.Msg {
	dir, err := config.Dir()
	if err != nil {
		return feedMsg{failed: true, manual: force, err: err}
	}
	var messages []feed.Message
	if force {
		messages, err = feed.Refresh(context.Background(), dir, m.update.version)
	} else {
		messages, err = feed.Fetch(context.Background(), dir, m.update.version)
	}
	if err != nil {
		return feedMsg{messages: messages, failed: true, manual: force, err: err}
	}
	return feedMsg{messages: messages, manual: force}
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
// m.sessions still carries the other scope's list. Status filters apply
// later via listedSessions.
func (m *Model) visibleSessions() []store.Session {
	visible := make([]store.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		if sess.Archived == m.showArchived {
			visible = append(visible, sess)
		}
	}
	return visible
}

// listedSessions is the archived scope narrowed by the status filter.
// Header counts, group rollups, and the tree all share this set so the
// numbers always match what the list can show.
//
// The selected session stays listed when its status leaves the filter
// (finished → idle on enter/ack) so rebuild cannot eject the cursor mid-work.
func (m *Model) listedSessions() []store.Session {
	visible := m.visibleSessions()
	if !m.statusFilter.active() {
		return visible
	}
	heldID := ""
	if sess, ok := m.selected(); ok {
		heldID = sess.ID
	}
	listed := make([]store.Session, 0, len(visible))
	for _, sess := range visible {
		if m.statusFilter.matches(sess.Status) || sess.ID == heldID {
			listed = append(listed, sess)
		}
	}
	return listed
}

// listedAgents is listedSessions without the shells, for the rollups that
// describe what a group is working on.
func (m *Model) listedAgents() []store.Session {
	listed := m.listedSessions()
	agents := make([]store.Session, 0, len(listed))
	for _, sess := range listed {
		if !m.isShell(sess.Tool) {
			agents = append(agents, sess)
		}
	}
	return agents
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

// focusSession puts the cursor on a session's row, for the keys that make
// one and leave the user on it. A session filtered out of the current view
// has no row, and the cursor stays where it was.
func (m *Model) focusSession(id string) {
	for i, row := range m.rows {
		if !row.isGroup && row.sess.ID == id {
			m.cursor = i
			return
		}
	}
}

// schedulePreview arms a single capture after previewSettle. Call after
// bumping previewGen so earlier timers and in-flight captures go stale.
func (m *Model) schedulePreview() tea.Cmd {
	gen := m.previewGen
	return tea.Tick(previewSettle, func(time.Time) tea.Msg {
		return previewSettleMsg{gen: gen}
	})
}

// previewCmd captures one session's pane and process stats off the
// render loop. gen tags the result so a newer cursor move can discard it.
// Size pins stay on resizeSessions / create / attach — not on every look —
// so a settle capture is one capture-pane, not resize+capture+pid storms.
func (m *Model) previewCmd(sess store.Session, gen uint64) tea.Cmd {
	return func() tea.Msg {
		msg := previewMsg{sessID: sess.ID, gen: gen}
		if sess.Archived || !m.tmux.Exists(sess.ID) {
			snapshot, err := storedPreview(m.store, m.tmux, sess.ID)
			if err != nil {
				return errMsg{err}
			}
			msg.preview = snapshot
			return msg
		}
		if pane, err := m.tmux.CapturePane(sess.ID); err == nil {
			msg.preview = pane
		}
		if pid, err := m.tmux.PanePID(sess.ID); err == nil {
			memTotal, _ := sysstat.MemTotalBytes()
			msg.proc = sysstat.Trees([]int{pid})[pid].ScaleToHost(sysstat.LogicalCPUs(), memTotal)
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
// panel's pixel box so a capture fills the preview 1:1. No-op when the
// size already matches; otherwise resizes in parallel so a fleet of
// sessions does not serialize N tmux round-trips on the UI path.
func (m *Model) resizeSessions() {
	width, height := m.previewPaneWidth(), m.previewPaneHeight()
	if width <= 0 || height <= 0 {
		return
	}
	if m.pane.geom == nil {
		m.pane.geom = map[string][2]int{}
	}
	var todo []string
	for _, sess := range m.sessions {
		if sess.Archived {
			continue
		}
		if last, ok := m.pane.geom[sess.ID]; ok && last[0] == width && last[1] == height {
			continue
		}
		todo = append(todo, sess.ID)
	}
	if len(todo) == 0 {
		return
	}
	type result struct {
		id  string
		err error
	}
	// Pause polling for the whole clear+resize window so a mid-reflow
	// capture cannot compare against a pre-resize hash.
	m.poller.reflowSessions(todo, func() {
		results := make(chan result, len(todo))
		for _, id := range todo {
			go func(id string) {
				results <- result{id: id, err: m.tmux.Resize(id, width, height)}
			}(id)
		}
		for range todo {
			r := <-results
			if r.err == nil {
				m.pane.geom[r.id] = [2]int{width, height}
			}
		}
	})
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
		// Re-assert the terminal backdrop: a reattach or a fresh outer
		// terminal delivers a size message and may carry stale colors.
		SyncTerminalBackground()
		// Geometry cache is stale for every session after a real resize.
		m.pane.geom = nil
		m.resizeSessions()
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

	case previewTickMsg:
		m.startupPhase++
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
		m.ageError()
		// The focused session can die or vanish under us; fall back to the
		// list rather than typing into nothing.
		var focusExit tea.Cmd
		if m.mode == modeFocus {
			if sess, ok := m.selected(); !ok || sessionGone(msg.sessions, sess.ID) {
				focusExit = m.leaveFocus()
			}
		}
		m.sessions = msg.sessions
		m.groups = msg.groups
		m.groupPaths = msg.groupPaths
		m.groupWorktrees = msg.groupWorktrees
		m.archivedGroups = msg.archivedGroups
		m.agents = msg.agents
		if msg.snapOK {
			m.snap = msg.snap
			m.updateNetRates(msg.snap)
		}
		if !m.sessionsSized && m.width > 0 && len(m.sessions) > 0 {
			m.sessionsSized = true
			// Sessions left from a previous run carry that run's window
			// size, which our cache knows nothing about, so the first pass
			// re-asserts geometry for every one of them.
			m.pane.geom = nil
		}
		// The preview box changes height for more reasons than a terminal
		// resize: the quick bar opening, the status line appearing, a new
		// badge in the header. A pane left at the old height paints a dead
		// band under its output, so every pass re-asserts the geometry.
		// The call is free when nothing moved: it diffs against paneGeom.
		if m.sessionsSized && m.width > 0 {
			m.resizeSessions()
		}
		m.rebuildRows()
		// A pass that ran with a stale selection (a session created this
		// tick) carries the wrong preview; resync and fetch it directly.
		if sess, ok := m.selected(); ok && sess.ID != msg.procFor {
			m.syncPollInput()
			m.previewGen++
			return m, tea.Batch(focusExit, m.previewCmd(sess, m.previewGen), m.diffRefreshCmd())
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
		return m, tea.Batch(focusExit, m.diffRefreshCmd())

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
		m.keepNoticeSelection(func() {
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
			m.keepNoticeSelection(func() { m.feedMessages = msg.messages })
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

	case focusCopiedMsg:
		m.errBar.text = ""
		m.copied = msg.chars
		return m, nil

	case focusScrollMsg:
		sess, ok := m.selected()
		if !ok || sess.ID != msg.sessID {
			return m, nil
		}
		if msg.offset != m.focusScroll || msg.rows != m.previewPaneHeight() {
			// The wheel or a resize moved the target while this capture was
			// in flight. Fetch just that final viewport.
			return m, m.focusRegionCmd(sess.ID, m.focusScroll)
		}
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
		// back down, and holding it would freeze the view for good.
		if m.pane.mouse && m.focusScroll != 0 {
			m.focusScroll = 0
		}
		// A scrolled-back pane holds still: live frames would yank the
		// view back to the bottom mid-read.
		if m.scrolledBack() {
			return m, nil
		}
		m.preview = msg.preview
		m.pane.cursor = paneCursor{x: msg.cursorX, y: msg.cursorY, ok: msg.cursorOK}
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
		// keeps that size on detach; shrink it back to the preview panel so
		// the capture is not clipped on the right.
		if m.pane.geom != nil {
			delete(m.pane.geom, msg.sessID)
		}
		width, height := m.previewPaneWidth(), m.previewPaneHeight()
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
			m.reportDone("opened " + msg.dir + " in " + msg.name)
		}
		if id := m.editorReturnID; id != "" {
			m.editorReturnID = ""
			return m, tea.Batch(resume, m.reattach(id, m.diff.gen))
		}
		return m, resume

	case reattachPreparedMsg:
		if msg.diffGen != m.diff.gen || m.diff.active {
			return m, nil
		}
		if msg.err != nil {
			m.errBar.text = msg.err.Error()
			return m, nil
		}
		m.errBar.text = msg.warn
		return m, tea.ExecProcess(m.tmux.AttachCommand(msg.sessID), func(err error) tea.Msg {
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

// setPreview stores a polled or ticked pane capture, unless the session's
// control client is already pushing frames. Those two paths sample at
// different times, and a capture taken a second ago repainting over a
// pushed one is what makes typed characters blink in and out.
func (m *Model) setPreview(sessID, preview string) {
	if sessID != "" && m.focus != nil && m.focus.serving(sessID) {
		return
	}
	// A scrolled-back pane holds still on this path too: without a control
	// client the poll is the only source of frames, and a live bottom
	// landing mid-read is the same yank the pushed frames are held back
	// from.
	if m.scrolledBack() {
		return
	}
	m.preview = preview
}

// sessionGone reports whether id is absent from a refresh's session list.
func sessionGone(sessions []store.Session, id string) bool {
	for _, sess := range sessions {
		if sess.ID == id {
			return false
		}
	}
	return true
}

// updateNetRates diffs cumulative interface counters between polls into
// bytes-per-second rates. Counters can reset (sleep, interface changes),
// so a backwards jump just reseeds the baseline.
func (m *Model) updateNetRates(snap sysstat.Snapshot) {
	now := time.Now()
	if m.net.prevOK && snap.NetOK &&
		snap.NetSent >= m.net.prevSent && snap.NetRecv >= m.net.prevRecv {
		if dt := now.Sub(m.net.prevAt).Seconds(); dt > 0 {
			m.net.up = uint64(float64(snap.NetSent-m.net.prevSent) / dt)
			m.net.down = uint64(float64(snap.NetRecv-m.net.prevRecv) / dt)
			m.net.rates = true
		}
	}
	m.net.prevSent = snap.NetSent
	m.net.prevRecv = snap.NetRecv
	m.net.prevAt = now
	m.net.prevOK = snap.NetOK
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

// rootGroup is the path every ungrouped session already carries.
const rootGroup = ""

// isRoot marks the pinned top-level row, which is not a stored group.
func (e treeRow) isRoot() bool { return e.isGroup && e.group == rootGroup }

// rowsBelowRoot is the tree without its pinned row: empty means the rail
// has nothing to list, however many rows it paints.
func rowsBelowRoot(rows []treeRow) []treeRow {
	if len(rows) > 0 && rows[0].isRoot() {
		return rows[1:]
	}
	return rows
}

func rowKey(entry treeRow) string {
	if entry.isGroup {
		return "g:" + entry.group
	}
	return "s:" + entry.sess.ID
}

// rebuildRows walks the group tree depth-first and emits one row per
// group node and per session, honoring collapse state, search, and the
// status filter. The cursor follows the previously selected row's
// identity, so list changes from the 2s poll never yank the selection.
func (m *Model) rebuildRows() {
	previousKey := ""
	if entry, ok := m.selectedRow(); ok {
		previousKey = rowKey(entry)
	}
	query := strings.ToLower(strings.TrimSpace(m.search))
	prunedView := query != "" || m.statusFilter.active()

	listed := m.listedSessions()
	listedIDs := make(map[string]bool, len(listed))
	for _, sess := range listed {
		listedIDs[sess.ID] = true
	}
	byID := make(map[string]store.Session, len(m.sessions))
	for _, sess := range m.sessions {
		byID[sess.ID] = sess
	}
	// m.sessions arrives ordered by the store (group, sort_order), so
	// per-group slices inherit the user's manual order.
	matched := make(map[string]bool, len(listed))
	for _, sess := range listed {
		if query == "" || matchesSearch(sess, query) {
			matched[sess.ID] = true
		}
	}
	// A parent the search itself missed still comes along to carry its
	// matching children, in the store's order rather than after them.
	carried := map[string]bool{}
	for _, sess := range listed {
		if !matched[sess.ID] || sess.ParentID == "" || !listedIDs[sess.ParentID] {
			continue
		}
		carried[sess.ParentID] = true
	}
	sessionsByGroup := map[string][]store.Session{}
	childrenByParent := map[string][]store.Session{}
	for _, sess := range listed {
		if sess.ParentID != "" {
			if _, ok := byID[sess.ParentID]; ok {
				if matched[sess.ID] {
					childrenByParent[sess.ParentID] = append(childrenByParent[sess.ParentID], sess)
				}
				continue
			}
		}
		if matched[sess.ID] || carried[sess.ID] {
			sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
		}
	}
	walked := map[string]bool{}
	for _, groupSessions := range sessionsByGroup {
		for _, sess := range groupSessions {
			walked[sess.ID] = true
		}
	}
	orphaned := map[string]bool{}
	// A child whose parent never made it into a group paints un-nested, in
	// the store's order rather than the order the parent map happens to
	// yield.
	for _, sess := range listed {
		if _, nested := childrenByParent[sess.ParentID]; !nested || walked[sess.ParentID] {
			continue
		}
		if !matched[sess.ID] {
			continue
		}
		sessionsByGroup[sess.Group] = append(sessionsByGroup[sess.Group], sess)
		orphaned[sess.ParentID] = true
	}
	for parentID := range orphaned {
		delete(childrenByParent, parentID)
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
		if prunedView {
			paths = pathsWithSessions(paths, sessionsByGroup)
		}
	}
	if m.hideEmptyGroups {
		// This is a presentation filter only: stored groups remain available
		// to forms and return to the tree as soon as the toggle is switched
		// off. Ancestors of groups with visible sessions stay in the tree.
		paths = pathsWithSessions(paths, sessionsByGroup)
	}
	children := childIndex(paths, m.groups)

	// Folds are a browsing convenience for the active tree; the archived,
	// search, and status-filter views already prune to matching groups, so
	// honoring folds there would hide the very sessions the user came for.
	honorFolds := !prunedView && !m.showArchived

	// Root is a standing move and spawn target; its sessions stay flat.
	rows := make([]treeRow, 0, len(m.sessions)+len(paths)+1)
	appendSession := func(sess store.Session, depth int) {
		rows = append(rows, treeRow{sess: sess, depth: depth})
		for _, child := range childrenByParent[sess.ID] {
			rows = append(rows, treeRow{sess: child, depth: depth + 1})
		}
	}
	rows = append(rows, treeRow{isGroup: true, group: rootGroup})
	for _, sess := range sessionsByGroup[""] {
		appendSession(sess, 0)
	}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		rows = append(rows, treeRow{isGroup: true, group: path, depth: depth})
		if honorFolds && m.collapsed[path] {
			return
		}
		for _, sess := range sessionsByGroup[path] {
			appendSession(sess, depth+1)
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
	} else if m.cursor == 0 && len(rows) > 1 && rows[0].isRoot() {
		// A launch opens on a session, not on root's rollup.
		m.cursor = 1
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
