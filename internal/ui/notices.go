package ui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/YoanWai/agent-manager/internal/store"
)

const (
	noticeWelcome   = "welcome"
	noticeBugReport = "bug-report"

	dismissedNoticesSetting = "dismissed_notices"
	lastSeenVersionSetting  = "last_seen_version"
	whatsNewVersionSetting  = "whats_new_version"

	repoURL = "https://github.com/YoanWai/agent-manager"
)

// notice is one dismissible message shown in the rail's messages panel
// and readable in full from the notices modal. url is what enter opens;
// glyph and tint are its mark in both places.
type notice struct {
	id     string
	glyph  string
	tint   lipgloss.Color
	banner string
	title  string
	body   []string
	url    string
}

func (n notice) mark() string {
	return lipgloss.NewStyle().Foreground(n.tint).Render(n.glyph)
}

func (m *Model) activeNotices() []notice {
	if m.store == nil {
		return nil
	}
	var notices []notice
	if m.update.latest != "" {
		notices = append(notices, notice{
			id:     "update-" + m.update.latest,
			glyph:  "↑",
			tint:   colorAccent,
			banner: m.update.latest + " available",
			title:  "Update available",
			body: []string{
				"Release " + m.update.latest + " is out; this build is " + m.update.version + ".",
				"Enter opens the release page with the changelog and install notes.",
			},
			url: m.update.url,
		})
	}
	if m.whatsNewVersion == m.update.version {
		notices = append(notices, notice{
			id:     "whatsnew-" + m.update.version,
			glyph:  "✦",
			tint:   colorAccent2,
			banner: "updated to " + m.update.version,
			title:  "What's new in " + m.update.version,
			body: []string{
				"This machine now runs " + m.update.version + ".",
				"Enter opens the release notes for everything that changed.",
			},
			url: repoURL + "/releases/tag/" + m.update.version,
		})
	}
	for _, msg := range m.feedMessages {
		notices = append(notices, notice{
			id:     msg.ID,
			glyph:  "◆",
			tint:   colorAccent2,
			banner: msg.Banner,
			title:  msg.Title,
			body:   msg.Body,
			url:    msg.URL,
		})
	}
	notices = append(notices,
		notice{
			id:     noticeWelcome,
			glyph:  "✳",
			tint:   colorAccent2,
			banner: "welcome — press M",
			title:  "Welcome to agent-manager",
			body: []string{
				"Every row on the left is a live agent session: enter attaches,",
				"space sends a quick prompt, ? shows every key.",
				"Messages like this one live here; x dismisses one for good.",
			},
			url: repoURL + "#readme",
		},
		notice{
			id:     noticeBugReport,
			glyph:  "?",
			tint:   colorAccent,
			banner: "report a bug",
			title:  "Found a bug?",
			body: []string{
				"Enter opens a GitHub issue prefilled with your version and OS.",
				"A pane screenshot and the steps that led there help the most.",
			},
			url: bugReportURL(m.update.version),
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
	body := fmt.Sprintf("**Version:** %s\n**OS:** %s/%s\n\n**What happened:**\n", version, runtime.GOOS, runtime.GOARCH)
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
	if err := m.store.SetSetting(lastSeenVersionSetting, m.update.version); err != nil {
		m.errBar.text = err.Error()
		return ""
	}
	if seen == "" {
		return noticeWelcome
	}
	if err := m.store.SetSetting(whatsNewVersionSetting, m.update.version); err != nil {
		m.errBar.text = err.Error()
	}
	m.whatsNewVersion = m.update.version
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
		rows = append(rows, n.mark()+" "+valueStyle.Render(n.banner))
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

// openBrowser is a var so tests can capture the URL instead of opening one.
var openBrowser = defaultOpenBrowser

func defaultOpenBrowser(target string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", target).Start()
	}
	return exec.Command("xdg-open", target).Start()
}

func (m *Model) openNotices(selectID string) {
	notices := m.activeNotices()
	if len(notices) == 0 {
		return
	}
	m.noticeCursor = 0
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

func (m *Model) handleNoticesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	notices := m.activeNotices()
	switch msg.String() {
	case "up", "k":
		if m.noticeCursor > 0 {
			m.noticeCursor--
		}
	case "down", "j":
		if m.noticeCursor < len(notices)-1 {
			m.noticeCursor++
		}
	case "enter":
		if m.noticeCursor < len(notices) && notices[m.noticeCursor].url != "" {
			if err := openBrowser(notices[m.noticeCursor].url); err != nil {
				m.errBar.text = err.Error()
			}
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
	case "esc", "q", "M":
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) viewNotices() string {
	inner := noticeModalInner
	if m.width >= 8 && inner > m.width-8 {
		inner = m.width - 8
	}
	notices := m.activeNotices()
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
	for _, line := range selected.body {
		rows = append(rows, mutedStyle.Render(line))
	}
	if selected.url != "" {
		rows = append(rows, subtleStyle.Render("↗ "+truncateTail(strings.TrimPrefix(selected.url, "https://"), inner-2)))
	}
	if m.errBar.text != "" {
		rows = append(rows, errStyle.Render("✕ "+m.errBar.text))
	}
	rows = append(rows, "")

	frame := noticeFrame(rows, inner,
		noticeLegend(),
		mutedStyle.Render("↑↓ pick · ↵ open · x dismiss · esc "))
	return m.centerOnBackdrop(frame)
}

// noticeModalInner is the modal's content column, sized for the welcome
// notice's longest body line.
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
