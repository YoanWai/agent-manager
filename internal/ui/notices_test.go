package ui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/YoanWai/agent-manager/internal/update"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func noticeStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func noticeModel(st *store.Store, version string) *Model {
	return &Model{
		update: updateInfo{version: version},
		services: services{
			store:    st,
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		notices: noticesState{
			dismissed:           loadDismissed(st),
			whatsNewVersion:     loadWhatsNewVersion(st),
			whatsNewFromVersion: loadWhatsNewFromVersion(st),
		},
	}
}

func noticeIDs(notices []notice) []string {
	ids := make([]string, len(notices))
	for i, n := range notices {
		ids[i] = n.id
	}
	return ids
}

func persistNoticeDismissal(t *testing.T, m *Model, id string) {
	t.Helper()
	m.dismissNotice(id)
	m.drainEffects(t)
}

func TestActiveNoticesPinnedByDefault(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	ids := noticeIDs(m.activeNotices())
	if !contains(ids, noticeWelcome) {
		t.Fatalf("want %s among %v", noticeWelcome, ids)
	}
	var welcome notice
	for _, n := range m.activeNotices() {
		if n.id == noticeWelcome {
			welcome = n
		}
	}
	joined := strings.Join(welcome.body, " ")
	if !strings.Contains(joined, "Settings (s)") || !strings.Contains(joined, "A bug or an idea?") {
		t.Fatalf("welcome should point at Settings for a bug or an idea: %q", joined)
	}
	for _, n := range m.activeNotices() {
		if n.id != noticeWelcome && n.url == bugReportURL(m.update.version) {
			t.Fatalf("standalone bug-report notice still active: %q", n.id)
		}
	}
}

func TestDismissPersistsAcrossRestart(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	persistNoticeDismissal(t, m, noticeWelcome)
	if contains(noticeIDs(m.activeNotices()), noticeWelcome) {
		t.Fatal("dismissed notice still active")
	}

	reopened := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(reopened.activeNotices()), noticeWelcome) {
		t.Fatal("dismissal did not survive restart")
	}
}

func TestConfigNotImportedNoticeCarriesTheReason(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(m.activeNotices()), noticeConfigNotImported) {
		t.Fatal("an import that went through has nothing to report")
	}
	m.notices.configImportError = "config.toml: keybindings.session.detach needs at least one key"
	var refused notice
	for _, n := range m.activeNotices() {
		if n.id == noticeConfigNotImported {
			refused = n
		}
	}
	if refused.id == "" {
		t.Fatalf("want %s among %v", noticeConfigNotImported, noticeIDs(m.activeNotices()))
	}
	if !contains(refused.body, m.notices.configImportError) {
		t.Fatalf("the notice should carry the reason: %q", refused.body)
	}
	persistNoticeDismissal(t, m, noticeConfigNotImported)
	reopened := noticeModel(st, "v0.2.0")
	reopened.notices.configImportError = m.notices.configImportError
	if contains(noticeIDs(reopened.activeNotices()), noticeConfigNotImported) {
		t.Fatal("dismissal did not survive restart")
	}
}

func TestUpdateNoticePerRelease(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.update.latest = "v0.3.0"
	m.update.url = "https://example.com/v0.3.0"

	ids := noticeIDs(m.activeNotices())
	if !contains(ids, "update-v0.3.0") {
		t.Fatalf("want update notice, got %v", ids)
	}

	persistNoticeDismissal(t, m, "update-v0.3.0")
	if contains(noticeIDs(m.activeNotices()), "update-v0.3.0") {
		t.Fatal("dismissed update notice still active")
	}

	m.update.latest = "v0.4.0"
	if !contains(noticeIDs(m.activeNotices()), "update-v0.4.0") {
		t.Fatal("a newer release must surface its own notice")
	}
}

func TestUpdateNoticeSummarizesEverySkippedRelease(t *testing.T) {
	m := modalModel(t)
	releases := []update.Release{
		uiRelease("v0.6.0", "Sessions: Keep focus after refresh"),
		uiRelease("v0.5.0", "Groups: Create immediately"),
		uiRelease("v0.4.0", "UI: Scroll long prompts"),
		uiRelease("v0.3.0", "Worktree: Respect group defaults"),
		uiRelease("v0.2.0", "Current release"),
	}
	m.Update(updateMsg{
		latest:   "v0.6.0",
		url:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.6.0",
		releases: releases,
	})
	m.openNotices("update-v0.6.0")

	n := m.activeNotices()[m.notices.noticeCursor]
	if n.title != "4 releases available · v0.6.0" {
		t.Fatalf("title = %q", n.title)
	}
	if len(n.releases) != 4 {
		t.Fatalf("summary has %d releases, want 4", len(n.releases))
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{
		"4 releases available · v0.6.0",
		"v0.3.0",
		"Worktree: Respect group defaults",
		"v0.6.0",
		"updates once to v0.6.0",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("cumulative modal missing %q:\n%s", want, frame)
		}
	}
}

func TestPostUpdateNoticeUsesPersistedStartingVersion(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(lastSeenVersionSetting, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.5.0")
	m.layout.width, m.layout.height = 100, 34
	m.update.checked = true
	m.update.releases = []update.Release{
		uiRelease("v0.5.0", "Groups: Create immediately"),
		uiRelease("v0.4.0", "UI: Scroll long prompts"),
		uiRelease("v0.3.0", "Worktree: Respect defaults"),
		uiRelease("v0.2.0", "Notices: Summarize releases"),
		uiRelease("v0.1.0", "Starting release"),
	}
	if got := m.startupNotice(); got != "whatsnew-v0.5.0" {
		t.Fatalf("startup notice = %q", got)
	}
	m.openNotices("whatsnew-v0.5.0")

	if from, _ := st.Setting(whatsNewFromSetting); from != "v0.1.0" {
		t.Fatalf("persisted starting version = %q", from)
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{
		"Updated across 4 releases · v0.5.0",
		"Updated from v0.1.0 to v0.5.0.",
		"v0.2.0",
		"v0.5.0",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("post-update modal missing %q:\n%s", want, frame)
		}
	}
}

func TestVersionDowngradeDoesNotClaimAnUpdate(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(lastSeenVersionSetting, "v0.5.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.4.0")
	if got := m.startupNotice(); got != "" {
		t.Fatalf("downgrade should not open what's new, got %q", got)
	}
	if m.notices.whatsNewVersion == "v0.4.0" {
		t.Fatal("downgrade was recorded as an upgrade")
	}
	if version, _ := st.Setting(whatsNewVersionSetting); version != "" {
		t.Fatalf("what's-new version persisted on downgrade: %q", version)
	}
	if version, _ := st.Setting(whatsNewFromSetting); version != "" {
		t.Fatalf("what's-new source persisted on downgrade: %q", version)
	}
	if version, _ := st.Setting(lastSeenVersionSetting); version != "v0.4.0" {
		t.Fatalf("last seen version = %q, want v0.4.0", version)
	}
}

func TestStartupNoticeFirstLaunch(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	if got := m.startupNotice(); got != noticeWelcome {
		t.Fatalf("first launch should open the welcome notice, got %q", got)
	}
	if seen, _ := st.Setting(lastSeenVersionSetting); seen != "v0.2.0" {
		t.Fatalf("last seen version not recorded, got %q", seen)
	}

	again := noticeModel(st, "v0.2.0")
	if got := again.startupNotice(); got != "" {
		t.Fatalf("same version must not reopen a notice, got %q", got)
	}
}

func TestStartupNoticeVersionChange(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(lastSeenVersionSetting, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.2.0")
	got := m.startupNotice()
	if got != "whatsnew-v0.2.0" {
		t.Fatalf("version change should open what's new, got %q", got)
	}
	if !contains(noticeIDs(m.activeNotices()), "whatsnew-v0.2.0") {
		t.Fatal("what's new notice must be listed until dismissed")
	}
	if seen, _ := st.Setting(lastSeenVersionSetting); seen != "v0.2.0" {
		t.Fatalf("last seen version not advanced, got %q", seen)
	}
}

func TestWhatsNewSurvivesRestartUntilDismissed(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(lastSeenVersionSetting, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.2.0")
	if m.startupNotice() == "" {
		t.Fatal("want an auto-opened notice")
	}

	reopened := noticeModel(st, "v0.2.0")
	if reopened.startupNotice() != "" {
		t.Fatal("startup notice must fire once per version")
	}
	if !contains(noticeIDs(reopened.activeNotices()), "whatsnew-v0.2.0") {
		t.Fatal("undismissed what's new should stay listed")
	}
	persistNoticeDismissal(t, reopened, "whatsnew-v0.2.0")
	if contains(noticeIDs(reopened.activeNotices()), "whatsnew-v0.2.0") {
		t.Fatal("dismissed what's new still listed")
	}
}

func TestFreshInstallNeverShowsWhatsNew(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	if got := m.startupNotice(); got != noticeWelcome {
		t.Fatalf("want welcome, got %q", got)
	}
	for _, id := range noticeIDs(m.activeNotices()) {
		if strings.HasPrefix(id, "whatsnew-") {
			t.Fatalf("fresh install must not list what's new, got %v", noticeIDs(m.activeNotices()))
		}
	}
}

func TestFeedMessagesBecomeNotices(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.notices.feedMessages = []feed.Message{{
		ID:     "feed-holdoff",
		Banner: "known issue in v0.2.0",
		Title:  "Hold off",
		Body:   []string{"details"},
		URL:    "https://example.com",
	}}

	ids := noticeIDs(m.activeNotices())
	if !contains(ids, "feed-holdoff") {
		t.Fatalf("feed message should be a notice, got %v", ids)
	}

	persistNoticeDismissal(t, m, "feed-holdoff")
	if contains(noticeIDs(m.activeNotices()), "feed-holdoff") {
		t.Fatal("dismissed feed notice still active")
	}
	reopened := noticeModel(st, "v0.2.0")
	reopened.notices.feedMessages = m.notices.feedMessages
	if contains(noticeIDs(reopened.activeNotices()), "feed-holdoff") {
		t.Fatal("feed dismissal must survive restart")
	}
}

func TestFeedUsesCanonicalTitleInModal(t *testing.T) {
	m := modalModel(t)
	m.notices.feedMessages = []feed.Message{{
		ID:     "feed-canonical",
		Banner: "legacy compact copy",
		Title:  "One title everywhere",
		Body:   []string{"details"},
	}}
	persistNoticeDismissal(t, m, noticeWelcome)

	m.openNotices("feed-canonical")
	modal := ansi.Strip(preparedView(m))
	if !strings.Contains(modal, "One title everywhere") || strings.Contains(modal, "legacy compact copy") {
		t.Fatalf("modal did not use the canonical title:\n%s", modal)
	}
}

func TestBugReportURLPrefillsVersion(t *testing.T) {
	got := bugReportURL("v0.2.0")
	if !strings.HasPrefix(got, "https://github.com/YoanWai/agent-manager/issues/new") {
		t.Fatalf("bug report should open the new-issue page, got %q", got)
	}
	if !strings.Contains(got, "template=bug_report.yml") {
		t.Fatalf("bug report should use the bug form, got %q", got)
	}
	if !strings.Contains(got, "version=v0.2.0") {
		t.Fatalf("issue URL should carry the version, got %q", got)
	}
}

func TestFeatureRequestURLUsesFeatureTemplate(t *testing.T) {
	got := featureRequestURL()
	if !strings.Contains(got, "template=feature_request.yml") {
		t.Fatalf("feature request should use the feature form, got %q", got)
	}
	if strings.Contains(got, "template=bug_report.yml") {
		t.Fatalf("feature request opened the bug form: %q", got)
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func footModel(t *testing.T) *Model {
	t.Helper()
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.workspace.snap = sysstat.Snapshot{
		CPUPercent: 42, CPUOK: true,
		MemPercent: 63, MemOK: true, MemUsed: 10 << 30, MemTotal: 16 << 30,
		DiskPercent: 71, DiskOK: true, DiskAvailable: 120 << 30,
	}
	return m
}

func TestRailFootOnlyShowsComputerStats(t *testing.T) {
	for _, full := range []bool{false, true} {
		for _, width := range []int{34, 70, 120} {
			m := footModel(t)
			m.prefs.fullLayout = full
			foot := strings.Join(m.railFootLines(width), "\n")
			if !strings.Contains(ansi.Strip(foot), "cpu") || strings.Contains(ansi.Strip(foot), "messages") {
				t.Fatalf("full=%v width=%d: stats foot contains messages or loses readings:\n%s", full, width, ansi.Strip(foot))
			}
			for _, n := range m.activeNotices() {
				m.dismissNotice(n.id)
			}
			if got := strings.Join(m.railFootLines(width), "\n"); got != foot {
				t.Fatalf("full=%v width=%d: notices changed the stats foot", full, width)
			}
		}
	}
}

func TestRailFootLinesFitWidth(t *testing.T) {
	m := footModel(t)
	m.update.latest = "v0.9.9"
	m.update.url = "https://example.com"
	for _, width := range []int{40, 55, 70} {
		for _, line := range m.railFootLines(width) {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line overflows at %d: %q", width, got, ansi.Strip(line))
			}
		}
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func modalModel(t *testing.T) *Model {
	t.Helper()
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeNotices
	return m
}

func TestOpenNoticesFromList(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeList
	m.handleKey(key("M"))
	if m.mode != modeNotices {
		t.Fatalf("M should open the notices modal, mode=%v", m.mode)
	}
}

func TestNoticesViewListsAndDetails(t *testing.T) {
	m := modalModel(t)
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"messages", "Welcome to agent-manager", "Settings (s)", "A bug or an idea?", "dismiss"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("modal missing %q:\n%s", want, frame)
		}
	}

	for _, terminal := range []int{100, 40} {
		m.layout.width = terminal
		frame := ansi.Strip(preparedView(m))
		var widths []int
		for _, line := range strings.Split(frame, "\n") {
			trimmed := strings.TrimRight(line, " ")
			if strings.ContainsAny(trimmed, "╭╰│") {
				widths = append(widths, len([]rune(trimmed)))
			}
		}
		if len(widths) == 0 {
			t.Fatalf("width %d: no frame rows found", terminal)
		}
		for i, width := range widths {
			if width != widths[0] {
				t.Fatalf("width %d: frame rows must align: row %d is %d, first is %d\n%s", terminal, i, width, widths[0], frame)
			}
		}
	}
}

func TestNoticesLongBodyWrapsFully(t *testing.T) {
	m := modalModel(t)
	m.notices.feedMessages = []feed.Message{{
		ID:     "feed-long",
		Banner: "long banner",
		Title:  "Long body message",
		Body: []string{
			"Each group can choose inherit, on, or off for spawning sessions into their own git worktree, set when creating a group or editing it later; children inherit from their parents, and the global setting stays the root fallback.",
		},
	}}
	m.openNotices("feed-long")
	frame := ansi.Strip(preparedView(m))
	for _, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, "╭") {
			continue
		}
		if got := lipgloss.Width(strings.TrimSpace(line)); got <= noticeModalInner+4 {
			t.Fatalf("wide terminal must widen the modal past %d, got %d", noticeModalInner+4, got)
		}
		break
	}
	for _, word := range []string{"worktree,", "parents,", "fallback."} {
		if !strings.Contains(frame, word) {
			t.Fatalf("long body must wrap, %q missing:\n%s", word, frame)
		}
	}
	for _, line := range strings.Split(frame, "\n") {
		if got := lipgloss.Width(line); got > m.layout.width {
			t.Fatalf("line overflows terminal at %d: %q", got, line)
		}
	}
}

func TestNoticesShortTerminalKeepsFrameAndHint(t *testing.T) {
	m := modalModel(t)
	m.layout.width, m.layout.height = 30, 12
	m.notices.feedMessages = []feed.Message{{
		ID:     "feed-long",
		Banner: "long banner",
		Title:  "Long body message",
		Body: []string{
			"Each group can choose inherit, on, or off for spawning sessions into their own git worktree, set when creating a group or editing it later.",
		},
	}}
	m.openNotices("feed-long")
	lines := strings.Split(ansi.Strip(preparedView(m)), "\n")
	if len(lines) > m.layout.height {
		t.Fatalf("frame must fit %d rows, got %d", m.layout.height, len(lines))
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "╰") {
		t.Fatalf("short terminal ate the modal's bottom border:\n%s", joined)
	}
	if !strings.Contains(joined, "↑↓ pick") {
		t.Fatalf("short terminal ate the key hint:\n%s", joined)
	}
	if !strings.Contains(joined, "┃") {
		t.Fatalf("a clipped body must show its scrollbar:\n%s", joined)
	}
}

func TestNoticesBodyScrollIsBoundedAndVisible(t *testing.T) {
	m := modalModel(t)
	m.layout.width, m.layout.height = 70, 14
	var body []string
	for i := 0; i < 30; i++ {
		body = append(body, fmt.Sprintf("change line %02d", i))
	}
	m.notices.feedMessages = []feed.Message{{ID: "feed-scroll", Banner: "scroll", Title: "Scrollable summary", Body: body}}
	m.openNotices("feed-scroll")

	before := ansi.Strip(preparedView(m))
	if !strings.Contains(before, "┃") {
		t.Fatalf("a clipped body shows its scrollbar:\n%s", before)
	}
	m.handleNoticesKey(key("pgdown"))
	if m.notices.noticeScroll == 0 {
		t.Fatalf("page down did not move the summary:\n%s", ansi.Strip(preparedView(m)))
	}
	limit := m.noticeScrollLimit(m.activeNotices())
	for i := 0; i < 20; i++ {
		m.handleNoticesKey(key("pgdown"))
	}
	if m.notices.noticeScroll != limit {
		t.Fatalf("scroll offset = %d, want bounded limit %d", m.notices.noticeScroll, limit)
	}
}

func TestReleaseSummaryMarksOmittedChangesAndPartialRange(t *testing.T) {
	n := notice{
		releases:      []update.Release{uiReleaseWithTotal("v0.3.0", 4, "Visible change")},
		rangeComplete: false,
	}
	body := ansi.Strip(strings.Join(renderNoticeBody(n, noticeModalInner), "\n"))
	for _, want := range []string{"OTHER · 1", "+3 more in the full notes", "catalog covers part of this range"} {
		if !strings.Contains(body, want) {
			t.Fatalf("partial summary missing %q:\n%s", want, body)
		}
	}
}

func TestNoticesManualRefreshWaitsForBothSources(t *testing.T) {
	m := modalModel(t)
	_, cmd := m.handleNoticesKey(key("r"))
	if cmd == nil || !m.update.refreshing || m.update.refreshPending != 2 {
		t.Fatalf("refresh did not start both sources: refreshing=%v pending=%d", m.update.refreshing, m.update.refreshPending)
	}
	m.Update(updateMsg{manual: true, releases: []update.Release{uiRelease("v0.2.0", "Current")}})
	if !m.update.refreshing || m.update.refreshPending != 1 {
		t.Fatalf("first result ended refresh early: refreshing=%v pending=%d", m.update.refreshing, m.update.refreshPending)
	}
	m.Update(feedMsg{manual: true})
	if m.update.refreshing || m.update.refreshPending != 0 {
		t.Fatalf("refresh did not finish after both results: refreshing=%v pending=%d", m.update.refreshing, m.update.refreshPending)
	}
}

func TestNoticesDismissAdvancesAndCloses(t *testing.T) {
	m := modalModel(t)
	total := len(m.activeNotices())
	for i := 0; i < total; i++ {
		m.handleNoticesKey(key("x"))
	}
	if m.mode != modeList {
		t.Fatal("dismissing the last notice should close the modal")
	}
	if len(m.activeNotices()) != 0 {
		t.Fatal("all notices should be gone")
	}
}

func TestNoticesEnterOpensURL(t *testing.T) {
	m := modalModel(t)
	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	want := m.activeNotices()[m.notices.noticeCursor].url
	if want == "" {
		t.Fatal("selected notice needs a url for this test")
	}
	_, cmd := m.handleNoticesKey(key("enter"))
	if opened != "" {
		t.Fatal("browser started during Update")
	}
	m.applyCmd(t, cmd)
	if opened != want {
		t.Fatalf("opened %q, want %q", opened, want)
	}
}

// Over SSH the opener would start a browser on the remote host, out of the
// user's sight, so the link goes to the page instead.
func TestOpenLinkOverSSHShowsThePage(t *testing.T) {
	overSSH(t)
	opened := ""
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	target := "https://example.com/issues/new?template=bug_report.yml"
	msg := openLink(target)()
	if opened != "" {
		t.Fatalf("the remote host opened %q", opened)
	}
	if msg != (linkPageMsg{url: target}) {
		t.Fatalf("openLink() = %#v, want the page for %q", msg, target)
	}
}

func TestBrowserCommandsHonorBrowserCandidates(t *testing.T) {
	target := "https://example.com/search?q=a b;still-an-argument"
	commands := browserCommands("linux", `missing:'/opt/Remote Browser/bin/open' --new-tab=%s:remote`, target)
	want := [][]string{
		{"missing", target},
		{"/opt/Remote Browser/bin/open", "--new-tab=" + target},
		{"remote", target},
		{"xdg-open", target},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %d, want %d", len(commands), len(want))
	}
	for i := range commands {
		if !slices.Equal(commands[i].Args, want[i]) {
			t.Errorf("command %d = %q, want %q", i, commands[i].Args, want[i])
		}
	}
}

func TestOpenBrowserTriesCandidatesBeforeXDGOpen(t *testing.T) {
	var attempted []string
	err := openBrowserWith("linux", "missing:remote", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "remote" {
			return nil
		}
		return errors.New("not found")
	})
	if err != nil {
		t.Fatalf("openBrowserWith() error = %v", err)
	}
	if want := []string{"missing", "remote"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserFallsBackToXDGOpen(t *testing.T) {
	var attempted []string
	err := openBrowserWith("linux", "missing", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "xdg-open" {
			return nil
		}
		return errors.New("not found")
	})
	if err != nil {
		t.Fatalf("openBrowserWith() error = %v", err)
	}
	if want := []string{"missing", "xdg-open"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserReportsAllFailures(t *testing.T) {
	missingErr := errors.New("missing")
	xdgErr := errors.New("xdg failed")
	var attempted []string
	err := openBrowserWith("linux", "missing", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "xdg-open" {
			return xdgErr
		}
		return missingErr
	})
	if !errors.Is(err, missingErr) || !errors.Is(err, xdgErr) {
		t.Fatalf("openBrowserWith() error = %v, want both failures", err)
	}
	if want := []string{"missing", "xdg-open"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserContinuesAfterRuntimeFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix true and false commands")
	}
	var attempted []string
	err := openBrowserWith("linux", "false:true", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("openBrowserWith() error = %v", err)
	}
	if want := []string{"false", "true"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserFailureCopiesURL(t *testing.T) {
	m := modalModel(t)
	openBrowser = func(string) error { return errors.New("no browser available") }
	var copied string
	copyBrowserURL = func(target string) error {
		copied = target
		return nil
	}
	t.Cleanup(func() {
		openBrowser = defaultOpenBrowser
		copyBrowserURL = clipboard.WriteText
	})

	target := "https://example.com/manual"
	m.applyCmd(t, openLink(target))
	if copied != target {
		t.Fatalf("copied %q, want %q", copied, target)
	}
	if !strings.Contains(m.errBar.text, "URL copied") || !strings.Contains(m.errBar.text, "no browser available") {
		t.Fatalf("failure = %q, want copy outcome and cause", m.errBar.text)
	}
}

func TestOpenBrowserAndCopyFailureNamesURL(t *testing.T) {
	m := modalModel(t)
	openBrowser = func(string) error { return errors.New("no browser available") }
	copyBrowserURL = func(string) error { return errors.New("no clipboard available") }
	t.Cleanup(func() {
		openBrowser = defaultOpenBrowser
		copyBrowserURL = clipboard.WriteText
	})

	target := "https://example.com/manual"
	m.applyCmd(t, openLink(target))
	if !strings.Contains(m.errBar.text, target) || !strings.Contains(m.errBar.text, "no clipboard available") {
		t.Fatalf("failure = %q, want URL and copy cause", m.errBar.text)
	}
}

func TestNoticesEscCloses(t *testing.T) {
	m := modalModel(t)
	m.handleNoticesKey(key("esc"))
	if m.mode != modeList {
		t.Fatal("esc should close the modal")
	}
	if len(m.activeNotices()) == 0 {
		t.Fatal("esc must not dismiss anything")
	}
}

func TestDevBuildNeverGreets(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "dev")
	m.openStartupNotice()
	if m.mode == modeNotices {
		t.Fatal("a dev build must not open the startup modal")
	}
	if seen, _ := st.Setting(lastSeenVersionSetting); seen != "" {
		t.Fatalf("a dev build must not advance the stored version, got %q", seen)
	}
}

func TestStartupOpensNoticesModalOncePerVersion(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.openStartupNotice()
	if m.mode != modeNotices {
		t.Fatalf("first launch should open the notices modal, mode=%v", m.mode)
	}
	if selected := m.activeNotices()[m.notices.noticeCursor]; selected.id != noticeWelcome {
		t.Fatalf("welcome should be selected, got %q", selected.id)
	}

	again := noticeModel(st, "v0.2.0")
	again.openStartupNotice()
	if again.mode == modeNotices {
		t.Fatal("a later launch of the same version must not reopen the modal")
	}
}

func TestLateReleaseKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	m.handleNoticesKey(key("down"))
	selected := m.activeNotices()[m.notices.noticeCursor].id

	m.Update(updateMsg{latest: "v9.9.9", url: "https://example.com"})
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != selected {
		t.Fatalf("selection moved from %q to %q when the release arrived", selected, got)
	}
}

func TestLateDismissedReleaseKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	persistNoticeDismissal(t, m, "update-v9.9.9")
	selected := m.activeNotices()[m.notices.noticeCursor].id

	m.Update(updateMsg{latest: "v9.9.9", url: "https://example.com"})
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != selected {
		t.Fatalf("a dismissed release must not move the selection, went from %q to %q", selected, got)
	}
}

func TestLateFeedKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	m.handleNoticesKey(key("down"))
	selected := m.activeNotices()[m.notices.noticeCursor].id

	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-late", Banner: "late", Title: "Late"}}})
	if !contains(noticeIDs(m.activeNotices()), "feed-late") {
		t.Fatal("feed message should have landed")
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != selected {
		t.Fatalf("selection moved from %q to %q when the feed arrived", selected, got)
	}
}

func TestNewFeedOpensNoticesModal(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		m := footModel(t)
		m.mode = modeList
		m.prefs.hideStats = hidden
		m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
		if m.mode != modeNotices {
			t.Fatalf("hidden=%v: a new feed message should open the modal, mode=%v", hidden, m.mode)
		}
		if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
			t.Fatalf("new message should be selected, got %q", got)
		}
	}
}

func TestSameFeedDoesNotReopenNoticesModal(t *testing.T) {
	m := footModel(t)
	m.mode = modeList
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	m.handleNoticesKey(key("esc"))
	if m.mode != modeList {
		t.Fatal("esc should close the modal")
	}

	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeList {
		t.Fatal("the same message must not reopen after esc")
	}
}

func TestDismissedFeedDoesNotOpenNoticesModal(t *testing.T) {
	m := footModel(t)
	m.mode = modeList
	persistNoticeDismissal(t, m, "feed-new")

	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeList {
		t.Fatal("a dismissed message must not open the modal")
	}
}

func TestNewFeedDoesNotStealFocus(t *testing.T) {
	m := footModel(t)
	m.mode = modeFocus

	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeFocus {
		t.Fatalf("a new message must not steal an open session, mode=%v", m.mode)
	}
}

func TestPendingFeedDoesNotOpenDuringQuit(t *testing.T) {
	m := footModel(t)
	m.mode = modeFocus
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.notices.pendingNotice != "feed-new" {
		t.Fatalf("pending notice = %q, want feed-new", m.notices.pendingNotice)
	}

	m.mode = modeList
	m.effects.quitting = true
	m.flushPendingNotice()
	if m.mode != modeList {
		t.Fatalf("pending feed opened while quitting: mode=%v", m.mode)
	}
}

func TestAdditionalFeedIdOpensNoticesModal(t *testing.T) {
	m := footModel(t)
	m.mode = modeList
	m.notices.feedMessages = []feed.Message{{ID: "feed-old", Banner: "old", Title: "Old"}}

	m.Update(feedMsg{messages: []feed.Message{
		{ID: "feed-old", Banner: "old", Title: "Old"},
		{ID: "feed-new", Banner: "new", Title: "Just in"},
	}})
	if m.mode != modeNotices {
		t.Fatalf("a new id should open the modal, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestFeedDuringFocusOpensOnLeave(t *testing.T) {
	m := footModel(t)
	m.mode = modeFocus
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeFocus {
		t.Fatalf("mode=%v, want focus until leave", m.mode)
	}

	m.leaveFocus()
	if m.mode != modeNotices {
		t.Fatalf("returning to the list should open the new message, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestFeedDuringSettingsOpensOnClose(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeSettings
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeSettings {
		t.Fatalf("mode=%v, want settings until close", m.mode)
	}

	m.mode = modeList
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if m.mode != modeNotices {
		t.Fatalf("closing settings should open the new message, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestExpiredPendingNoticeDoesNotOpen(t *testing.T) {
	m := footModel(t)
	m.mode = modeFocus
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	m.Update(feedMsg{})
	m.leaveFocus()
	if m.mode != modeList {
		t.Fatalf("a notice that left the feed must not open, mode=%v", m.mode)
	}
}

func TestNewFeedWaitsForSearchToClose(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeList
	m.rail.SetSearch(m.rail.Search(), true)
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeList || !m.rail.Searching() {
		t.Fatalf("search should keep the modal closed, mode=%v searching=%v", m.mode, m.rail.Searching())
	}

	m.rail.SetSearch(m.rail.Search(), false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if m.mode != modeNotices {
		t.Fatalf("closing search should open the new message, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestNewFeedWaitsForQuickBar(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeList
	m.quick.active = true
	m.quick.input = textarea.New()
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeList || !m.quick.active {
		t.Fatalf("quick bar should keep the modal closed, mode=%v quick=%v", m.mode, m.quick.active)
	}

	m.quick.active = false
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if m.mode != modeNotices {
		t.Fatalf("closing the quick bar should open the new message, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestNewFeedWaitsForResize(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeList
	m.layout.split.resizeMode = true
	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeList || !m.layout.split.resizeMode {
		t.Fatalf("resize should keep the modal closed, mode=%v resize=%v", m.mode, m.layout.split.resizeMode)
	}

	m.layout.split.resizeMode = false
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if m.mode != modeNotices {
		t.Fatalf("leaving resize should open the new message, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestNewUpdateOpensNoticesModal(t *testing.T) {
	m := footModel(t)
	m.mode = modeList

	m.Update(updateMsg{latest: "v0.3.0", url: "https://example.com"})
	if m.mode != modeNotices {
		t.Fatalf("a new update notice should open the modal, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "update-v0.3.0" {
		t.Fatalf("update notice should be selected, got %q", got)
	}
}

func TestKnownLatestDoesNotReopenOnFetch(t *testing.T) {
	m := footModel(t)
	m.mode = modeList
	m.update.latest = "v0.3.0"
	m.update.url = "https://example.com"
	m.indexReleaseRanges()

	m.Update(updateMsg{latest: "v0.3.0", url: "https://example.com"})
	if m.mode != modeList {
		t.Fatal("a latest already on the list must not pop the modal again")
	}
}

func TestNewerReleaseOpensNoticesModal(t *testing.T) {
	m := footModel(t)
	m.mode = modeList
	m.update.latest = "v0.3.0"
	m.update.url = "https://example.com"
	m.indexReleaseRanges()

	m.Update(updateMsg{latest: "v0.4.0", url: "https://example.com"})
	if m.mode != modeNotices {
		t.Fatalf("a newer tag should open the modal, mode=%v", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != "update-v0.4.0" {
		t.Fatalf("new release should be selected, got %q", got)
	}
}

func TestFeedArrivingDuringWelcomeStaysOnWelcome(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.openStartupNotice()
	if m.mode != modeNotices {
		t.Fatal("first launch should open welcome")
	}

	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.mode != modeNotices {
		t.Fatalf("mode=%v, want notices", m.mode)
	}
	if got := m.activeNotices()[m.notices.noticeCursor].id; got != noticeWelcome {
		t.Fatalf("welcome should stay selected, got %q", got)
	}
	if !contains(noticeIDs(m.activeNotices()), "feed-new") {
		t.Fatal("feed should still be listed")
	}
}

func TestAutoOpenFeedAndUpdateFrames(t *testing.T) {
	feedModel := footModel(t)
	feedModel.layout.width, feedModel.layout.height = 100, 34
	feedModel.mode = modeList
	feedModel.Update(feedMsg{messages: []feed.Message{{
		ID:     "feed-holdoff",
		Banner: "Hold off on v0.36",
		Title:  "Hold off on v0.36",
		Body:   []string{"Sessions may drop.", "Stay on v0.35 until the fix."},
		URL:    "https://github.com/YoanWai/agent-manager/issues/1",
	}}})
	feedFrame := ansi.Strip(preparedView(feedModel))
	for _, want := range []string{"messages", "Hold off on v0.36", "Sessions may drop", "Stay on v0.35", "esc"} {
		if !strings.Contains(feedFrame, want) {
			t.Fatalf("feed modal missing %q:\n%s", want, feedFrame)
		}
	}

	upd := footModel(t)
	upd.layout.width, upd.layout.height = 100, 34
	upd.mode = modeList
	upd.Update(updateMsg{
		latest:   "v0.3.0",
		url:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
		releases: []update.Release{uiRelease("v0.3.0", "Notices: Open the modal on a new message")},
	})
	updFrame := ansi.Strip(preparedView(upd))
	for _, want := range []string{"messages", "v0.3.0 available", "You are on v0.2.0", "u update", "x dismiss"} {
		if !strings.Contains(updFrame, want) {
			t.Fatalf("update modal missing %q:\n%s", want, updFrame)
		}
	}
}

func TestUpdateNoticeAppliesOnU(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.update.latest = "v0.3.0"
	m.openNotices("update-v0.3.0")
	if m.mode != modeNotices || m.notices.noticeCursor != 0 {
		t.Fatalf("update notice should open selected, mode=%v cursor=%d", m.mode, m.notices.noticeCursor)
	}

	applied := ""
	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest: "v0.6.0",
			URL:    "https://github.com/YoanWai/agent-manager/releases/tag/v0.6.0",
			Releases: []update.Release{
				uiRelease("v0.6.0", "Newest release"),
				uiRelease("v0.2.0", "Current release"),
			},
		}, nil
	}
	applyUpdate = func(_ context.Context, tag, execPath string) error {
		applied = tag + " " + execPath
		return nil
	}

	_, cmd := m.handleNoticesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("u on the update notice should start the update")
	}
	if !m.update.applying {
		t.Fatal("applying should be marked while the download runs")
	}
	msg, ok := cmd().(updateAppliedMsg)
	if !ok {
		t.Fatalf("cmd returned %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("apply: %v", msg.err)
	}
	if !strings.HasPrefix(applied, "v0.6.0 ") {
		t.Fatalf("applyUpdate saw %q", applied)
	}
	updated, quit := m.Update(msg)
	m = updated.(*Model)
	if m.update.applying {
		t.Fatal("applying should clear once the swap lands")
	}
	if m.RestartPath() == "" {
		t.Fatal("a successful swap must set the restart path")
	}
	if quit == nil {
		t.Fatal("a successful swap must quit so main can exec the new build")
	}
}

func TestUpdateNoticeApplyFailureSurfaces(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.update.latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	orig := applyUpdate
	defer func() { applyUpdate = orig }()
	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest:   "v0.3.0",
			URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
			Releases: []update.Release{uiRelease("v0.3.0", "Newest release"), uiRelease("v0.2.0", "Current release")},
		}, nil
	}
	applyUpdate = func(_ context.Context, _, _ string) error {
		return errors.New("permission denied")
	}
	_, cmd := m.handleNoticesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	updated, _ := m.Update(cmd().(updateAppliedMsg))
	m = updated.(*Model)
	if m.update.applying {
		t.Fatal("applying should clear on failure")
	}
	if m.RestartPath() != "" {
		t.Fatal("a failed swap must not restart")
	}
	if !strings.Contains(m.errBar.text, "permission denied") {
		t.Fatalf("failure should surface, err=%q", m.errBar.text)
	}
}

func TestUpdateRefreshFailureDoesNotInstallCachedTag(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.update.latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{}, errors.New("github unavailable")
	}
	origApply := applyUpdate
	defer func() { applyUpdate = origApply }()
	called := false
	applyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	_, cmd := m.handleNoticesKey(key("u"))
	updated, _ := m.Update(cmd().(updateAppliedMsg))
	m = updated.(*Model)
	if called || m.RestartPath() != "" {
		t.Fatal("a failed latest-release refresh must not install a stale tag")
	}
	if !strings.Contains(m.errBar.text, "github unavailable") {
		t.Fatalf("refresh error not surfaced: %q", m.errBar.text)
	}
}

func TestUpdateActionClearsStaleNoticeWhenAlreadyCurrent(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.3.0")
	m.update.latest = "v0.4.0"
	m.openNotices("update-v0.4.0")

	origRefresh := refreshUpdatesForApply
	defer func() { refreshUpdatesForApply = origRefresh }()
	refreshUpdatesForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{}, nil
	}
	origApply := applyUpdate
	defer func() { applyUpdate = origApply }()
	called := false
	applyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	_, cmd := m.handleNoticesKey(key("u"))
	updated, quit := m.Update(cmd().(updateAppliedMsg))
	m = updated.(*Model)
	if called || quit != nil || m.update.latest != "" || m.RestartPath() != "" {
		t.Fatal("an already-current refresh should clear the stale notice without installing")
	}
	if m.errBar.text != "already up to date" {
		t.Fatalf("status = %q", m.errBar.text)
	}
}

func TestUOutsideUpdateNoticeDoesNothing(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.openNotices(noticeWelcome)
	_, cmd := m.handleNoticesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd != nil || m.update.applying {
		t.Fatal("u on a plain notice must not start an update")
	}
}

func TestNoticesTinyTerminalStaysInside(t *testing.T) {
	m := modalModel(t)
	for _, width := range []int{5, 12, 30} {
		m.layout.width, m.layout.height = width, 10
		for _, line := range strings.Split(ansi.Strip(preparedView(m)), "\n") {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line overflows at %d: %q", width, got, line)
			}
		}
	}
}

func uiRelease(version string, changes ...string) update.Release {
	return uiReleaseWithTotal(version, len(changes), changes...)
}

func uiReleaseWithTotal(version string, total int, changes ...string) update.Release {
	release := update.Release{
		Version:      version,
		URL:          "https://github.com/YoanWai/agent-manager/releases/tag/" + version,
		TotalChanges: total,
	}
	for _, text := range changes {
		release.Changes = append(release.Changes, update.Change{Kind: update.KindOther, Text: text})
	}
	return release
}

func TestUpdateDelegatesToPackageManager(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.update.latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	origDetect := detectManager
	defer func() { detectManager = origDetect }()
	detectManager = func(string) update.Manager {
		return update.Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	}

	_, cmd := m.handleNoticesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("u on a package-manager install should start the delegated upgrade")
	}
	if !m.update.applying {
		t.Fatal("applying should be marked while the manager runs")
	}
}

func TestDelegatedUpdateResult(t *testing.T) {
	manager := update.Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	// A base name no PATH holds, so RestartTarget deterministically falls
	// back to execPath on any machine.
	execPath := "/x/agent-manager-delegated-test"

	msg := delegatedUpdateResult(manager, execPath, nil)
	if msg.err != nil {
		t.Fatalf("success reported %v", msg.err)
	}
	if msg.path != execPath {
		t.Fatalf("restart path = %q, want %q", msg.path, execPath)
	}

	failed := delegatedUpdateResult(manager, execPath, errors.New("exit status 1"))
	if failed.err == nil || !strings.Contains(failed.err.Error(), "brew upgrade --cask yoanwai/tap/agent-manager") {
		t.Fatalf("failure should name the command, got %v", failed.err)
	}
	if failed.path != "" {
		t.Fatal("a failed upgrade must not restart")
	}
}

func TestUpdateWithoutRunnableManagerSurfacesAdvice(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.update.latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	origDetect := detectManager
	defer func() { detectManager = origDetect }()
	advice := "installed with pacman; install an AUR helper first, then: yay -S agent-manager-bin"
	detectManager = func(string) update.Manager {
		return update.Manager{Name: "pacman", Advice: advice}
	}

	_, cmd := m.handleNoticesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("u should still answer with the advice")
	}
	msg, ok := cmd().(updateAppliedMsg)
	if !ok || msg.err == nil || msg.err.Error() != advice {
		t.Fatalf("want the advice as the error, got %#v", cmd())
	}
}

func TestArrowStepNoticeListedUntilDismissed(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	found := false
	for _, n := range m.activeNotices() {
		if n.id == noticeArrowStep {
			found = true
		}
	}
	if !found {
		t.Fatal("arrow-step beta notice missing from active notices")
	}
	persistNoticeDismissal(t, m, noticeArrowStep)
	for _, n := range m.activeNotices() {
		if n.id == noticeArrowStep {
			t.Fatal("dismissed notice still listed")
		}
	}
}

// Full screen condenses the machine readings to one line.
func TestFullLayoutFootLine(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	foot := ansi.Strip(strings.Join(m.railFootLines(m.layout.width-1), "\n"))
	for _, want := range []string{"cpu 22%", "mem 75%", "net"} {
		if !strings.Contains(foot, want) {
			t.Errorf("condensed foot line misses %q:\n%s", want, foot)
		}
	}
	if strings.Contains(foot, "messages") {
		t.Fatalf("no notices should mean no messages count:\n%s", foot)
	}
	if lines := m.railFootLines(m.layout.width - 1); len(lines) != 1 {
		t.Fatalf("full screen foot should be one line, got %d", len(lines))
	}
}

func TestLayoutsCanHideStats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		full  bool
		width int
	}{
		{name: "split", width: 36},
		{name: "full", full: true, width: 119},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := footModel(t)
			m.layout.width, m.layout.height = 120, 34
			m.prefs.fullLayout = tc.full
			m.prefs.hideStats = true
			if foot := m.railFootLines(tc.width); len(foot) > 0 {
				t.Fatalf("hidden stats still paint %d lines:\n%s", len(foot), ansi.Strip(strings.Join(foot, "\n")))
			}
			rail := m.railLines(tc.width, m.listBodyHeight())
			if len(rail) != m.listBodyHeight() {
				t.Fatalf("footerless rail = %d rows, body is %d", len(rail), m.listBodyHeight())
			}
			for _, line := range rail {
				if line.rule {
					t.Fatal("hidden stats left their separator behind")
				}
			}
		})
	}
}

func TestReleaseSummaryShowsTheListUnderTheHighlights(t *testing.T) {
	release := uiReleaseWithTotal("v0.34.0", 17, "UI: Full screen sessions mode")
	release.Highlights = []string{"The session list can take the whole terminal"}
	body := ansi.Strip(strings.Join(renderNoticeBody(notice{releases: []update.Release{release}, rangeComplete: true}, noticeModalInner), "\n"))

	if !strings.Contains(body, "• The session list can take the whole terminal") {
		t.Fatalf("highlights missing:\n%s", body)
	}
	for _, want := range []string{"OTHER · 1", "• UI: Full screen sessions mode", "+16 more in the full notes"} {
		if !strings.Contains(body, want) {
			t.Fatalf("the generated list follows the highlights, missing %q:\n%s", want, body)
		}
	}
}

func TestReleaseSummaryShowsThanksUnderHighlights(t *testing.T) {
	release := uiReleaseWithTotal("v0.35.0", 7, "UI: Add header and stats visibility settings")
	release.Highlights = []string{"Revive without a captured id opens the tool's own picker"}
	release.Thanks = []string{
		"@dolutech asked in #388 and built the picker (#400)",
		"@fruch reported that a live rename moved the worktree (#418)",
	}
	body := ansi.Strip(strings.Join(renderNoticeBody(notice{releases: []update.Release{release}, rangeComplete: true}, noticeModalInner), "\n"))

	for _, want := range []string{
		"• Revive without a captured id opens the tool's own picker",
		"THANK YOU",
		"• @dolutech asked in #388 and built the picker (#400)",
		"• @fruch reported that a live rename moved the worktree (#418)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("thanks under highlights missing %q:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "Add header and stats visibility settings") {
		t.Fatalf("the generated list follows the highlights:\n%s", body)
	}
}

func TestReleaseSummaryShowsThanksUnderGeneratedList(t *testing.T) {
	release := uiRelease("v0.33.0", "UI: A change")
	release.Thanks = []string{"@pandysp asked for a way to put the preview away in #357"}
	body := ansi.Strip(strings.Join(renderNoticeBody(notice{
		releases:      []update.Release{release},
		rangeComplete: true,
	}, noticeModalInner), "\n"))
	for _, want := range []string{
		"OTHER · 1",
		"• UI: A change",
		"THANK YOU",
		"• @pandysp asked for a way to put the preview away in #357",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("thanks under generated list missing %q:\n%s", want, body)
		}
	}
}

func TestReleaseSummaryFallsBackToTheGeneratedList(t *testing.T) {
	body := ansi.Strip(strings.Join(renderNoticeBody(notice{
		releases:      []update.Release{uiRelease("v0.33.0", "UI: A change")},
		rangeComplete: true,
	}, noticeModalInner), "\n"))
	if !strings.Contains(body, "OTHER · 1") || !strings.Contains(body, "• UI: A change") {
		t.Fatalf("release without highlights lost its generated list:\n%s", body)
	}
}

func TestEmptyNoticesPanelStillOpensAndOffersRefresh(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.mode = modeList
	for _, n := range m.activeNotices() {
		persistNoticeDismissal(t, m, n.id)
	}
	if len(m.activeNotices()) != 0 {
		t.Fatal("test needs an empty panel")
	}

	m.handleKey(key("M"))
	if m.mode != modeNotices {
		t.Fatalf("M should open an empty panel, mode=%v", m.mode)
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"nothing new", "r refresh"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("empty panel missing %q:\n%s", want, frame)
		}
	}

	if _, cmd := m.handleNoticesKey(key("r")); cmd == nil || !m.update.refreshing {
		t.Fatalf("r unreachable on an empty panel: refreshing=%v", m.update.refreshing)
	}
	if !strings.Contains(ansi.Strip(preparedView(m)), "refreshing releases and messages") {
		t.Fatalf("empty panel hides the refresh it started:\n%s", ansi.Strip(preparedView(m)))
	}
}

// The welcome card's session-key row is laid out on the card's columns and
// follows the table, so a first run under a remapped config reads right.
func TestWelcomeSessionKeysLineFollowsTheKeyTable(t *testing.T) {
	m := &Model{
		services: services{
			keys: keybind.DefaultSession(),
		},
	}
	if got := m.welcomeSessionKeysLine(); got != "ctrl+q back to the manager   ctrl+r review its diff" {
		t.Errorf("default line = %q", got)
	}
	m.services.keys = sessionOf(t, []string{"f9"}, nil, []string{"f3"})
	if got := m.welcomeSessionKeysLine(); got != "f9     back to the manager" {
		t.Errorf("review off line = %q", got)
	}
}

func TestWelcomeSessionKeysLineOmitsDisabledActions(t *testing.T) {
	m := &Model{services: services{keys: sessionOf(t, nil, []string{"ctrl+r"}, nil)}}
	got := m.welcomeSessionKeysLine()
	if strings.Contains(got, "back to the manager") || !strings.Contains(got, "ctrl+r review its diff") {
		t.Fatalf("detach off line = %q", got)
	}

	m.services.keys = sessionOf(t, nil, nil, nil)
	if got := m.welcomeSessionKeysLine(); got != "" {
		t.Fatalf("all session actions off line = %q", got)
	}
}

// The welcome's list keys follow the list table the same way: a moved key
// is named where it moved to, and an action turned off is left out.
func TestWelcomeBodyFollowsTheListTable(t *testing.T) {
	m := &Model{
		services: services{
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
	}
	body := strings.Join(m.welcomeBody(), "\n")
	for _, want := range []string{"n      new session           space  quick prompt mode", "↵      focus it              A      attach it full screen", "x / v  kill / revive         s      settings", "space on a group row", "Press ? for every key: the map scrolls, and / searches it.", "Settings (s)"} {
		if !strings.Contains(body, want) {
			t.Errorf("default welcome is missing %q:\n%s", want, body)
		}
	}
	m.services.listKeys = m.services.listKeys.
		With(keybind.NewSession, bindingOf(t, "N")).
		With(keybind.Prompt, bindingOf(t)).
		With(keybind.Search, bindingOf(t)).
		With(keybind.Settings, bindingOf(t, "S"))
	body = strings.Join(m.welcomeBody(), "\n")
	for _, want := range []string{"N      new session", "Press ? for every key: the map scrolls.", "Settings (S)", "x / v  kill / revive         S      settings"} {
		if !strings.Contains(body, want) {
			t.Errorf("remapped welcome is missing %q:\n%s", want, body)
		}
	}
	for _, gone := range []string{"prompt it", "on a group row", "searches it", "n      new session"} {
		if strings.Contains(body, gone) {
			t.Errorf("remapped welcome should drop %q:\n%s", gone, body)
		}
	}
}

// A notice opening mid-drag would swallow the release and strand the
// lifted row, and one over the row menu would bury it, so both wait.
func TestNoticesWaitForADragOrTheRowMenu(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "alpha", t.TempDir(), "")
	createSession(t, m, "beta", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	preparedView(m)
	if !m.listReadyForNotice() {
		t.Fatal("test setup: a plain list takes a notice")
	}
	selection, _ := m.rail.Selected()
	handle := m.layout.displayedRail.Handles[selectionKeyForTest(selection)]
	line := paintedRailLines(t, m, "alpha")[0]
	y0, _ := m.bodyYRange()
	m.handleMouse(tea.MouseMsg{X: handle, Y: y0 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if m.listReadyForNotice() {
		t.Fatal("a lifted row should hold notices back")
	}
	m.handleMouse(tea.MouseMsg{X: handle, Y: y0 + line, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m.handleMouse(tea.MouseMsg{X: 2, Y: y0 + line, Button: tea.MouseButtonRight, Action: tea.MouseActionPress})
	if m.listReadyForNotice() {
		t.Fatal("an open row menu should hold notices back")
	}
}

func TestFullFootDiskAvailable(t *testing.T) {
	m := &Model{workspace: workspace{snap: sysstat.Snapshot{
		DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000,
	}}}
	line := ansi.Strip(strings.Join(m.fullFootLine(200), "\n"))
	if !strings.Contains(line, "disk 66% 167.9GB available") {
		t.Fatalf("disk reading = %q", line)
	}
}
