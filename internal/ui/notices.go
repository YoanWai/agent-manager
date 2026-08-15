package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/update"
)

const (
	noticeWelcome = "welcome"
	// noticeArrowStep introduces the beta ←→ pair; it ships in the binary
	// and stays listed until dismissed, like the welcome.
	noticeArrowStep = "arrow-step-beta"

	dismissedNoticesSetting = "dismissed_notices"
	lastSeenVersionSetting  = "last_seen_version"
	whatsNewVersionSetting  = "whats_new_version"
	whatsNewFromSetting     = "whats_new_from_version"

	repoURL = "https://github.com/YoanWai/agent-manager"
)

// notice is one dismissible message shown in the rail's messages panel
// and readable in full from the notices modal. url is what enter opens;
// glyph and tint are its mark in both places.
type notice struct {
	id            string
	glyph         string
	tint          lipgloss.Color
	title         string
	body          []string
	releases      []update.Release
	after         []string
	rangeComplete bool
	url           string
}

func (n notice) mark() string {
	return lipgloss.NewStyle().Foreground(n.tint).Render(n.glyph)
}

// indexReleaseRanges keeps catalog scans out of the paint path. It runs only
// when startup state or a background GitHub result changes.
func (m *Model) indexReleaseRanges() {
	m.update.available = update.Between(m.update.releases, m.update.version, m.update.latest)
	m.update.installed = update.Between(m.update.releases, m.whatsNewFromVersion, m.update.version)
}

func releaseCountLabel(releaseRange update.ReleaseRange) string {
	count := fmt.Sprint(len(releaseRange.Releases))
	if !releaseRange.Complete {
		count += "+"
	}
	return count
}

func (m *Model) activeNotices() []notice {
	if m.store == nil {
		return nil
	}
	var notices []notice
	if m.update.latest != "" {
		releaseRange := m.update.available
		title := m.update.latest + " available"
		if len(releaseRange.Releases) > 1 {
			title = releaseCountLabel(releaseRange) + " releases available · " + m.update.latest
		}
		body := []string{"You are on " + m.update.version + ". Here is everything released since then:"}
		if len(releaseRange.Releases) == 0 {
			body = append(body, "The generated change summary is not available yet; r refreshes it now.")
		}
		notices = append(notices, notice{
			id:            "update-" + m.update.latest,
			glyph:         "↑",
			tint:          colorAccent,
			title:         title,
			body:          body,
			releases:      releaseRange.Releases,
			rangeComplete: releaseRange.Complete,
			after: []string{
				"u updates once to " + m.update.latest + " and restarts; every release above is included.",
				"Enter opens the full release notes.",
			},
			url: m.update.url,
		})
	}
	if m.whatsNewVersion == m.update.version {
		releaseRange := m.update.installed
		title := "Updated to " + m.update.version
		if len(releaseRange.Releases) > 1 {
			title = "Updated across " + releaseCountLabel(releaseRange) + " releases · " + m.update.version
		}
		body := []string{"Updated from " + m.whatsNewFromVersion + " to " + m.update.version + "."}
		if len(releaseRange.Releases) == 0 && !m.update.checked {
			body = append(body, "Loading the change summary from GitHub…")
		} else if len(releaseRange.Releases) == 0 {
			body = append(body, "No generated change summary was found; r refreshes GitHub now.")
		}
		notices = append(notices, notice{
			id:       "whatsnew-" + m.update.version,
			glyph:    "✦",
			tint:     colorAccent2,
			title:    title,
			body:     body,
			releases: releaseRange.Releases,
			after: []string{
				"Enter opens the full release notes.",
			},
			rangeComplete: releaseRange.Complete,
			url:           repoURL + "/releases/tag/v" + strings.TrimPrefix(m.update.version, "v"),
		})
	}
	for _, msg := range m.feedMessages {
		notices = append(notices, notice{
			id:    msg.ID,
			glyph: "◆",
			tint:  colorAccent2,
			title: msg.Title,
			body:  msg.Body,
			url:   msg.URL,
		})
	}
	notices = append(notices,
		notice{
			id:    noticeWelcome,
			glyph: "✳",
			tint:  colorAccent2,
			title: "Welcome to agent-manager",
			body: []string{
				"Every row on the left is a live agent session.",
				"",
				"n      new session           space  prompt it, no attach",
				"↵      focus it              A      attach it full screen",
				"ctrl+q back to the manager   ctrl+r review its diff",
				"x / v  kill / revive         s      settings",
				"",
				"Press ? for every key: the map scrolls, and / searches it.",
				"A bug or an idea? Settings (s) has a row for each.",
				"Messages like this one live here; x dismisses one for good.",
			},
			url: repoURL + "#readme",
		},
		notice{
			id:    noticeArrowStep,
			glyph: "↔",
			tint:  lipgloss.Color("#e2c044"),
			title: "New in beta: step in and out with ← →",
			body: []string{
				"→ on a list row steps in: it focuses the session, or opens the group.",
				"← steps out: it closes the group, and inside a focused session it",
				"returns here - only while the caret sits at the start of the agent's",
				"prompt, where ← would do nothing. Anywhere else in the prompt it",
				"moves the caret as always.",
				"",
				"We are testing this. If a ← ever lands somewhere you did not expect,",
				"Enter here opens a prefilled report, and Settings (s) has a",
				"\"←→ step in/out\" row that turns the pair off.",
			},
			url: arrowStepFeedbackURL(m.update.version),
		},
	)

	kept := notices[:0]
	for _, n := range notices {
		if !m.dismissed[n.id] {
			kept = append(kept, n)
		}
	}
	return kept
}

func bugReportURL(version string) string {
	q := url.Values{}
	q.Set("template", "bug_report.yml")
	if version != "" {
		q.Set("version", version)
	}
	return repoURL + "/issues/new?" + q.Encode()
}

func featureRequestURL() string {
	return repoURL + "/issues/new?template=feature_request.yml"
}

// arrowStepFeedbackURL prefills a report scoped to the beta ←→ pair, so
// feedback on it arrives labeled without the reporter typing the context.
func arrowStepFeedbackURL(version string) string {
	body := fmt.Sprintf("**Version:** %s\n**OS:** %s/%s\n\n**Area**\n←→ step in/out (beta)\n\n**What happened:**\n", version, runtime.GOOS, runtime.GOARCH)
	return repoURL + "/issues/new?body=" + url.QueryEscape(body)
}

// startupNotice decides what greets this launch: the welcome notice the
// first time the manager ever runs, the what's-new notice the first run
// after an update, nothing otherwise. It advances the stored version as a
// side effect, so each greeting fires exactly once; the notice itself
// stays listed until dismissed.
func (m *Model) startupNotice() string {
	seen, err := m.store.Setting(lastSeenVersionSetting)
	if err != nil {
		return ""
	}
	if seen == m.update.version {
		return ""
	}
	if seen == "" {
		if err := m.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
			m.errBar.text = err.Error()
			return ""
		}
		return noticeWelcome
	}
	if !update.Newer(m.update.version, seen) {
		if err := m.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
			m.errBar.text = err.Error()
		}
		return ""
	}
	if err := m.store.SetSetting(whatsNewFromSetting, seen); err != nil {
		m.errBar.text = err.Error()
		return ""
	}
	if err := m.store.SetSetting(whatsNewVersionSetting, m.update.version); err != nil {
		m.errBar.text = err.Error()
		return ""
	}
	if err := m.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
		m.errBar.text = err.Error()
		return ""
	}
	m.whatsNewFromVersion = seen
	m.whatsNewVersion = m.update.version
	m.indexReleaseRanges()
	return "whatsnew-" + m.update.version
}

// loadWhatsNewVersion restores the version whose what's-new notice is
// still current. A read failure just means no notice.
func loadWhatsNewVersion(st *store.Store) string {
	version, err := st.Setting(whatsNewVersionSetting)
	if err != nil {
		return ""
	}
	return version
}

func loadWhatsNewFromVersion(st *store.Store) string {
	version, err := st.Setting(whatsNewFromSetting)
	if err != nil {
		return ""
	}
	return version
}

// noticePanelMin is the narrowest messages column worth reading; a rail
// too tight for it keeps the machine meters alone.
const noticePanelMin = 16

// noticeCardHex is the messages card's fill: the rail's panel tone warmed
// toward yellow, so the card reads as a sticky note in any theme.
func noticeCardHex() string { return mix(panelHex(), "#e2c044", 0.16) }

// noticeBorderStyle is the card's rounded frame, dimmed toward the same
// yellow so the outline and the fill read as one object.
func noticeBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Subtle, "#e2c044", 0.45)))
}

// noticeLegend is the card's title, set into the border: lowercase, warm,
// no fill, so it reads as a fieldset legend rather than a badge.
func noticeLegend() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Bright, "#e2c044", 0.5))).Bold(true).Render(" messages ")
}

// railFootLines is the rail's foot: the machine meters with the messages
// card docked to their right when both notices and width exist. The card
// hugs its content behind a rule that separates the two blocks.
func (m *Model) railFootLines(width int) []string {
	meters := m.computerLines(width)
	notices := m.activeNotices()
	metersWidth := maxLineWidth(meters)
	room := width - metersWidth - 3
	if len(notices) == 0 || room < noticePanelMin {
		return meters
	}

	card := m.noticeCardLines(notices, room, len(meters))
	separator := subtleStyle.Render("│")
	lines := make([]string, len(meters))
	for i := range meters {
		row := ""
		if i < len(card) {
			row = card[i]
		}
		lines[i] = padRight(meters[i], metersWidth) + " " + separator + " " + row
	}
	return lines
}

// noticeCardLines is the messages card: a fieldset — the messages legend
// set into the rounded top border, the open key into the bottom one —
// hugging its content in width and spanning the meters block in height.
func (m *Model) noticeCardLines(notices []notice, maxWidth, height int) []string {
	room := height - 2
	shown := notices
	overflow := ""
	if len(shown) > room {
		shown = shown[:room-1]
		overflow = subtleStyle.Render(fmt.Sprintf("+%d more · M", len(notices)-len(shown)))
	}

	var rows []string
	for _, n := range shown {
		rows = append(rows, n.mark()+" "+valueStyle.Render(n.title))
	}
	if overflow != "" {
		rows = append(rows, overflow)
	}

	head := noticeLegend()
	foot := keyCap("M", "open") + subtleStyle.Render(" ")
	inner := max(lipgloss.Width(head), lipgloss.Width(foot)) + 2
	if w := maxLineWidth(rows); w > inner {
		inner = w
	}
	if inner > maxWidth-4 {
		inner = maxWidth - 4
	}

	filled := make([]string, height-2)
	copy(filled, rows)
	return noticeFrame(filled, inner, head, foot)
}

// noticeFrame wraps content rows in the notices fieldset: a rounded
// yellow border with legends set into the top and bottom edges and the
// card fill behind every row. inner is the content column's width.
func noticeFrame(rows []string, inner int, topLegend, bottomLegend string) []string {
	border := noticeBorderStyle()
	edge := border.Render("│")
	legend := func(left, label, right string) string {
		label = ansi.Truncate(label, inner+1, "…")
		dashes := inner + 2 - lipgloss.Width(label)
		if label != "" {
			label = border.Render("─") + label
			dashes--
		}
		if dashes < 0 {
			dashes = 0
		}
		return border.Render(left) + label + border.Render(strings.Repeat("─", dashes)+right)
	}
	lines := []string{legend("╭", topLegend, "╮")}
	for _, row := range rows {
		row = ansi.Truncate(row, inner, "…")
		lines = append(lines, edge+paint(" "+row, inner+2, noticeCardHex())+edge)
	}
	return append(lines, legend("╰", bottomLegend, "╯"))
}

var (
	// Seams keep link tests from mutating the desktop and clipboard.
	openBrowser    = defaultOpenBrowser
	copyBrowserURL = clipboard.WriteText
)

func defaultOpenBrowser(target string) error {
	return openBrowserWith(runtime.GOOS, os.Getenv("BROWSER"), target, func(cmd *exec.Cmd) error {
		return cmd.Run()
	})
}

func openBrowserWith(goos, browser, target string, run func(*exec.Cmd) error) error {
	commands := browserCommands(goos, browser, target)
	var failures []error
	for _, cmd := range commands {
		err := run(cmd)
		if err == nil {
			return nil
		}
		failures = append(failures, fmt.Errorf("%s: %w", cmd.Args[0], err))
	}
	return errors.Join(failures...)
}

func browserCommands(goos, browser, target string) []*exec.Cmd {
	if goos == "darwin" {
		return []*exec.Cmd{exec.Command("open", target)}
	}

	var commands []*exec.Cmd
	// Keep candidates as argv so shell syntax in a URL remains inert text.
	for _, line := range strings.Split(browser, ":") {
		argv := splitEditorLine(line)
		if len(argv) == 0 {
			continue
		}
		placed := false
		for i := range argv {
			if strings.Contains(argv[i], "%s") {
				argv[i] = strings.ReplaceAll(argv[i], "%s", target)
				placed = true
			}
		}
		if !placed {
			argv = append(argv, target)
		}
		commands = append(commands, exec.Command(argv[0], argv[1:]...))
	}
	return append(commands, exec.Command("xdg-open", target))
}

type browserOpenMsg struct {
	target  string
	err     error
	copyErr error
}

func openLink(target string) tea.Cmd {
	return func() tea.Msg {
		err := openBrowser(target)
		if err == nil {
			return browserOpenMsg{target: target}
		}
		return browserOpenMsg{target: target, err: err, copyErr: copyBrowserURL(target)}
	}
}

func (m *Model) handleBrowserOpen(msg browserOpenMsg) {
	if msg.err == nil {
		return
	}
	if msg.copyErr == nil {
		m.errBar.text = fmt.Sprintf("could not open link; URL copied to clipboard: %v", msg.err)
		return
	}
	m.errBar.text = fmt.Sprintf("could not open %s: %v; copying URL: %v", msg.target, msg.err, msg.copyErr)
}

func (m *Model) openNotices(selectID string) {
	notices := m.activeNotices()
	if len(notices) == 0 {
		return
	}
	m.noticeCursor = 0
	m.noticeScroll = 0
	for i, n := range notices {
		if n.id == selectID {
			m.noticeCursor = i
			break
		}
	}
	m.mode = modeNotices
}

// openStartupNotice greets the launch when there is something to say:
// the welcome message on the first run ever, what's new on the first run
// after an update. A dev build is the developer's own tree, not an
// install, so it never greets and never advances the stored version.
func (m *Model) openStartupNotice() {
	if m.update.version == "dev" {
		return
	}
	if id := m.startupNotice(); id != "" {
		m.openNotices(id)
	}
}

// keepNoticeSelection applies a mutation that may add or remove notices
// and re-points the cursor at the notice that was selected before, by id.
// Index arithmetic cannot do this: whether the list actually changed
// depends on dismissals and on what the mutation replaced.
func (m *Model) keepNoticeSelection(apply func()) {
	if m.mode != modeNotices {
		apply()
		return
	}
	selected := ""
	if notices := m.activeNotices(); m.noticeCursor < len(notices) {
		selected = notices[m.noticeCursor].id
	}
	apply()
	m.noticeCursor = 0
	for i, n := range m.activeNotices() {
		if n.id == selected {
			m.noticeCursor = i
			break
		}
	}
}

// RestartPath is the binary main execs into after the program exits;
// empty when no self-update happened this run.
func (m *Model) RestartPath() string { return m.update.restartPath }

// isUpdateNotice marks the one notice that carries the in-place update
// action.
func isUpdateNotice(n notice) bool {
	return strings.HasPrefix(n.id, "update-")
}

// applyUpdate is the self-update seam: tests swap it for a fake instead
// of downloading a release.
var applyUpdate = update.Apply

// refreshUpdatesForApply makes u resolve the newest release at action time,
// even when several releases landed after the notice was first cached.
var refreshUpdatesForApply = update.Refresh

// detectManager is the install-source seam: tests swap it to steer u
// between the in-place swap and a delegated package manager.
var detectManager = update.DetectManager

// applyUpdateCmd routes u by install source. A package-manager install
// hands the terminal to that manager's upgrade command; a direct install
// downloads the latest release off the event loop and swaps the running
// binary. Both report the path to restart into.
func (m *Model) applyUpdateCmd() tea.Cmd {
	execPath, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return updateAppliedMsg{err: err} }
	}
	manager := detectManager(execPath)
	if manager.Advice != "" {
		return func() tea.Msg { return updateAppliedMsg{err: errors.New(manager.Advice)} }
	}
	if manager.Delegated() {
		return delegatedUpdateCmd(manager, execPath)
	}
	return func() tea.Msg {
		dir, err := config.Dir()
		if err != nil {
			return updateAppliedMsg{err: err}
		}
		result, err := refreshUpdatesForApply(context.Background(), dir, m.update.version)
		if err != nil {
			return updateAppliedMsg{result: result, err: err}
		}
		if result.Latest == "" {
			return updateAppliedMsg{result: result, upToDate: true}
		}
		if err := applyUpdate(context.Background(), result.Latest, execPath); err != nil {
			return updateAppliedMsg{result: result, err: err}
		}
		return updateAppliedMsg{path: execPath, result: result}
	}
}

// delegatedUpdateCmd suspends the TUI and runs the manager's upgrade
// command on the real terminal, so its progress output and any password
// prompt work as in a plain shell.
func delegatedUpdateCmd(manager update.Manager, execPath string) tea.Cmd {
	command := exec.Command(manager.Command[0], manager.Command[1:]...)
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return delegatedUpdateResult(manager, execPath, err)
	})
}

func delegatedUpdateResult(manager update.Manager, execPath string, err error) updateAppliedMsg {
	if err != nil {
		return updateAppliedMsg{err: fmt.Errorf("%s: %w", manager.String(), err)}
	}
	return updateAppliedMsg{path: update.RestartTarget(execPath)}
}

func (m *Model) handleNoticesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	notices := m.activeNotices()
	switch msg.String() {
	case "r":
		if m.update.refreshing {
			return m, nil
		}
		m.update.refreshing = true
		m.update.refreshPending = 2
		m.errBar.text = ""
		return m, tea.Batch(m.refreshUpdates, m.refreshFeed)
	case "u":
		if m.update.applying {
			return m, nil
		}
		if m.noticeCursor < len(notices) && isUpdateNotice(notices[m.noticeCursor]) {
			m.update.applying = true
			m.errBar.text = ""
			return m, m.applyUpdateCmd()
		}
	case "up", "k":
		if m.noticeCursor > 0 {
			m.noticeCursor--
			m.noticeScroll = 0
		}
	case "down", "j":
		if m.noticeCursor < len(notices)-1 {
			m.noticeCursor++
			m.noticeScroll = 0
		}
	case "pgup", "ctrl+u":
		m.noticeScroll = max(0, m.noticeScroll-max(4, m.height/3))
	case "pgdown", "ctrl+d":
		m.noticeScroll = min(m.noticeScroll+max(4, m.height/3), m.noticeScrollLimit(notices))
	case "enter":
		if m.noticeCursor < len(notices) && notices[m.noticeCursor].url != "" {
			return m, openLink(notices[m.noticeCursor].url)
		}
	case "x", "d":
		if m.noticeCursor < len(notices) {
			m.dismissNotice(notices[m.noticeCursor].id)
		}
		if len(notices) <= 1 {
			m.mode = modeList
			return m, nil
		}
		m.noticeCursor = min(m.noticeCursor, len(notices)-2)
		m.noticeScroll = 0
	case "esc", "q", "M":
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) finishNoticeRefresh() {
	if m.update.refreshPending > 0 {
		m.update.refreshPending--
	}
	m.update.refreshing = m.update.refreshPending > 0
}

func (m *Model) noticeScrollLimit(notices []notice) int {
	if m.noticeCursor >= len(notices) {
		return 0
	}
	selected := notices[m.noticeCursor]
	bodyRows := len(renderNoticeBody(selected, noticeInnerWidth(notices, m.width)))
	tailRows := 1
	if selected.url != "" {
		tailRows++
	}
	if m.update.refreshing {
		tailRows++
	}
	if m.update.applying {
		tailRows++
	}
	if m.errBar.text != "" {
		tailRows++
	}
	bodyRoom := noticeBodyRoom(m.height, len(notices), tailRows)
	return max(0, bodyRows-bodyRoom)
}

func noticeBodyRoom(height, noticeCount, tailRows int) int {
	return max(1, height-2-(noticeCount+2)-tailRows)
}

func (m *Model) viewNotices() string {
	notices := m.activeNotices()
	inner := noticeInnerWidth(notices, m.width)
	if len(notices) == 0 {
		frame := noticeFrame([]string{subtleStyle.Render("nothing new")}, inner,
			noticeLegend(), keyCap("esc", "close"))
		return m.centerOnBackdrop(frame)
	}
	var rows []string
	rows = append(rows, "")
	for i, n := range notices {
		marker := "  "
		title := valueStyle.Render(n.title)
		if i == m.noticeCursor {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▸ ")
			title = lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(n.title)
		}
		rows = append(rows, marker+n.mark()+" "+title)
	}
	selected := notices[m.noticeCursor]
	rows = append(rows, noticeBorderStyle().Render(strings.Repeat("┄", inner)))

	body := renderNoticeBody(selected, inner)
	var tail []string
	if selected.url != "" {
		tail = append(tail, subtleStyle.Render("↗ "+truncateTail(strings.TrimPrefix(selected.url, "https://"), inner-2)))
	}
	if m.update.refreshing {
		tail = append(tail, lipgloss.NewStyle().Foreground(colorAccent2).Render("↻ refreshing releases and messages…"))
	}
	if m.update.applying {
		tail = append(tail, lipgloss.NewStyle().Foreground(colorAccent).Render("↓ downloading "+m.update.latest+"…"))
	}
	if m.errBar.text != "" {
		tail = append(tail, m.statusMessage("✕", "●"))
	}
	tail = append(tail, "")

	rows = append(rows, fitBody(body, noticeBodyRoom(m.height, len(notices), len(tail)), m.noticeScroll)...)
	rows = append(rows, tail...)

	hint := "↑↓ pick · pgup/pgdn scroll · r refresh · ↵ open · x dismiss · esc "
	if isUpdateNotice(selected) {
		hint = "↑↓ pick · pgup/pgdn scroll · r refresh · u update · ↵ open · x dismiss · esc "
	}
	frame := noticeFrame(rows, inner,
		noticeLegend(),
		mutedStyle.Render(hint))
	return m.centerOnBackdrop(frame)
}

func noticeInnerWidth(notices []notice, terminalWidth int) int {
	inner := noticeModalInner
	for _, n := range notices {
		lines := append(append([]string{}, n.body...), n.after...)
		for _, release := range n.releases {
			lines = append(lines, release.Version)
			for _, change := range release.Changes {
				lines = append(lines, "• "+change)
			}
		}
		for _, line := range lines {
			if w := lipgloss.Width(line); w > inner {
				inner = w
			}
		}
	}
	if fit := terminalWidth - 8; inner > fit {
		inner = max(fit, 1)
	}
	return inner
}

func renderNoticeBody(n notice, width int) []string {
	var body []string
	for _, line := range n.body {
		body = appendStyledWrap(body, line, width, mutedStyle)
	}
	if len(n.releases) > 0 {
		body = append(body, "")
	}
	for i, release := range n.releases {
		if i > 0 {
			body = append(body, "")
		}
		count := release.TotalChanges
		label := "change"
		if count != 1 {
			label = "changes"
		}
		heading := release.Version
		if count > 0 {
			heading += fmt.Sprintf(" · %d %s", count, label)
		}
		body = append(body, lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(heading))
		if len(release.Changes) == 0 {
			body = append(body, subtleStyle.Render("  No summarized changes."))
		}
		for _, change := range release.Changes {
			wrapped := strings.Split(ansi.Wordwrap(change, max(width-2, 1), "-"), "\n")
			for lineIndex, line := range wrapped {
				prefix := "  "
				if lineIndex == 0 {
					prefix = "• "
				}
				body = append(body, mutedStyle.Render(prefix+line))
			}
		}
		if omitted := release.TotalChanges - len(release.Changes); omitted > 0 {
			body = append(body, subtleStyle.Render(fmt.Sprintf("  +%d more in the full notes", omitted)))
		}
	}
	if len(n.releases) > 0 && !n.rangeComplete {
		body = append(body, "", subtleStyle.Render("The local catalog covers part of this range; Enter opens the complete notes."))
	}
	if len(n.after) > 0 {
		body = append(body, "")
		for _, line := range n.after {
			body = appendStyledWrap(body, line, width, mutedStyle)
		}
	}
	return body
}

func appendStyledWrap(lines []string, text string, width int, style lipgloss.Style) []string {
	if text == "" {
		return append(lines, "")
	}
	for _, wrapped := range strings.Split(ansi.Wordwrap(text, width, "-"), "\n") {
		lines = append(lines, style.Render(wrapped))
	}
	return lines
}

// fitBody returns a scrollable window without letting a short terminal eat
// the modal border or hints. Continuation rows make hidden content explicit.
func fitBody(body []string, room, offset int) []string {
	if room < 1 {
		room = 1
	}
	if room >= len(body) {
		return body
	}
	maxOffset := max(0, len(body)-room)
	offset = min(max(offset, 0), maxOffset)
	window := append([]string(nil), body[offset:min(offset+room, len(body))]...)
	above := offset > 0
	below := offset+room < len(body)
	if len(window) == 1 && above && below {
		window[0] = subtleStyle.Render("↕ more…")
		return window
	}
	if above {
		window[0] = subtleStyle.Render("↑ more above…")
	}
	if below {
		window[len(window)-1] = subtleStyle.Render("↓ more below…")
	}
	return window
}

// noticeModalInner is the modal content column's floor, sized for the
// welcome notice's longest body line; longer bodies widen it up to the
// terminal.
const noticeModalInner = 62

func loadDismissed(st *store.Store) map[string]bool {
	dismissed := map[string]bool{}
	raw, err := st.Setting(dismissedNoticesSetting)
	if err != nil || raw == "" {
		return dismissed
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return dismissed
	}
	for _, id := range ids {
		dismissed[id] = true
	}
	return dismissed
}

func (m *Model) dismissNotice(id string) {
	m.dismissed[id] = true
	ids := make([]string, 0, len(m.dismissed))
	for dismissedID := range m.dismissed {
		ids = append(ids, dismissedID)
	}
	sort.Strings(ids)
	raw, err := json.Marshal(ids)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if err := m.store.SetSetting(dismissedNoticesSetting, string(raw)); err != nil {
		m.errBar.text = err.Error()
	}
}
