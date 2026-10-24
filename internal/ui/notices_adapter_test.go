package ui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
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
	return &Model{
		release: uirelease.New(st, version),
		poller:  &poller{runner: execution.New(execution.Dependencies{Store: st}, execution.Options{})},
		services: services{
			store:    st,
			keys:     keybind.DefaultSession(),
			listKeys: keybind.DefaultList(),
		},
		notices: noticesPanel{
			Panel: uinotices.NewPanel(uirelease.Dismissed(st)),
		},
	}
}

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
	ids := noticeIDs(m.activeNotices())
	if !contains(ids, uinotices.Welcome) {
		t.Fatalf("want %s among %v", uinotices.Welcome, ids)
	}
	var welcome uinotices.Notice
	for _, n := range m.activeNotices() {
		if n.ID == uinotices.Welcome {
			welcome = n
		}
	}
	joined := strings.Join(welcome.Body, " ")
	if !strings.Contains(joined, "Settings (s)") || !strings.Contains(joined, "A bug or an idea?") {
		t.Fatalf("welcome should point at Settings for a bug or an idea: %q", joined)
	}
	for _, n := range m.activeNotices() {
		if n.ID != uinotices.Welcome && n.URL == bugReportURL(m.release.Version) {
			t.Fatalf("standalone bug-report notice still active: %q", n.ID)
		}
	}
}

func TestDismissPersistsAcrossRestart(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	persistNoticeDismissal(t, m, uinotices.Welcome)
	if contains(noticeIDs(m.activeNotices()), uinotices.Welcome) {
		t.Fatal("dismissed notice still active")
	}

	reopened := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(reopened.activeNotices()), uinotices.Welcome) {
		t.Fatal("dismissal did not survive restart")
	}
}

func TestConfigNotImportedNoticeCarriesTheReason(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(m.activeNotices()), uinotices.ConfigNotImported) {
		t.Fatal("an import that went through has nothing to report")
	}
	m.notices.configImportError = "config.toml: keybindings.session.detach needs at least one key"
	var refused uinotices.Notice
	for _, n := range m.activeNotices() {
		if n.ID == uinotices.ConfigNotImported {
			refused = n
		}
	}
	if refused.ID == "" {
		t.Fatalf("want %s among %v", uinotices.ConfigNotImported, noticeIDs(m.activeNotices()))
	}
	if !contains(refused.Body, m.notices.configImportError) {
		t.Fatalf("the notice should carry the reason: %q", refused.Body)
	}
	persistNoticeDismissal(t, m, uinotices.ConfigNotImported)
	reopened := noticeModel(st, "v0.2.0")
	reopened.notices.configImportError = m.notices.configImportError
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
	if n.Title != "4 releases available · v0.6.0" {
		t.Fatalf("title = %q", n.Title)
	}
	if len(n.Releases) != 4 {
		t.Fatalf("summary has %d releases, want 4", len(n.Releases))
	}
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{
		"4 releases available · v0.6.0",
		"v0.3.0",
		"Worktree: Respect group defaults",
		"v0.6.0",
		"Press u to update to v0.6.0",
	} {
		if !strings.Contains(frame, want) {
			t.Fatalf("cumulative modal missing %q:\n%s", want, frame)
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
	if got := m.startupNotice(); got != "whatsnew-v0.5.0" {
		t.Fatalf("startup notice = %q", got)
	}
	m.openNotices("whatsnew-v0.5.0")

	if from, _ := st.Setting(uirelease.WhatsNewFromSetting); from != "v0.1.0" {
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
	if err := st.SetSetting(uirelease.LastSeenSetting, "v0.5.0"); err != nil {
		t.Fatal(err)
	}
	m := noticeModel(st, "v0.4.0")
	if got := m.startupNotice(); got != "" {
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
	if got := m.startupNotice(); got != uinotices.Welcome {
		t.Fatalf("first launch should open the welcome notice, got %q", got)
	}
	if seen, _ := st.Setting(uirelease.LastSeenSetting); seen != "v0.2.0" {
		t.Fatalf("last seen version not recorded, got %q", seen)
	}

	again := noticeModel(st, "v0.2.0")
	if got := again.startupNotice(); got != "" {
		t.Fatalf("same version must not reopen a notice, got %q", got)
	}
}

func TestStartupNoticeVersionChange(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(uirelease.LastSeenSetting, "v0.1.0"); err != nil {
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
	if got := m.startupNotice(); got != uinotices.Welcome {
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
	persistNoticeDismissal(t, m, uinotices.Welcome)

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
	if !strings.Contains(joined, "↑↓ · r · ↵ · x · esc") {
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
	m.openStartupNotice()
	if m.noticesOpen() {
		t.Fatal("a dev build must not open the startup modal")
	}
	if seen, _ := st.Setting(uirelease.LastSeenSetting); seen != "" {
		t.Fatalf("a dev build must not advance the stored version, got %q", seen)
	}
}

func TestStartupOpensNoticesModalOncePerVersion(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.openStartupNotice()
	if !m.noticesOpen() {
		t.Fatalf("first launch should open the notices modal, screen = %v", m.screenID())
	}
	if selected := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()]; selected.ID != uinotices.Welcome {
		t.Fatalf("welcome should be selected, got %q", selected.ID)
	}

	again := noticeModel(st, "v0.2.0")
	again.openStartupNotice()
	if again.noticesOpen() {
		t.Fatal("a later launch of the same version must not reopen the modal")
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
	m.handleKey(key("down"))
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
	if m.notices.pendingNotice != "feed-new" {
		t.Fatalf("pending notice = %q, want feed-new", m.notices.pendingNotice)
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
	if !m.onList() || !m.layout.split.resizeMode {
		t.Fatalf("resize should keep the modal closed, screen = %v resize=%v", m.screenID(), m.layout.split.resizeMode)
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

func TestFeedArrivingDuringWelcomeStaysOnWelcome(t *testing.T) {
	m := footModel(t)
	m.layout.width, m.layout.height = 100, 34
	m.openStartupNotice()
	if !m.noticesOpen() {
		t.Fatal("first launch should open welcome")
	}

	m.Update(uirelease.FeedMsg{Messages: []feed.Message{{ID: "feed-new", Banner: "new", Title: "Just in"}}})
	if !m.noticesOpen() {
		t.Fatalf("screen = %v, want notices", m.screenID())
	}
	if got := m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID; got != uinotices.Welcome {
		t.Fatalf("welcome should stay selected, got %q", got)
	}
	if !contains(noticeIDs(m.activeNotices()), "feed-new") {
		t.Fatal("feed should still be listed")
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
	for _, want := range []string{"messages", "v0.3.0 available", "You are on v0.2.0", "u update", "x dismiss"} {
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
	m.openNotices("update-v0.4.0")

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
	m.openNotices(uinotices.Welcome)
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

func TestArrowStepNoticeListedUntilDismissed(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	found := false
	for _, n := range m.activeNotices() {
		if n.ID == uinotices.ArrowStep {
			found = true
		}
	}
	if !found {
		t.Fatal("arrow-step beta notice missing from active notices")
	}
	persistNoticeDismissal(t, m, uinotices.ArrowStep)
	for _, n := range m.activeNotices() {
		if n.ID == uinotices.ArrowStep {
			t.Fatal("dismissed notice still listed")
		}
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
