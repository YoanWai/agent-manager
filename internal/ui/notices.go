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
// and readable in full from the notices modal. url is what enter opens.
type notice struct {
	id     string
	banner string
	title  string
	body   []string
	url    string
}

func (m *Model) activeNotices() []notice {
	if m.store == nil {
		return nil
	}
	var notices []notice
	if m.update.latest != "" {
		notices = append(notices, notice{
			id:     "update-" + m.update.latest,
			banner: "↑ " + m.update.latest + " available",
			title:  "Update available",
			body: []string{
				"Release " + m.update.latest + " is out; this build is " + m.update.version + ".",
				"Enter opens the release page with the changelog and install notes.",
			},
			url: m.update.url,
		})
	}
	if updated, err := m.store.Setting(whatsNewVersionSetting); err == nil && updated == m.update.version {
		notices = append(notices, notice{
			id:     "whatsnew-" + m.update.version,
			banner: "✦ updated to " + m.update.version,
			title:  "What's new in " + m.update.version,
			body: []string{
				"This machine now runs " + m.update.version + ".",
				"Enter opens the release notes for everything that changed.",
			},
			url: repoURL + "/releases/tag/" + m.update.version,
		})
	}
	notices = append(notices,
		notice{
			id:     noticeWelcome,
			banner: "✳ welcome — press M",
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
			banner: "? report a bug",
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
	return "whatsnew-" + m.update.version
}

// noticePanelMin is the narrowest messages column worth reading; a rail
// too tight for it keeps the machine meters alone.
const noticePanelMin = 16

// railFootLines is the rail's foot: the machine meters with the messages
// panel docked to their right when both notices and width exist.
func (m *Model) railFootLines(width int) []string {
	meters := m.computerLines(width)
	notices := m.activeNotices()
	metersWidth := 0
	for _, line := range meters {
		if w := lipgloss.Width(line); w > metersWidth {
			metersWidth = w
		}
	}
	panelWidth := width - metersWidth - railGutter
	if len(notices) == 0 || panelWidth < noticePanelMin {
		return meters
	}

	panel := m.noticePanelLines(notices, panelWidth, len(meters))
	gap := strings.Repeat(" ", railGutter)
	lines := make([]string, len(meters))
	for i := range meters {
		if i >= len(panel) {
			lines[i] = meters[i]
			continue
		}
		lines[i] = padRight(meters[i], metersWidth) + gap + panel[i]
	}
	return lines
}

// noticePanelLines is the messages column: a quiet header, one banner per
// notice, and an overflow count when the meters block is shorter than the
// list. Height mirrors the meters so the two columns read as one block.
func (m *Model) noticePanelLines(notices []notice, width, height int) []string {
	lines := []string{subtleStyle.Render("messages") + subtleStyle.Render("  M")}
	room := height - 2
	if room < 1 {
		room = 1
	}
	shown := notices
	if len(shown) > room {
		shown = shown[:room-1]
	}
	for _, n := range shown {
		lines = append(lines, truncateTail(valueStyle.Render(n.banner), width))
	}
	if rest := len(notices) - len(shown); rest > 0 {
		lines = append(lines, subtleStyle.Render(fmt.Sprintf("+%d more · M", rest)))
	}
	return lines
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
		if remaining := len(notices) - 1; m.noticeCursor >= remaining {
			m.noticeCursor = remaining - 1
		}
		if m.noticeCursor < 0 {
			m.noticeCursor = 0
		}
		if len(notices) <= 1 {
			m.mode = modeList
		}
	case "esc", "q", "M":
		m.mode = modeList
	}
	return m, nil
}

func (m *Model) viewNotices() string {
	notices := m.activeNotices()
	if len(notices) == 0 {
		return m.card("messages", subtleStyle.Render("nothing new"), "esc close")
	}
	if m.noticeCursor >= len(notices) {
		m.noticeCursor = len(notices) - 1
	}
	var body strings.Builder
	for i, n := range notices {
		marker := subtleStyle.Render("  ")
		title := valueStyle.Render(n.title)
		if i == m.noticeCursor {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("▸ ")
			title = lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(n.title)
		}
		body.WriteString(marker + title + "\n")
	}
	body.WriteString("\n")
	for _, line := range notices[m.noticeCursor].body {
		body.WriteString(subtleStyle.Render(line) + "\n")
	}
	return m.card("messages", strings.TrimRight(body.String(), "\n"), "↑↓ pick · ↵ open link · x dismiss · esc close")
}

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
