package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"net/url"
	"os"
	"os/exec"
	"runtime"
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
	noticeArrowStep         = "arrow-step-beta"
	noticeConfigNotImported = "config-not-imported"

	dismissedNoticesSetting = "dismissed_notices"
	lastSeenVersionSetting  = "last_seen_version"
	whatsNewVersionSetting  = "whats_new_version"
	whatsNewFromSetting     = "whats_new_from_version"

	repoURL = "https://github.com/YoanWai/agent-manager"
)

type notice struct {
	id       string
	glyph    string
	tint     lipgloss.Color
	title    string
	headline string
	// accent holds the phrases of body drawn in the accent color.
	accent []string
	body   []string
	// releaseNotes marks a body that introduces releases.
	releaseNotes  bool
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
	m.update.installed = update.Between(m.update.releases, m.notices.whatsNewFromVersion, m.update.version)
}

// newestHeadline is the headline of the last release in an oldest-first range.
func newestHeadline(releases []update.Release) string {
	if len(releases) == 0 {
		return ""
	}
	return releases[len(releases)-1].Headline
}

func releaseCountLabel(releaseRange update.ReleaseRange) string {
	count := fmt.Sprint(len(releaseRange.Releases))
	if !releaseRange.Complete {
		count += "+"
	}
	return count
}

func (p *noticesPanel) active(h noticesHost) []notice {
	src := h.noticeSources()
	if !src.store {
		return nil
	}
	var notices []notice
	if src.update.latest != "" {
		releaseRange := src.update.available
		title := src.update.latest + " available"
		if len(releaseRange.Releases) > 1 {
			title = releaseCountLabel(releaseRange) + " releases available · " + src.update.latest
		}
		body := []string{"You are on " + src.update.version + ". Here is everything released since then:"}
		if len(releaseRange.Releases) == 0 {
			body = append(body, "The generated change summary is not available yet; r refreshes it now.")
		}
		notices = append(notices, notice{
			id:            "update-" + src.update.latest,
			glyph:         "↑",
			tint:          colorAccent,
			title:         title,
			headline:      newestHeadline(releaseRange.Releases),
			releaseNotes:  true,
			body:          body,
			releases:      releaseRange.Releases,
			rangeComplete: releaseRange.Complete,
			after: []string{
				"u updates once to " + src.update.latest + " and restarts; every release above is included.",
				"Enter opens the full release notes.",
			},
			url: src.update.url,
		})
	}
	if p.whatsNewVersion == src.update.version {
		releaseRange := src.update.installed
		title := "Updated to " + src.update.version
		if len(releaseRange.Releases) > 1 {
			title = "Updated across " + releaseCountLabel(releaseRange) + " releases · " + src.update.version
		}
		body := []string{"Updated from " + p.whatsNewFromVersion + " to " + src.update.version + "."}
		if len(releaseRange.Releases) == 0 && !src.update.checked {
			body = append(body, "Loading the change summary from GitHub…")
		} else if len(releaseRange.Releases) == 0 {
			body = append(body, "No generated change summary was found; r refreshes GitHub now.")
		}
		notices = append(notices, notice{
			id:           "whatsnew-" + src.update.version,
			glyph:        "✦",
			tint:         colorAccent2,
			title:        title,
			headline:     newestHeadline(releaseRange.Releases),
			releaseNotes: true,
			body:         body,
			releases:     releaseRange.Releases,
			after: []string{
				"Enter opens the full release notes.",
			},
			rangeComplete: releaseRange.Complete,
			url:           repoURL + "/releases/tag/v" + strings.TrimPrefix(src.update.version, "v"),
		})
	}
	for _, msg := range p.feedMessages {
		notices = append(notices, notice{
			id:       msg.ID,
			glyph:    "◆",
			tint:     colorAccent2,
			title:    msg.Title,
			headline: msg.Headline,
			accent:   msg.Accent,
			body:     msg.Body,
			url:      msg.URL,
		})
	}
	if p.configImportError != "" {
		notices = append(notices, notice{
			id:    noticeConfigNotImported,
			glyph: "⚙",
			tint:  lipgloss.Color("#e2c044"),
			title: "Your config.toml was not carried into Settings",
			body: []string{
				"The keys and the editor live in Settings now. A config.toml that",
				"names them is read once to carry them over, and yours was refused:",
				"",
				p.configImportError,
				"",
				"The keys are the defaults and the editor is picked for you until",
				"you set them in Settings. The file is not read again, and is yours",
				"to delete.",
			},
			after: []string{
				"Enter opens the page on what Settings holds.",
			},
			url: repoURL + "/blob/main/docs/configuration.md",
		})
	}
	notices = append(notices,
		notice{
			id:    noticeWelcome,
			glyph: "✳",
			tint:  colorAccent2,
			title: "Welcome to agent-manager",
			body:  src.welcomeBody(),
			url:   repoURL + "#readme",
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
			url: arrowStepFeedbackURL(src.update.version),
		},
	)

	kept := notices[:0]
	for _, n := range notices {
		if !p.dismissed[n.id] {
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
	seen, err := m.services.store.Setting(lastSeenVersionSetting)
	if err != nil {
		return ""
	}
	if seen == m.update.version {
		return ""
	}
	if seen == "" {
		if err := m.services.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
			m.reportErr(err.Error())
			return ""
		}
		return noticeWelcome
	}
	if !update.Newer(m.update.version, seen) {
		if err := m.services.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
			m.reportErr(err.Error())
		}
		return ""
	}
	if err := m.services.store.SetSetting(whatsNewFromSetting, seen); err != nil {
		m.reportErr(err.Error())
		return ""
	}
	if err := m.services.store.SetSetting(whatsNewVersionSetting, m.update.version); err != nil {
		m.reportErr(err.Error())
		return ""
	}
	if err := m.services.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
		m.reportErr(err.Error())
		return ""
	}
	m.notices.whatsNewFromVersion = seen
	m.notices.whatsNewVersion = m.update.version
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

// noticeCardHex warms the messages modal's fill toward yellow.
func noticeCardHex() string { return mix(panelHex(), "#e2c044", 0.16) }

// noticeBorderStyle is the card's rounded frame, dimmed toward the same
// yellow so the outline and the fill read as one object.
func noticeBorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Subtle, "#e2c044", 0.45)))
}

func noticeTitleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(mix(current.Bright, "#e2c044", 0.5))).Bold(true)
}

// noticeLegend is the card's title, set into the border: lowercase, warm,
// no fill, so it reads as a fieldset legend rather than a badge.
func noticeLegend() string {
	return noticeTitleStyle().Render(" messages ")
}

// noticeHit follows the messages entry in the painted key legend.
type noticeHit struct {
	x0, x1, y0, y1 int
	ok             bool
}

func (h noticeHit) contains(x, y int) bool {
	return h.ok && x >= h.x0 && x < h.x1 && y >= h.y0 && y < h.y1
}

func (p *noticesPanel) placeHit(h noticesHost, binding, footer string, firstRow int) {
	if binding == "" {
		return
	}
	label := ansi.Strip(keyCapQuiet(binding, "messages"))
	for i, line := range splitLines(footer) {
		line = ansi.Strip(line)
		start := strings.Index(line, label)
		if start < 0 {
			continue
		}
		x := ansi.StringWidth(line[:start])
		y := firstRow + i
		if width, height := h.size(); x+ansi.StringWidth(label) <= width && y < height {
			p.noticeHit = noticeHit{x0: x, x1: x + ansi.StringWidth(label), y0: y, y1: y + 1, ok: true}
		}
		return
	}
}

func (m *Model) railFootLines(width int) []string {
	if m.prefs.hideStats {
		return nil
	}
	if m.prefs.fullLayout {
		return m.fullFootLine(width)
	}
	return m.computerLines(width)
}

func (m *Model) fullFootLine(width int) []string {
	reading := func(label, value string, ok bool) string {
		if !ok {
			return labelStyle.Render(label+" ") + subtleStyle.Render("n/a")
		}
		return labelStyle.Render(label+" ") + valueStyle.Render(value)
	}
	snap := m.workspace.snap
	parts := []string{
		reading("cpu", fmt.Sprintf("%.0f%%", snap.CPUPercent), snap.CPUOK),
		reading("mem", fmt.Sprintf("%.0f%% %s/%s", snap.MemPercent, humanBytes(snap.MemUsed), humanBytes(snap.MemTotal)), snap.MemOK),
	}
	if snap.SwapOK && snap.SwapTotal > 0 {
		parts = append(parts, reading("swap", fmt.Sprintf("%.0f%% %s/%s", snap.SwapPercent, humanBytes(snap.SwapUsed), humanBytes(snap.SwapTotal)), true))
	}
	parts = append(parts, reading("disk", fmt.Sprintf("%.0f%% %s available", snap.DiskPercent, diskBytes(snap.DiskAvailable)), snap.DiskOK))
	if snap.BatteryOK {
		value := fmt.Sprintf("%.0f%%", snap.BatteryPercent)
		if snap.BatteryCharging {
			value += " charging"
		}
		parts = append(parts, reading("batt", value, true))
	}
	if temps := tempReadings(snap); temps != "" {
		parts = append(parts, labelStyle.Render("temp ")+temps)
	}
	if m.workspace.net.rates {
		parts = append(parts, reading("net", "↓ "+humanBytes(m.workspace.net.down)+"/s ↑ "+humanBytes(m.workspace.net.up)+"/s", true))
	}
	line := strings.Repeat(" ", railInset) + strings.Join(parts, "  ")
	return []string{ansi.Truncate(line, max(width-railInset, 0), "…")}
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
		if remoteTerminal() {
			return linkPageMsg{url: target}
		}
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
		m.reportErr(fmt.Sprintf("could not open link; URL copied to clipboard: %v", msg.err))
		return
	}
	m.reportErr(fmt.Sprintf("could not open %s: %v; copying URL: %v", msg.target, msg.err, msg.copyErr))
}

// openNotices shows the panel even with nothing in it: a fresh install and
// the first run after an update both retire every message, and that is
// exactly when someone reaches for r to look again.
func (p *noticesPanel) open(h noticesHost, selectID string) {
	notices := p.active(h)
	p.noticeCursor = 0
	p.noticeScroll = 0
	for i, n := range notices {
		if n.id == selectID {
			p.noticeCursor = i
			break
		}
	}
	h.setMode(modeNotices)
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
		m.notices.open(m, id)
	}
}

// keepSelection applies a mutation that may add or remove notices
// and re-points the cursor at the notice that was selected before, by id.
// Index arithmetic cannot do this: whether the list actually changed
// depends on dismissals and on what the mutation replaced.
func (p *noticesPanel) keepSelection(h noticesHost, apply func()) {
	if h.currentMode() != modeNotices {
		apply()
		return
	}
	selected := ""
	if notices := p.active(h); p.noticeCursor < len(notices) {
		selected = notices[p.noticeCursor].id
	}
	apply()
	p.noticeCursor = 0
	for i, n := range p.active(h) {
		if n.id == selected {
			p.noticeCursor = i
			break
		}
	}
}

func (m *Model) applyNotices(apply func()) {
	before := map[string]bool{}
	for _, n := range m.notices.active(m) {
		before[n.id] = true
	}
	m.notices.keepSelection(m, apply)
	var added string
	for _, n := range m.notices.active(m) {
		if !before[n.id] {
			added = n.id
			break
		}
	}
	if added == "" {
		return
	}
	if m.mode == modeNotices {
		return
	}
	if m.listReadyForNotice() {
		m.notices.open(m, added)
		return
	}
	m.notices.pendingNotice = added
}

func (m *Model) listReadyForNotice() bool {
	return !m.effects.quitting && m.mode == modeList && !m.rail.Searching() && !m.quick.active && !m.layout.split.resizeMode &&
		!m.rail.Reordering() && !m.rail.MenuOpen()
}

func (m *Model) flushPendingNotice() {
	if m.notices.pendingNotice == "" || !m.listReadyForNotice() {
		return
	}
	id := m.notices.pendingNotice
	m.notices.pendingNotice = ""
	for _, n := range m.notices.active(m) {
		if n.id == id {
			m.notices.open(m, id)
			return
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
	return execTerminalProcess(command, func(err error) tea.Msg {
		return delegatedUpdateResult(manager, execPath, err)
	})
}

func delegatedUpdateResult(manager update.Manager, execPath string, err error) updateAppliedMsg {
	if err != nil {
		return updateAppliedMsg{err: fmt.Errorf("%s: %w", manager.String(), err)}
	}
	return updateAppliedMsg{path: update.RestartTarget(execPath)}
}

func (p *noticesPanel) handleKey(h noticesHost, msg tea.KeyMsg) tea.Cmd {
	notices := p.active(h)
	_, height := h.size()
	switch msg.String() {
	case "r":
		return h.refreshNotices()
	case "u":
		if p.noticeCursor < len(notices) && isUpdateNotice(notices[p.noticeCursor]) {
			return h.startUpdate()
		}
	case "up", "k":
		if p.noticeCursor > 0 {
			p.noticeCursor--
			p.noticeScroll = 0
		}
	case "down", "j":
		if p.noticeCursor < len(notices)-1 {
			p.noticeCursor++
			p.noticeScroll = 0
		}
	case "pgup", "ctrl+u":
		p.scroll(h, notices, -max(4, height/3))
	case "pgdown", "ctrl+d":
		p.scroll(h, notices, max(4, height/3))
	case "home", "g":
		p.noticeScroll = 0
	case "end", "G":
		p.noticeScroll = p.scrollLimit(h, notices)
	case "enter":
		if p.noticeCursor < len(notices) && notices[p.noticeCursor].url != "" {
			return openLink(notices[p.noticeCursor].url)
		}
	case "x", "d":
		if p.noticeCursor < len(notices) {
			h.dismissNotice(notices[p.noticeCursor].id)
		}
		if len(notices) <= 1 {
			h.setMode(modeList)
			return nil
		}
		p.noticeCursor = min(p.noticeCursor, len(notices)-2)
		p.noticeScroll = 0
	case "esc", "q", "M":
		h.setMode(modeList)
	}
	return nil
}

func (m *Model) finishNoticeRefresh() {
	if m.update.refreshPending > 0 {
		m.update.refreshPending--
	}
	m.update.refreshing = m.update.refreshPending > 0
}

func (p *noticesPanel) scrollLimit(h noticesHost, notices []notice) int {
	if p.noticeCursor >= len(notices) {
		return 0
	}
	width, height := h.size()
	inner := noticeInnerWidth(notices, width)
	room := noticeBodyRoom(height, len(notices), len(p.tail(h, notices, inner))+1)
	body, _ := noticeBodyLayout(notices[p.noticeCursor], inner, room)
	return max(0, len(body)-room)
}

// scrollNotice steps from the page on screen, since a resize can leave the saved offset past the last page.
func (p *noticesPanel) scroll(h noticesHost, notices []notice, rows int) {
	limit := p.scrollLimit(h, notices)
	p.noticeScroll = min(max(min(p.noticeScroll, limit)+rows, 0), limit)
}

func noticeBodyRoom(height, noticeCount, tailRows int) int {
	return max(1, height-2-(noticeCount+2)-tailRows)
}

func (p *noticesPanel) tail(h noticesHost, notices []notice, inner int) []string {
	var tail []string
	if p.noticeCursor < len(notices) {
		if url := notices[p.noticeCursor].url; url != "" {
			tail = append(tail, subtleStyle.Render("↗ "+truncateTail(strings.TrimPrefix(url, "https://"), inner-2)))
		}
	}
	release := h.noticeSources().update
	if release.refreshing {
		tail = append(tail, lipgloss.NewStyle().Foreground(colorAccent2).Render("↻ refreshing releases and messages…"))
	}
	if release.applying {
		tail = append(tail, lipgloss.NewStyle().Foreground(colorAccent).Render("↓ downloading "+release.latest+"…"))
	}
	if status := h.statusRow(); status != "" {
		tail = append(tail, status)
	}
	return tail
}

func (p *noticesPanel) view(h noticesHost) []string {
	notices := p.active(h)
	width, height := h.size()
	inner := noticeInnerWidth(notices, width)
	if len(notices) == 0 {
		rows := []string{subtleStyle.Render("nothing new")}
		rows = append(rows, p.tail(h, notices, inner)...)
		frame := noticeFrame(rows, inner, noticeLegend(),
			mutedStyle.Render("r refresh · esc "))
		return frame
	}
	var rows []string
	rows = append(rows, "")
	for i, n := range notices {
		marker := "  "
		title := valueStyle.Render(n.title)
		if i == p.noticeCursor {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▸ ")
			title = lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(n.title)
		}
		rows = append(rows, marker+n.mark()+" "+title)
	}
	selected := notices[p.noticeCursor]
	rows = append(rows, noticeBorderStyle().Render(strings.Repeat("┄", inner)))

	tail := p.tail(h, notices, inner)
	tail = append(tail, "")

	room := noticeBodyRoom(height, len(notices), len(tail))
	body, scrolls := noticeBodyLayout(selected, inner, room)
	if scrolls {
		body = noticeScrollWindow(body, room, p.noticeScroll, inner)
	}
	rows = append(rows, body...)
	rows = append(rows, tail...)

	frame := noticeFrame(rows, inner,
		noticeLegend(),
		mutedStyle.Render(noticeHint(selected, inner)))
	return frame
}

func noticeHint(selected notice, inner int) string {
	updateKey := ""
	if isUpdateNotice(selected) {
		updateKey = "u update · "
	}
	if hint := "↑↓ pick · pgup/pgdn/home/end scroll · r refresh · " + updateKey + "↵ open · x dismiss · esc "; lipgloss.Width(hint) <= inner+1 {
		return hint
	}
	return "↑↓ pick · pgup/pgdn scroll · r refresh · " + updateKey + "↵ open · x dismiss · esc "
}

// plainMarks drops the accent marks release text carries.
func plainMarks(text string) string {
	return strings.ReplaceAll(text, "`", "")
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
	if id == "" || m.services.store == nil || m.noticeDismissQueued(id) || m.notices.dismissed[id] {
		return
	}
	m.notices.dismissed[id] = true
	m.enqueueEffect(noticeDismissRequest{
		id:            id,
		foregroundGen: m.gens.foreground,
		modal:         m.mode == modeNotices,
	}, 0, false)
}

// The welcome names the keys as the tables bind them on this run: the first
// key of each action, and nothing for an action turned off.
func (src noticeSources) welcomeBody() []string {
	prompt, help, search, settings := src.firstListKey(keybind.Prompt), src.firstListKey(keybind.Help), src.firstListKey(keybind.Search), src.firstListKey(keybind.Settings)
	body := []string{
		"Every row on the left is a live agent session.",
		"",
		welcomeRow(src.firstListKey(keybind.NewSession), "new session", prompt, "quick prompt mode"),
		welcomeRow(src.firstListKey(keybind.Open), "focus it", src.firstListKey(keybind.Attach), "attach it full screen"),
		src.welcomeSessionKeysLine(),
		welcomeRow(src.firstListKeys(keybind.Kill, keybind.Revive), "kill / revive", settings, "settings"),
		"",
	}
	if prompt != "" {
		body = append(body, prompt+" on a group row opens quick prompt mode to start a new agent there.")
	}
	body = append(body, "Each row's mark is its state: ◐ working, ◆ waiting, ● finished, ○ idle.", "")
	if help != "" {
		keyMap := "Press " + help + " for every key: the map scrolls"
		if search != "" {
			keyMap += ", and " + search + " searches it"
		}
		body = append(body, keyMap+".")
	}
	return append(body,
		"A bug or an idea? Settings ("+settings+") has a row for each.",
		"Messages like this one live here; x dismisses one for good.",
	)
}

func (src noticeSources) firstListKey(action string) string {
	keys := src.listKeys.Binding(action).Keys()
	if len(keys) == 0 {
		return ""
	}
	return keys[0].Glyph()
}

func (src noticeSources) firstListKeys(actions ...string) string {
	var glyphs []string
	for _, action := range actions {
		if glyph := src.firstListKey(action); glyph != "" {
			glyphs = append(glyphs, glyph)
		}
	}
	return strings.Join(glyphs, " / ")
}

// welcomeRow is two key columns; a half with no key left is blank.
func welcomeRow(leftKey, leftDoes, rightKey, rightDoes string) string {
	if leftKey == "" {
		leftDoes = ""
	}
	if rightKey == "" {
		rightDoes = ""
	}
	return strings.TrimRight(fmt.Sprintf("%-6s %-22s%-6s %s", leftKey, leftDoes, rightKey, rightDoes), " ")
}

// The widths are the columns of the welcome rows around this line.
func (src noticeSources) welcomeSessionKeysLine() string {
	first := func(action string) string {
		keys := src.sessionKeys.Binding(action).Keys()
		if len(keys) == 0 {
			return ""
		}
		return keys[0].Tea()
	}
	return welcomeRow(first(keybind.Detach), "back to the manager", first(keybind.Review), "review its diff")
}
