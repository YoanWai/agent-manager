package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
	"github.com/YoanWai/agent-manager/internal/update"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func noticeStore(t *testing.T) *store.Store {
	t.Helper()
	st := openTestStore(t, filepath.Join(t.TempDir(), "state.db"))
	t.Cleanup(func() { st.Close() })
	return st
}

func noticeModel(st *store.Store, version string) *Model {
	release := uirelease.New(st, version)
	release.Feed = []feed.Message{{ID: noticeTip, Title: "A tip", Body: []string{"details"}, URL: "https://example.com/tip"}}
	return &Model{
		release: release,
		poller:  &poller{runner: execution.New(execution.Dependencies{Store: st}, execution.Options{})},
		services: services{
			store:    st,
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		notices: noticesPanel{
			Panel: uinotices.NewPanel(uirelease.Dismissed(st), uirelease.Seen(st)),
		},
	}
}

// noticeTip is the feed message every noticeModel lists, a notice to open
// and dismiss now that none ship in the binary.
const noticeTip = "feed-tip"

// noticeModalFloor is the panel's narrowest content column.
const noticeModalFloor = 62

func noticeIDs(notices []uinotices.Notice) []string {
	ids := make([]string, len(notices))
	for i, n := range notices {
		ids[i] = n.ID
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
	if ids := noticeIDs(m.activeNotices()); !slices.Equal(ids, []string{noticeTip}) {
		t.Fatalf("a fresh store should list the feed alone, got %v", ids)
	}
}

func TestDismissPersistsAcrossRestart(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	persistNoticeDismissal(t, m, noticeTip)
	if contains(noticeIDs(m.activeNotices()), noticeTip) {
		t.Fatal("dismissed notice still active")
	}

	reopened := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(reopened.activeNotices()), noticeTip) {
		t.Fatal("dismissal did not survive restart")
	}
}

func TestConfigNotImportedNoticeCarriesTheReason(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(m.activeNotices()), uinotices.ConfigNotImported) {
		t.Fatal("an import that went through has nothing to report")
	}
	m.notices.ImportError = "config.toml: keybindings.session.detach needs at least one key"
	var refused uinotices.Notice
	for _, n := range m.activeNotices() {
		if n.ID == uinotices.ConfigNotImported {
			refused = n
		}
	}
	if refused.ID == "" {
		t.Fatalf("want %s among %v", uinotices.ConfigNotImported, noticeIDs(m.activeNotices()))
	}
	if !contains(refused.Body, m.notices.ImportError) {
		t.Fatalf("the notice should carry the reason: %q", refused.Body)
	}
	persistNoticeDismissal(t, m, uinotices.ConfigNotImported)
	reopened := noticeModel(st, "v0.2.0")
	reopened.notices.ImportError = m.notices.ImportError
	if contains(noticeIDs(reopened.activeNotices()), uinotices.ConfigNotImported) {
		t.Fatal("dismissal did not survive restart")
	}
}

func TestUpdateNoticePerRelease(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.release.Latest = "v0.3.0"
	m.release.URL = "https://example.com/v0.3.0"

	ids := noticeIDs(m.activeNotices())
	if !contains(ids, "update-v0.3.0") {
		t.Fatalf("want update notice, got %v", ids)
	}

	persistNoticeDismissal(t, m, "update-v0.3.0")
	if contains(noticeIDs(m.activeNotices()), "update-v0.3.0") {
		t.Fatal("dismissed update notice still active")
	}

	m.release.Latest = "v0.4.0"
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
	m.Update(uirelease.UpdateMsg{
		Latest:   "v0.6.0",
		URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.6.0",
		Releases: releases,
	})
	m.openNotices("update-v0.6.0")

	n := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()]
	if n.Title != "4 releases available · 0.6.0" {
		t.Fatalf("title = %q", n.Title)
	}
	if len(n.Releases) != 4 {
		t.Fatalf("summary has %d releases, want 4", len(n.Releases))
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{
		"4 releases available · 0.6.0",
		"v0.3.0",
		"v0.6.0",
		"4 releases · you're on 0.2.0",
		"Update to 0.6.0 and restart",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("cumulative modal missing %q:\n%s", want, frame)
		}
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	changes := ansi.Strip(preparedView(m))
	for _, want := range []string{"v0.3.0", "Worktree: Respect group defaults", "Sessions: Keep focus after refresh"} {
		if !strings.Contains(changes, want) {
			t.Fatalf("the Changes tab misses %q:\n%s", want, changes)
		}
	}
}

func TestPostUpdateNoticeUsesPersistedStartingVersion(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.LastSeenSetting, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.5.0")
	m.layout.width, m.layout.height = 100, 34
	m.release.Checked = true
	m.release.Releases = []update.Release{
		uiRelease("v0.5.0", "Groups: Create immediately"),
		uiRelease("v0.4.0", "UI: Scroll long prompts"),
		uiRelease("v0.3.0", "Worktree: Respect defaults"),
		uiRelease("v0.2.0", "Notices: Summarize releases"),
		uiRelease("v0.1.0", "Starting release"),
	}
	if got := m.startupGreeting(); got != "whatsnew-v0.5.0" {
		t.Fatalf("startup notice = %q", got)
	}
	m.openNotices("whatsnew-v0.5.0")

	if from, _ := st.Setting(uirelease.WhatsNewFromSetting); from != "v0.1.0" {
		t.Fatalf("persisted starting version = %q", from)
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{
		"Updated across 4 releases · v0.5.0",
		"4 releases · you were on 0.1.0",
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
	if err := st.SetSetting(uirelease.LastSeenSetting, "v0.5.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.4.0")
	if got := m.startupGreeting(); got != "" {
		t.Fatalf("downgrade should not open what's new, got %q", got)
	}
	if m.release.WhatsNewVersion == "v0.4.0" {
		t.Fatal("downgrade was recorded as an upgrade")
	}
	if version, _ := st.Setting(uirelease.WhatsNewSetting); version != "" {
		t.Fatalf("what's-new version persisted on downgrade: %q", version)
	}
	if version, _ := st.Setting(uirelease.WhatsNewFromSetting); version != "" {
		t.Fatalf("what's-new source persisted on downgrade: %q", version)
	}
	if version, _ := st.Setting(uirelease.LastSeenSetting); version != "v0.4.0" {
		t.Fatalf("last seen version = %q, want v0.4.0", version)
	}
}

func TestStartupNoticeFirstLaunch(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	if got := m.startupGreeting(); got != setupGreeting {
		t.Fatalf("first launch should open the setup, got %q", got)
	}
	if seen, _ := st.Setting(uirelease.LastSeenSetting); seen != "v0.2.0" {
		t.Fatalf("last seen version not recorded, got %q", seen)
	}

	again := noticeModel(st, "v0.2.0")
	if got := again.startupGreeting(); got != "" {
		t.Fatalf("same version must not reopen a notice, got %q", got)
	}
}

func TestStartupNoticeVersionChange(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.LastSeenSetting, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.2.0")
	got := m.startupGreeting()
	if got != "whatsnew-v0.2.0" {
		t.Fatalf("version change should open what's new, got %q", got)
	}
	if !contains(noticeIDs(m.activeNotices()), "whatsnew-v0.2.0") {
		t.Fatal("what's new notice must be listed until dismissed")
	}
	if seen, _ := st.Setting(uirelease.LastSeenSetting); seen != "v0.2.0" {
		t.Fatalf("last seen version not advanced, got %q", seen)
	}
}

func TestWhatsNewSurvivesRestartUntilDismissed(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.LastSeenSetting, "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.2.0")
	if m.startupGreeting() == "" {
		t.Fatal("want an auto-opened notice")
	}

	reopened := noticeModel(st, "v0.2.0")
	if reopened.startupGreeting() != "" {
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
	if got := m.startupGreeting(); got != setupGreeting {
		t.Fatalf("want the setup, got %q", got)
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
	m.release.Feed = []feed.Message{{
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
	reopened.release.Feed = m.release.Feed
	if contains(noticeIDs(reopened.activeNotices()), "feed-holdoff") {
		t.Fatal("feed dismissal must survive restart")
	}
}

func TestFeedUsesCanonicalTitleInModal(t *testing.T) {
	m := modalModel(t)
	m.release.Feed = []feed.Message{{
		ID:     "feed-canonical",
		Banner: "legacy compact copy",
		Title:  "One title everywhere",
		Body:   []string{"details"},
	}}
	m.openNotices("feed-canonical")
	modal := ansi.Strip(preparedView(m))
	if !strings.Contains(modal, "◆ One title everywhere") || strings.Count(modal, "legacy compact copy") != 1 || !strings.Contains(modal, "NEWS   legacy compact copy") {
		t.Fatalf("modal should list the title and keep the banner to the message's dim line:\n%s", modal)
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
	m.openNotices("")
	return m
}

func TestOpenNoticesFromList(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.base = baseList
	m.clearSurfaces()
	m.handleKey(key("M"))
	if !m.noticesOpen() {
		t.Fatalf("M should open the notices modal, screen = %v", m.screenID())
	}
}

func TestNoticesViewListsAndDetails(t *testing.T) {
	m := modalModel(t)
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"messages", "A tip", "details", "dismiss"} {
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
	m.release.Feed = []feed.Message{{
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
		if got := lipgloss.Width(strings.TrimSpace(line)); got <= noticeModalFloor+4 {
			t.Fatalf("wide terminal must widen the modal past %d, got %d", noticeModalFloor+4, got)
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
	m.release.Feed = []feed.Message{{
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
	for _, hint := range []string{"r refresh", "↵ open", "x dismiss", "esc/q/M"} {
		if !strings.Contains(joined, hint) {
			t.Fatalf("short terminal ate %q:\n%s", hint, joined)
		}
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
	m.release.Feed = []feed.Message{{ID: "feed-scroll", Banner: "scroll", Title: "Scrollable summary", Body: body}}
	m.openNotices("feed-scroll")

	before := ansi.Strip(preparedView(m))
	if !strings.Contains(before, "┃") {
		t.Fatalf("a clipped body shows its scrollbar:\n%s", before)
	}
	m.handleKey(key("pgdown"))
	if noticesSurfaceOf(m).Panel().Offset() == 0 {
		t.Fatalf("page down did not move the summary:\n%s", ansi.Strip(preparedView(m)))
	}
	limit := noticesSurfaceOf(m).Panel().ScrollLimit(m.activeNotices(), m.noticesFrame())
	for i := 0; i < 20; i++ {
		m.handleKey(key("pgdown"))
	}
	if noticesSurfaceOf(m).Panel().Offset() != limit {
		t.Fatalf("scroll offset = %d, want bounded limit %d", noticesSurfaceOf(m).Panel().Offset(), limit)
	}
}

func TestNoticesManualRefreshWaitsForBothSources(t *testing.T) {
	m := modalModel(t)
	_, cmd := m.handleKey(key("r"))
	if cmd == nil || !m.release.Refreshing || m.release.RefreshPending != 2 {
		t.Fatalf("refresh did not start both sources: refreshing=%v pending=%d", m.release.Refreshing, m.release.RefreshPending)
	}
	m.Update(uirelease.UpdateMsg{Manual: true, Releases: []update.Release{uiRelease("v0.2.0", "Current")}})
	if !m.release.Refreshing || m.release.RefreshPending != 1 {
		t.Fatalf("first result ended refresh early: refreshing=%v pending=%d", m.release.Refreshing, m.release.RefreshPending)
	}
	m.Update(uirelease.FeedMsg{Manual: true})
	if m.release.Refreshing || m.release.RefreshPending != 0 {
		t.Fatalf("refresh did not finish after both results: refreshing=%v pending=%d", m.release.Refreshing, m.release.RefreshPending)
	}
}

func TestNoticesDismissAdvancesAndCloses(t *testing.T) {
	m := modalModel(t)
	total := len(m.activeNotices())
	for i := 0; i < total; i++ {
		m.handleKey(key("x"))
	}
	if !m.onList() {
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
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	want := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].URL
	if want == "" {
		t.Fatal("selected notice needs a url for this test")
	}
	_, cmd := m.handleKey(key("enter"))
	if opened != "" {
		t.Fatal("browser started during Update")
	}
	m.applyCmd(t, cmd)
	if opened != want {
		t.Fatalf("opened %q, want %q", opened, want)
	}
}

func TestNoticesEscCloses(t *testing.T) {
	m := modalModel(t)
	m.handleKey(key("esc"))
	if !m.onList() {
		t.Fatal("esc should close the modal")
	}
	if len(m.activeNotices()) == 0 {
		t.Fatal("esc must not dismiss anything")
	}
}

func TestDevBuildNeverGreets(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "dev")
	m.openStartupGreeting()
	if !m.listClear() {
		t.Fatal("a dev build must not open a startup greeting")
	}
	if seen, _ := st.Setting(uirelease.LastSeenSetting); seen != "" {
		t.Fatalf("a dev build must not advance the stored version, got %q", seen)
	}
}

func TestStartupOpensTheSetupOnce(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.openStartupGreeting()
	if !m.setupOpen() {
		t.Fatalf("first launch should open the setup, mode=%v", m.screenID())
	}

	again := noticeModel(st, "v0.2.0")
	again.openStartupGreeting()
	if !again.listClear() {
		t.Fatalf("a later launch of the same version must not greet again, mode=%v", again.screenID())
	}
}

func TestLateReleaseKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	m.handleKey(key("down"))
	selected := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID

	m.Update(uirelease.UpdateMsg{Latest: "v9.9.9", URL: "https://example.com"})
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != selected {
		t.Fatalf("selection moved from %q to %q when the release arrived", selected, got)
	}
}

func TestLateDismissedReleaseKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	persistNoticeDismissal(t, m, "update-v9.9.9")
	selected := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID

	m.Update(uirelease.UpdateMsg{Latest: "v9.9.9", URL: "https://example.com"})
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != selected {
		t.Fatalf("a dismissed release must not move the selection, went from %q to %q", selected, got)
	}
}

func TestLateFeedKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	m.release.Latest = "v9.9.9"
	m.openNotices("update-v9.9.9")
	selected := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-late", Banner: "late", Title: "Late"}}})
	if !contains(noticeIDs(m.activeNotices()), "feed-late") {
		t.Fatal("feed message should have landed")
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != selected {
		t.Fatalf("selection moved from %q to %q when the feed arrived", selected, got)
	}
}

func TestNewFeedOpensNoticesModal(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		m := footModel(t)
		m.base = baseList
		m.clearSurfaces()
		m.prefs.hideStats = hidden
		m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
		if !m.noticesOpen() {
			t.Fatalf("hidden=%v: a new feed message should open the modal, screen = %v", hidden, m.screenID())
		}
		if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
			t.Fatalf("new message should be selected, got %q", got)
		}
	}
}

func TestSameFeedDoesNotReopenNoticesModal(t *testing.T) {
	m := footModel(t)
	m.base = baseList
	m.clearSurfaces()
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	m.handleKey(key("esc"))
	if !m.onList() {
		t.Fatal("esc should close the modal")
	}

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.onList() {
		t.Fatal("the same message must not reopen after esc")
	}
}

func TestDismissedFeedDoesNotOpenNoticesModal(t *testing.T) {
	m := footModel(t)
	m.base = baseList
	m.clearSurfaces()
	persistNoticeDismissal(t, m, "feed-new")

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.onList() {
		t.Fatal("a dismissed message must not open the modal")
	}
}

func TestNewFeedDoesNotStealFocus(t *testing.T) {
	m := footModel(t)
	m.push(newFocusSurface(m))

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.focused() {
		t.Fatalf("a new message must not steal an open session, screen = %v", m.screenID())
	}
}

func TestPendingFeedDoesNotOpenDuringQuit(t *testing.T) {
	m := footModel(t)
	m.push(newFocusSurface(m))
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if m.notices.Held != "feed-new" {
		t.Fatalf("pending notice = %q, want feed-new", m.notices.Held)
	}

	m.base = baseList
	m.clearSurfaces()
	m.effects.Quitting = true
	m.flushPendingNotice()
	if !m.onList() {
		t.Fatalf("pending feed opened while quitting: screen = %v", m.screenID())
	}
}

func TestAdditionalFeedIdOpensNoticesModal(t *testing.T) {
	m := footModel(t)
	m.base = baseList
	m.clearSurfaces()
	m.release.Feed = []feed.Message{{ID: "feed-old", Banner: "old", Title: "Old"}}

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{
		{ID: "feed-old", Banner: "old", Title: "Old"},
		{ID: "feed-new", Banner: "new", Title: "Just in"},
	}})
	if !m.noticesOpen() {
		t.Fatalf("a new id should open the modal, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestFeedDuringFocusOpensOnLeave(t *testing.T) {
	m := footModel(t)
	m.push(newFocusSurface(m))
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.focused() {
		t.Fatalf("screen = %v, want focus until leave", m.screenID())
	}

	m.leaveFocus()
	if !m.noticesOpen() {
		t.Fatalf("returning to the list should open the new message, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestFeedDuringSettingsOpensOnClose(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.push(newSettingsSurface(m, uisettings.State{}))
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.settingsOpen() || m.noticesOpen() {
		t.Fatalf("notices open=%v, want settings until close", m.noticesOpen())
	}

	m.surfaces = nil
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if !m.noticesOpen() {
		t.Fatalf("closing settings should open the new message, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestExpiredPendingNoticeDoesNotOpen(t *testing.T) {
	m := footModel(t)
	m.push(newFocusSurface(m))
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	m.Update(uirelease.FeedMsg{})
	m.leaveFocus()
	if !m.onList() {
		t.Fatalf("a notice that left the feed must not open, screen = %v", m.screenID())
	}
}

func TestNewFeedWaitsForSearchToClose(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.base = baseList
	m.clearSurfaces()
	m.rail.SetSearch(m.rail.Search(), true)
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.onList() || !m.rail.Searching() {
		t.Fatalf("search should keep the modal closed, screen = %v searching=%v", m.screenID(), m.rail.Searching())
	}

	m.rail.SetSearch(m.rail.Search(), false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if !m.noticesOpen() {
		t.Fatalf("closing search should open the new message, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestNewFeedWaitsForQuickBar(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.base = baseList
	m.clearSurfaces()
	pushQuick(m)
	m.quick().Input = textarea.New()
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.onList() || (m.quick() == nil) {
		t.Fatalf("quick bar should keep the modal closed, screen = %v quick=%v", m.screenID(), (m.quick() != nil))
	}

	m.closeQuick()
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if !m.noticesOpen() {
		t.Fatalf("closing the quick bar should open the new message, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestNewFeedWaitsForResize(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.base = baseList
	m.clearSurfaces()
	m.enterResizeMode()
	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.onList() || !m.layout.split.ResizeMode {
		t.Fatalf("resize should keep the modal closed, screen = %v resize=%v", m.screenID(), m.layout.split.ResizeMode)
	}

	m.exitResizeMode(false)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 34})
	if !m.noticesOpen() {
		t.Fatalf("leaving resize should open the new message, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "feed-new" {
		t.Fatalf("new message should be selected, got %q", got)
	}
}

func TestNewUpdateOpensNoticesModal(t *testing.T) {
	m := footModel(t)
	m.base = baseList
	m.clearSurfaces()

	m.Update(uirelease.UpdateMsg{Latest: "v0.3.0", URL: "https://example.com"})
	if !m.noticesOpen() {
		t.Fatalf("a new update notice should open the modal, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "update-v0.3.0" {
		t.Fatalf("update notice should be selected, got %q", got)
	}
}

func TestKnownLatestDoesNotReopenOnFetch(t *testing.T) {
	m := footModel(t)
	m.base = baseList
	m.clearSurfaces()
	m.release.Latest = "v0.3.0"
	m.release.URL = "https://example.com"
	m.release.IndexRanges()

	m.Update(uirelease.UpdateMsg{Latest: "v0.3.0", URL: "https://example.com"})
	if !m.onList() {
		t.Fatal("a latest already on the list must not pop the modal again")
	}
}

func TestNewerReleaseOpensNoticesModal(t *testing.T) {
	m := footModel(t)
	m.base = baseList
	m.clearSurfaces()
	m.release.Latest = "v0.3.0"
	m.release.URL = "https://example.com"
	m.release.IndexRanges()

	m.Update(uirelease.UpdateMsg{Latest: "v0.4.0", URL: "https://example.com"})
	if !m.noticesOpen() {
		t.Fatalf("a newer tag should open the modal, screen = %v", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != "update-v0.4.0" {
		t.Fatalf("new release should be selected, got %q", got)
	}
}

func TestFeedArrivingDuringSetupWaitsForTheList(t *testing.T) {
	m := footModel(t)
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	m.services.cfg = cfg
	m.layout.width, m.layout.height = 100, 34
	m.openStartupGreeting()
	if !m.setupOpen() {
		t.Fatal("first launch should open the setup")
	}

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.setupOpen() {
		t.Fatalf("a feed message took the screen from the setup, mode=%v", m.screenID())
	}
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyRunes, Runes: []rune("y")}} {
		_, cmd := m.handleKey(key)
		if cmd != nil {
			m.routeSetupMsg(cmd())
		}
	}
	m.flushPendingNotice()
	if !m.noticesOpen() || m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID != "feed-new" {
		t.Fatalf("the waiting message should open once the setup closes, mode=%v", m.screenID())
	}
}

func TestAutoOpenFeedAndUpdateFrames(t *testing.T) {
	feedModel := footModel(t)
	feedModel.layout.width, feedModel.layout.height = 100, 34
	feedModel.base = baseList
	feedModel.Update(uirelease.FeedMsg{Messages: []feed.Message{{
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
	upd.base = baseList
	upd.Update(uirelease.UpdateMsg{
		Latest:   "v0.3.0",
		URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
		Releases: []update.Release{uiRelease("v0.3.0", "Notices: Open the modal on a new message")},
	})
	updFrame := ansi.Strip(preparedView(upd))
	for _, want := range []string{"messages", "0.3.0 available", "you're on 0.2.0", "u update", "x dismiss"} {
		if !strings.Contains(updFrame, want) {
			t.Fatalf("update modal missing %q:\n%s", want, updFrame)
		}
	}
}

func TestUpdateNoticeAppliesOnU(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.release.Latest = "v0.3.0"
	m.openNotices("update-v0.3.0")
	if !m.noticesOpen() || noticesSurfaceOf(m).Panel().Cursor() != 0 {
		t.Fatalf("update notice should open selected, screen = %v cursor=%d", m.screenID(), noticesSurfaceOf(m).Panel().Cursor())
	}

	applied := ""
	orig := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = orig }()
	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest: "v0.6.0",
			URL:    "https://github.com/YoanWai/agent-manager/releases/tag/v0.6.0",
			Releases: []update.Release{
				uiRelease("v0.6.0", "Newest release"),
				uiRelease("v0.2.0", "Current release"),
			},
		}, nil
	}
	uirelease.ApplyUpdate = func(_ context.Context, tag, execPath string) error {
		applied = tag + " " + execPath
		return nil
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("u on the update notice should start the update")
	}
	if !m.release.Applying {
		t.Fatal("applying should be marked while the download runs")
	}
	msg, ok := cmd().(uirelease.AppliedMsg)
	if !ok {
		t.Fatalf("cmd returned %T", cmd())
	}
	if msg.Err != nil {
		t.Fatalf("apply: %v", msg.Err)
	}
	if !strings.HasPrefix(applied, "v0.6.0 ") {
		t.Fatalf("uirelease.ApplyUpdate saw %q", applied)
	}
	updated, quit := m.Update(msg)
	m = updated.(*Model)
	if m.release.Applying {
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
	m.release.Latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	orig := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = orig }()
	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{
			Latest:   "v0.3.0",
			URL:      "https://github.com/YoanWai/agent-manager/releases/tag/v0.3.0",
			Releases: []update.Release{uiRelease("v0.3.0", "Newest release"), uiRelease("v0.2.0", "Current release")},
		}, nil
	}
	uirelease.ApplyUpdate = func(_ context.Context, _, _ string) error {
		return errors.New("permission denied")
	}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	updated, _ := m.Update(cmd().(uirelease.AppliedMsg))
	m = updated.(*Model)
	if m.release.Applying {
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
	m.release.Latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{}, errors.New("github unavailable")
	}
	origApply := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = origApply }()
	called := false
	uirelease.ApplyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	_, cmd := m.handleKey(key("u"))
	updated, _ := m.Update(cmd().(uirelease.AppliedMsg))
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
	m.release.Latest = "v0.4.0"
	// Read already, so the reselect after the refresh sends no SeenRequest to make quit non-nil.
	m.notices.See(noticeTip)
	m.openNotices("update-v0.4.0")
	m.drainEffects(t)

	origRefresh := uirelease.RefreshForApply
	defer func() { uirelease.RefreshForApply = origRefresh }()
	uirelease.RefreshForApply = func(context.Context, string, string) (update.Result, error) {
		return update.Result{}, nil
	}
	origApply := uirelease.ApplyUpdate
	defer func() { uirelease.ApplyUpdate = origApply }()
	called := false
	uirelease.ApplyUpdate = func(context.Context, string, string) error {
		called = true
		return nil
	}

	_, cmd := m.handleKey(key("u"))
	updated, quit := m.Update(cmd().(uirelease.AppliedMsg))
	m = updated.(*Model)
	if called || quit != nil || m.release.Latest != "" || m.RestartPath() != "" {
		t.Fatal("an already-current refresh should clear the stale notice without installing")
	}
	if m.errBar.text != "already up to date" {
		t.Fatalf("status = %q", m.errBar.text)
	}
}

func TestUOutsideUpdateNoticeDoesNothing(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.openNotices(noticeTip)
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd != nil || m.release.Applying {
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
	m.release.Latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	origDetect := uirelease.DetectManager
	defer func() { uirelease.DetectManager = origDetect }()
	uirelease.DetectManager = func(string) update.Manager {
		return update.Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("u on a package-manager install should start the delegated upgrade")
	}
	if !m.release.Applying {
		t.Fatal("applying should be marked while the manager runs")
	}
}

func TestUpdateWithoutRunnableManagerSurfacesAdvice(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.release.Latest = "v0.3.0"
	m.openNotices("update-v0.3.0")

	origDetect := uirelease.DetectManager
	defer func() { uirelease.DetectManager = origDetect }()
	advice := "installed with pacman; install an AUR helper first, then: yay -S agent-manager-bin"
	uirelease.DetectManager = func(string) update.Manager {
		return update.Manager{Name: "pacman", Advice: advice}
	}

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("u should still answer with the advice")
	}
	msg, ok := cmd().(uirelease.AppliedMsg)
	if !ok || msg.Err == nil || msg.Err.Error() != advice {
		t.Fatalf("want the advice as the error, got %#v", cmd())
	}
}

func TestEmptyNoticesPanelStillOpensAndOffersRefresh(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.base = baseList
	m.clearSurfaces()
	for _, n := range m.activeNotices() {
		persistNoticeDismissal(t, m, n.ID)
	}
	if len(m.activeNotices()) != 0 {
		t.Fatal("test needs an empty panel")
	}

	m.handleKey(key("M"))
	if !m.noticesOpen() {
		t.Fatalf("M should open an empty panel, screen = %v", m.screenID())
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"nothing new", "r refresh"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("empty panel missing %q:\n%s", want, frame)
		}
	}

	if _, cmd := m.handleKey(key("r")); cmd == nil || !m.release.Refreshing {
		t.Fatalf("r unreachable on an empty panel: refreshing=%v", m.release.Refreshing)
	}
	if !strings.Contains(ansi.Strip(preparedView(m)), "refreshing releases and messages") {
		t.Fatalf("empty panel hides the refresh it started:\n%s", ansi.Strip(preparedView(m)))
	}
}

// A notice opening mid-drag would swallow the release and strand the
// lifted row, and one over the row menu would bury it, so both wait.
func TestNoticesWaitForADragOrTheRowMenu(t *testing.T) {
	t.Parallel()
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

func TestDismissNoticeDeduplicatesPendingEffect(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.dismissNotice(noticeTip)
	m.dismissNotice(noticeTip)

	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("duplicate dismissal queued %d effects, want 1", len(m.effects.Main.Pending))
	}
	request, ok := m.effects.Main.Pending[0].Request.(uinotices.DismissRequest)
	if !ok || request.ID != noticeTip {
		t.Fatalf("queued request = %#v", m.effects.Main.Pending[0].Request)
	}
	m.drainEffects(t)
}

func TestDismissNoticeFailureRestoresNotice(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.DismissedSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	m.openNotices(noticeTip)

	_, _ = m.handleKey(key("x"))
	if contains(noticeIDs(m.activeNotices()), noticeTip) {
		t.Fatal("accepted dismissal stayed visible while its write was pending")
	}
	cmd := m.nextEffectCmd()
	if cmd == nil {
		t.Fatal("dismissal did not queue persistence")
	}
	m.applyCmd(t, cmd)

	if !contains(noticeIDs(m.activeNotices()), noticeTip) {
		t.Fatal("failed persistence did not restore the notice")
	}
	if !m.noticesOpen() || m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID != noticeTip {
		t.Fatalf("failed dismissal did not restore its modal selection: screen = %v cursor=%d", m.screenID(), noticesSurfaceOf(m).Panel().Cursor())
	}
	if !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("failure reason = %q", m.errBar.text)
	}
}

func TestDismissNoticeFailureDoesNotReopenAfterNewerInput(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.DismissedSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	m.openNotices(noticeTip)
	_, _ = m.handleKey(key("x"))
	cmd := m.nextEffectCmd()

	updated, _ := m.handleKey(key("esc"))
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if !m.onList() {
		t.Fatalf("failed old dismissal stole focus from newer input: screen = %v", m.screenID())
	}
	if !contains(noticeIDs(m.activeNotices()), noticeTip) {
		t.Fatal("failed old dismissal did not restore the notice card")
	}
}

func TestDismissNoticeFailureDuringQuitRestoresWithoutReopening(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.DismissedSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	// Leave one visible card so accepting its dismissal closes the modal.
	m.openNotices(noticeTip)

	_, _ = m.handleKey(key("x"))
	cmd := m.nextEffectCmd()
	if cmd == nil || !m.onList() {
		t.Fatalf("last dismissal was not accepted: cmd=%v screen = %v", cmd != nil, m.screenID())
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if !m.effects.Quitting {
		t.Fatal("ctrl+c did not start draining the accepted dismissal")
	}
	m.applyCmd(t, cmd)

	if !m.onList() {
		t.Fatalf("failed dismissal reopened over quit: screen = %v", m.screenID())
	}
	if !contains(noticeIDs(m.activeNotices()), noticeTip) {
		t.Fatal("failed dismissal did not restore the notice data")
	}
	if !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("failure reason = %q", m.errBar.text)
	}
}

func TestNoticeMarksAreResolvedWithoutChangingExternalContent(t *testing.T) {
	m := noticeModel(noticeStore(t), "v1.0.0")
	const text = "⚙❯✳ literal notice"
	m.release.Feed = []feed.Message{{ID: "literal", Title: text, Body: []string{text}, URL: "https://example.com"}}
	m.release.Latest = "v1.1.0"
	m.release.Refreshing, m.release.Applying = true, true
	for _, tc := range []struct {
		mode                                          presentation.GlyphMode
		feed, update, refresh, download, link, cursor string
	}{
		{presentation.GlyphModeBasic, "i", "^", "R", "v", "|   enter", "|"},
		{presentation.GlyphModeAdvanced, "◆", "↑", "↻", "↓", "▍   ↵", "▍"},
	} {
		m.prefs.glyphMode = tc.mode
		notices := m.activeNotices()
		for _, n := range notices {
			if n.ID == "literal" && (n.Title != text || strings.Join(n.Body, "") != text) {
				t.Fatal("external message rewritten")
			}
		}
		m.layout.width, m.layout.height = 120, 40
		m.notices.Open(notices, "literal")
		got := ansi.Strip(strings.Join(m.notices.View(notices, m.noticesFrame()), "\n"))
		tail := got
		for _, want := range []string{tc.feed + " " + text, tc.update + " 1.1.0 available", tc.refresh + " refreshing", tc.download + " downloading", tc.link + "    Open link", "example.com"} {
			if !strings.Contains(tail, want) {
				t.Fatalf("%s tail missing %q: %s", tc.mode, want, tail)
			}
		}
		m.notices.Open(notices, "update-v1.1.0")
		got = ansi.Strip(strings.Join(m.notices.View(m.activeNotices(), m.noticesFrame()), "\n"))
		if !strings.Contains(got, tc.cursor+" "+tc.update) {
			t.Fatalf("%s notice cursor = %s", tc.mode, got)
		}
	}
}

func TestClickOnAMessageTitleSelectsIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.release.Feed = []feed.Message{{ID: "feed-first", Title: "Hold off on v0.2.0", Body: []string{"details"}}}
	m.release.Latest = "v9.9.9"
	m.openNotices("")
	notices := m.activeNotices()
	if len(notices) < 2 {
		t.Fatalf("test setup: a fresh model shows two notices, has %d", len(notices))
	}
	x, y := paintedCell(t, m, notices[1].Title)
	if m = leftClick(m, x, y); noticesSurfaceOf(m).Panel().Cursor() != 1 || !m.noticesOpen() {
		t.Fatalf("a click on the second title should select it, cursor %d mode %v", noticesSurfaceOf(m).Panel().Cursor(), m.screenID())
	}
	if body := preparedView(m); !strings.Contains(body, notices[1].Title) {
		t.Fatal("the selected notice should stay on screen")
	}
}

func TestUpdateMsgSetsAndClearsBadge(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	m.Update(uirelease.UpdateMsg{Latest: "v0.11.1", URL: "https://example/rel"})
	if m.release.Latest != "v0.11.1" || m.release.URL != "https://example/rel" {
		t.Errorf("badge not set: %q %q", m.release.Latest, m.release.URL)
	}
	m.Update(uirelease.UpdateMsg{})
	if m.release.Latest != "" || m.release.URL != "" {
		t.Errorf("an up-to-date result should clear the badge: %q %q", m.release.Latest, m.release.URL)
	}
}

func TestFailedUpdateCheckKeepsBadge(t *testing.T) {
	m := &Model{
		layout:  layoutState{width: 120},
		release: uirelease.State{Latest: "v0.11.1", URL: "https://example/rel"},
	}
	m.Update(uirelease.UpdateMsg{Failed: true})
	if m.release.Latest != "v0.11.1" || m.release.URL != "https://example/rel" {
		t.Errorf("a failed check must leave the badge alone: %q %q", m.release.Latest, m.release.URL)
	}
}

func TestUpdateTickReArms(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	if _, cmd := m.Update(uirelease.TickMsg{}); cmd == nil {
		t.Error("update tick should re-arm the timer and re-check")
	}
}

func scrollModel(t *testing.T, lines int) *Model {
	t.Helper()
	m := modalModel(t)
	m.layout.width, m.layout.height = 70, 14
	var body []string
	for index := 0; index < lines; index++ {
		body = append(body, fmt.Sprintf("change line %02d", index))
	}
	m.release.Feed = []feed.Message{{ID: "feed-scroll", Banner: "scroll", Title: "Scrollable summary", Body: body}}
	m.openNotices("feed-scroll")
	return m
}

func thumbRows(frame string) []int {
	var rows []int
	for index, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "┃") {
			rows = append(rows, index)
		}
	}
	return rows
}

func TestShortBodyHasNoScrollbar(t *testing.T) {
	m := scrollModel(t, 2)
	if frame := ansi.Strip(preparedView(m)); strings.Contains(frame, "┃") {
		t.Fatalf("a body that fits needs no scrollbar:\n%s", frame)
	}
}

func TestWheelScrollsTheMessageBody(t *testing.T) {
	m := scrollModel(t, 30)
	before := thumbRows(ansi.Strip(preparedView(m)))
	m = wheelOnCard(m, tea.MouseButtonWheelDown)
	if noticesSurfaceOf(m).Panel().Offset() != uinotices.WheelRows {
		t.Fatalf("wheel down moved %d rows, want %d", noticesSurfaceOf(m).Panel().Offset(), uinotices.WheelRows)
	}
	for i := 0; i < 40; i++ {
		m = wheelOnCard(m, tea.MouseButtonWheelDown)
	}
	limit := noticesSurfaceOf(m).Panel().ScrollLimit(m.activeNotices(), m.noticesFrame())
	if noticesSurfaceOf(m).Panel().Offset() != limit {
		t.Fatalf("wheel scrolled to %d, want it bounded at %d", noticesSurfaceOf(m).Panel().Offset(), limit)
	}
	after := thumbRows(ansi.Strip(preparedView(m)))
	if len(before) == 0 || len(after) == 0 || after[0] <= before[0] {
		t.Fatalf("the thumb should have moved down: before %v, after %v", before, after)
	}
	m = wheelOnCard(m, tea.MouseButtonWheelUp)
	if noticesSurfaceOf(m).Panel().Offset() != limit-uinotices.WheelRows {
		t.Fatalf("wheel up moved to %d, want %d", noticesSurfaceOf(m).Panel().Offset(), limit-uinotices.WheelRows)
	}
	if !m.noticesOpen() {
		t.Fatalf("the wheel must not leave the panel, screen = %v", m.screenID())
	}
}

func TestHomeAndEndJumpTheMessageBody(t *testing.T) {
	m := scrollModel(t, 30)
	limit := noticesSurfaceOf(m).Panel().ScrollLimit(m.activeNotices(), m.noticesFrame())
	if limit <= 0 {
		t.Fatalf("the body should scroll, limit %d", limit)
	}
	for _, jump := range []string{"end", "G"} {
		m.handleKey(key("home"))
		m.handleKey(key(jump))
		if noticesSurfaceOf(m).Panel().Offset() != limit {
			t.Fatalf("%s moved to %d, want the bottom at %d", jump, noticesSurfaceOf(m).Panel().Offset(), limit)
		}
	}
	for _, jump := range []string{"home", "g"} {
		m.handleKey(key("end"))
		m.handleKey(key(jump))
		if noticesSurfaceOf(m).Panel().Offset() != 0 {
			t.Fatalf("%s moved to %d, want the top", jump, noticesSurfaceOf(m).Panel().Offset())
		}
	}
}

func TestGrowingTheTerminalKeepsTheLastPageInView(t *testing.T) {
	m := scrollModel(t, 30)
	m.handleKey(key("end"))
	bottom := noticesSurfaceOf(m).Panel().Offset()
	m.layout.height = 20
	frame := ansi.Strip(preparedView(m))
	if !strings.Contains(frame, "change line 29") || !strings.Contains(frame, "╰") {
		t.Fatalf("an offset past the new last page shows that last page inside the frame:\n%s", frame)
	}
	limit := noticesSurfaceOf(m).Panel().ScrollLimit(m.activeNotices(), m.noticesFrame())
	if limit >= bottom {
		t.Fatalf("the taller terminal should have fewer pages: limit %d, saved offset %d", limit, bottom)
	}
	m = wheelOnCard(m, tea.MouseButtonWheelUp)
	if noticesSurfaceOf(m).Panel().Offset() != limit-uinotices.WheelRows {
		t.Fatalf("the first scroll up starts from the page on screen: offset %d, want %d", noticesSurfaceOf(m).Panel().Offset(), limit-uinotices.WheelRows)
	}
}

func TestHintNamesHomeAndEndWhenItFits(t *testing.T) {
	m := modalModel(t)
	m.release.Feed = []feed.Message{{ID: "feed-wide", Banner: "wide", Title: "Wide", Body: []string{strings.Repeat("wide words ", 12)}}}
	m.openNotices("feed-wide")
	m.layout.width = 180
	if frame := ansi.Strip(preparedView(m)); !strings.Contains(frame, "pgup/pgdn/home/end scroll") {
		t.Fatalf("a wide frame names home and end:\n%s", frame)
	}
	m.layout.width = 80
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"x dismiss", "esc"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("a narrow frame keeps %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "home/end") {
		t.Fatalf("a narrow frame keeps the short hint:\n%s", frame)
	}
}

func noticesOverList(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	// The real trigger: the messages key on the list.
	m.Update(runeKey("M"))
	if !m.noticesOpen() {
		t.Fatal("M did not open the messages modal")
	}
	return m
}

func TestNoticesSurfaceOpensOverTheList(t *testing.T) {
	m := noticesOverList(t)
	if !m.onList() || len(m.surfaces) != 1 {
		t.Fatalf("screen = %v, surfaces = %d", m.screenID(), len(m.surfaces))
	}
	m.openNotices("")
	if len(m.surfaces) != 1 {
		t.Fatalf("opening an open panel stacked another: %d surfaces", len(m.surfaces))
	}
}

func TestNoticesSurfaceClosesOnEscAndBackdrop(t *testing.T) {
	if !(&uinotices.Surface{}).Dismissible() {
		t.Fatal("the messages modal must be dismissible")
	}
	for name, closeIt := range map[string]func(*Model) *Model{
		"esc": func(m *Model) *Model { m.Update(namedKey(tea.KeyEsc)); return m },
		"click off the card": func(m *Model) *Model {
			return clickBackdrop(t, m)
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := closeIt(noticesOverList(t))
			if m.noticesOpen() || m.effects.Quitting {
				t.Fatalf("modal open = %t, quitting = %t", m.noticesOpen(), m.effects.Quitting)
			}
		})
	}
}

// A dismissal that fails to persist reopens the modal only while nothing
// else is on screen; a card the user opened since owns the screen.
func TestNoticesSurfaceFencesAStaleDismissalCompletion(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.layout.width, m.layout.height = 120, 34
	m.openNotices(noticeTip)
	job := &uieffects.Job{Request: uinotices.DismissRequest{ID: noticeTip, ForegroundGen: m.gens.foreground, Modal: true}}

	m.Pop()
	m.openHelp()
	m.applyNoticeDismiss(job, errTest("persist failed"))
	if m.noticesOpen() {
		t.Fatal("a failed dismissal opened the modal over another card")
	}

	m.Pop()
	m.applyNoticeDismiss(job, errTest("persist failed"))
	if !m.noticesOpen() {
		t.Fatal("a failed dismissal did not reopen the modal on a clear list")
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestFeedResultClearsThePendingAlert(t *testing.T) {
	cases := []struct {
		name string
		msg  uirelease.FeedMsg
		open bool
	}{
		{name: "new entry", msg: uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}}, open: true},
		{name: "nothing new", msg: uirelease.FeedMsg{}, open: false},
		{name: "refused", msg: uirelease.FeedMsg{Failed: true, Err: errors.New("feed down")}, open: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := footModel(t)
			m.release.FeedPending = true
			tc.msg.Startup = true
			m.Update(tc.msg)
			if m.release.FeedPending {
				t.Fatal("the feed result left the check pending")
			}
			if _, label := m.messagesAlert(); label == uinotices.FeedPendingText {
				t.Fatal("the checking alert stayed up")
			}
			if opened := m.noticesOpen(); opened != tc.open {
				t.Fatalf("panel open = %v, want %v", opened, tc.open)
			}
		})
	}
}

func TestFeedPendingWaitsForStartupResult(t *testing.T) {
	for _, manual := range []bool{true, false} {
		m := footModel(t)
		m.release.FeedPending = true
		m.Update(uirelease.FeedMsg{Manual: manual})
		if !m.release.FeedPending {
			t.Fatalf("a non-startup result (manual=%v) cleared the pending startup check", manual)
		}
		m.Update(uirelease.FeedMsg{Startup: true})
		if m.release.FeedPending {
			t.Fatal("the startup result left the check pending")
		}
	}
}

func TestCheckingAlertOpensMessagesByClick(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.release.FeedPending = true
	view := ansi.Strip(preparedView(m))
	x, y := -1, 0
	for row, line := range strings.Split(view, "\n") {
		if col := strings.Index(line, uinotices.FeedPendingText); col >= 0 {
			x, y = ansi.StringWidth(line[:col]), row
			break
		}
	}
	if x < 0 {
		t.Fatalf("footer should say the feed check has not landed:\n%s", view)
	}
	if m = leftClick(m, x, y); !m.noticesOpen() {
		t.Fatal("a click on the checking alert should open messages")
	}
	m.Update(key("esc"))
	if m.noticesOpen() {
		t.Fatal("esc should close messages")
	}
	m.Update(key("M"))
	if !m.noticesOpen() {
		t.Fatal("M should open messages while the check is pending")
	}
}

func TestOpenPanelSaysTheFeedCheckHasNotLanded(t *testing.T) {
	m := modalModel(t)
	m.release.FeedPending = true
	if got := ansi.Strip(preparedView(m)); !strings.Contains(got, uinotices.FeedPendingText) {
		t.Fatalf("an open panel should say the feed check has not landed:\n%s", got)
	}
}

func TestNoticesCloseFollowsSavedBinding(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.openNotices("")
	m.services.listKeys = m.services.listKeys.With(keybind.Messages, bindingOf(t, "N", "f2"))
	m.handleKey(key("M"))
	if !m.noticesOpen() {
		t.Fatal("old messages binding closed dialog")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyF2})
	if m.noticesOpen() {
		t.Fatal("saved messages binding did not close dialog")
	}
	m.openNotices("")
	m.services.listKeys = m.services.listKeys.With(keybind.Messages, bindingOf(t))
	m.handleKey(key("M"))
	if !m.noticesOpen() {
		t.Fatal("disabled messages binding retained old alias")
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.noticesOpen() {
		t.Fatal("Escape did not close messages")
	}
}

func TestNoticesToggleDoesNotOverrideRefresh(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.openNotices("")
	m.services.listKeys = m.services.listKeys.With(keybind.Messages, bindingOf(t, "r"))
	_, cmd := m.handleKey(key("r"))
	if !m.noticesOpen() || cmd == nil || !m.release.Refreshing {
		t.Fatal("toggle stole local refresh action")
	}
}

const noticeNews = "feed-news"

// seenModel lists two feed messages, the tip first, with the startup feed
// check landed so the footer chip reads the notices themselves.
func seenModel(st *store.Store) *Model {
	m := noticeModel(st, "v0.2.0")
	m.release.Feed = append(m.release.Feed, feed.Message{ID: noticeNews, Title: "Some news", Body: []string{"more"}})
	m.release.FeedPending = false
	m.layout.width, m.layout.height = 100, 34
	return m
}

func unreadIDs(notices []uinotices.Notice) []string {
	var ids []string
	for _, n := range notices {
		if n.Unread {
			ids = append(ids, n.ID)
		}
	}
	return ids
}

func TestOpeningThePanelMarksTheSelectedNoticeSeen(t *testing.T) {
	st := noticeStore(t)
	m := seenModel(st)
	if got := unreadIDs(m.activeNotices()); !slices.Equal(got, []string{noticeTip, noticeNews}) {
		t.Fatalf("a fresh store should leave both unread, got %v", got)
	}
	m.openNotices(noticeTip)
	m.drainEffects(t)
	if got := unreadIDs(m.activeNotices()); !slices.Equal(got, []string{noticeNews}) {
		t.Fatalf("unread after opening on the tip = %v, want the news alone", got)
	}
	if raw, err := st.Setting(uirelease.SeenSetting); err != nil || raw != `["`+noticeTip+`"]` {
		t.Fatalf("seen setting = %q, %v", raw, err)
	}
}

func TestMovingTheCursorMarksTheNextNoticeSeen(t *testing.T) {
	m := seenModel(noticeStore(t))
	m.openNotices(noticeTip)
	m.handleKey(key("down"))
	m.drainEffects(t)
	if got := unreadIDs(m.activeNotices()); got != nil {
		t.Fatalf("unread after moving to the news = %v, want none", got)
	}
}

func TestClickingAMessageMarksItSeen(t *testing.T) {
	m := seenModel(noticeStore(t))
	m.openNotices(noticeTip)
	x, y := paintedCell(t, m, "Some news")
	m = leftClick(m, x, y)
	m.drainEffects(t)
	if got := unreadIDs(m.activeNotices()); got != nil {
		t.Fatalf("unread after clicking the news = %v, want none", got)
	}
}

func TestFooterChipSaysMessagesOnceEverythingIsSeen(t *testing.T) {
	m := seenModel(noticeStore(t))
	m.openNotices(noticeTip)
	m.Pop()
	m.drainEffects(t)
	if _, label := m.messagesAlert(); label != "unread" {
		t.Fatalf("one unseen notice: chip = %q, want unread", label)
	}
	persistNoticeDismissal(t, m, noticeNews)
	if _, label := m.messagesAlert(); label != "messages" {
		t.Fatalf("every active notice seen: chip = %q, want messages", label)
	}
	persistNoticeDismissal(t, m, noticeTip)
	if glyph, label := m.messagesAlert(); glyph != "" || label != "" {
		t.Fatalf("nothing active: chip = %q %q, want none", glyph, label)
	}
}

func TestReadMessagesChipIsADimPair(t *testing.T) {
	m := seenModel(noticeStore(t))
	m.openNotices(noticeTip)
	m.handleKey(key("down"))
	m.Pop()
	m.drainEffects(t)
	legend := m.viewLegend()
	if legend.Alerts != nil {
		t.Fatalf("a read inbox raised the loud alert: %v", legend.Alerts)
	}
	if len(legend.Pairs) == 0 || legend.Pairs[0][1] != "messages" {
		t.Fatalf("the read chip should lead the quiet pairs, got %v", legend.Pairs)
	}
}

func TestSeenSurvivesARestart(t *testing.T) {
	st := noticeStore(t)
	m := seenModel(st)
	m.openNotices(noticeTip)
	m.drainEffects(t)

	reopened := seenModel(st)
	if got := unreadIDs(reopened.activeNotices()); !slices.Equal(got, []string{noticeNews}) {
		t.Fatalf("unread after a restart = %v, want the news alone", got)
	}
}

func TestSeenWriteFailureKeepsTheMark(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.SeenSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := seenModel(st)
	m.openNotices(noticeTip)
	m.drainEffects(t)
	if !strings.HasPrefix(m.errBar.text, "mark message read: ") || !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("status bar = %q", m.errBar.text)
	}
	if got := unreadIDs(m.activeNotices()); !slices.Equal(got, []string{noticeNews}) {
		t.Fatalf("a failed write dropped the in-memory mark: unread = %v", got)
	}
}

func TestMovingAnOpenPanelMarksItsNoticeSeen(t *testing.T) {
	m := seenModel(noticeStore(t))
	m.openNotices(noticeTip)
	m.openNotices(noticeNews)
	m.drainEffects(t)
	if got := unreadIDs(m.activeNotices()); got != nil {
		t.Fatalf("unread after the open panel moved to the news = %v, want none", got)
	}
}
