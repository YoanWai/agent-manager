package ui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
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
		store:           st,
		update:          updateInfo{version: version},
		dismissed:       loadDismissed(st),
		whatsNewVersion: loadWhatsNewVersion(st),
	}
}

func noticeIDs(notices []notice) []string {
	ids := make([]string, len(notices))
	for i, n := range notices {
		ids[i] = n.id
	}
	return ids
}

func TestActiveNoticesPinnedByDefault(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	ids := noticeIDs(m.activeNotices())
	for _, want := range []string{noticeWelcome, noticeBugReport} {
		if !contains(ids, want) {
			t.Fatalf("want %s among %v", want, ids)
		}
	}
}

func TestDismissPersistsAcrossRestart(t *testing.T) {
	st := noticeStore(t)
	m := noticeModel(st, "v0.2.0")
	m.dismissNotice(noticeWelcome)
	if contains(noticeIDs(m.activeNotices()), noticeWelcome) {
		t.Fatal("dismissed notice still active")
	}

	reopened := noticeModel(st, "v0.2.0")
	if contains(noticeIDs(reopened.activeNotices()), noticeWelcome) {
		t.Fatal("dismissal did not survive restart")
	}
	if !contains(noticeIDs(reopened.activeNotices()), noticeBugReport) {
		t.Fatal("other notices must stay")
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

	m.dismissNotice("update-v0.3.0")
	if contains(noticeIDs(m.activeNotices()), "update-v0.3.0") {
		t.Fatal("dismissed update notice still active")
	}

	m.update.latest = "v0.4.0"
	if !contains(noticeIDs(m.activeNotices()), "update-v0.4.0") {
		t.Fatal("a newer release must surface its own notice")
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
	reopened.dismissNotice("whatsnew-v0.2.0")
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
	m.feedMessages = []feed.Message{{
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

	m.dismissNotice("feed-holdoff")
	if contains(noticeIDs(m.activeNotices()), "feed-holdoff") {
		t.Fatal("dismissed feed notice still active")
	}
	reopened := noticeModel(st, "v0.2.0")
	reopened.feedMessages = m.feedMessages
	if contains(noticeIDs(reopened.activeNotices()), "feed-holdoff") {
		t.Fatal("feed dismissal must survive restart")
	}
}

func TestBugReportNoticeCarriesPrefilledURL(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	var bug notice
	for _, n := range m.activeNotices() {
		if n.id == noticeBugReport {
			bug = n
		}
	}
	if !strings.HasPrefix(bug.url, "https://github.com/YoanWai/agent-manager/issues/new") {
		t.Fatalf("bug report should open the new-issue page, got %q", bug.url)
	}
	if !strings.Contains(bug.url, "v0.2.0") {
		t.Fatalf("issue URL should carry the version, got %q", bug.url)
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
	m.snap = sysstat.Snapshot{
		CPUPercent: 42, CPUOK: true,
		MemPercent: 63, MemOK: true, MemUsed: 10 << 30, MemTotal: 16 << 30,
		DiskPercent: 71, DiskOK: true, DiskFree: 120 << 30,
	}
	return m
}

func TestRailFootPutsMessagesRightOfComputer(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(70)

	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "messages") {
		t.Fatalf("want a messages card, got %q", joined)
	}
	if !strings.Contains(joined, "welcome") {
		t.Fatalf("want the welcome banner, got %q", joined)
	}

	for _, line := range lines {
		clean := ansi.Strip(line)
		if !strings.Contains(clean, "messages") {
			continue
		}
		if strings.Index(clean, "messages") < strings.Index(ansi.Strip(lines[0]), "computer") {
			t.Fatalf("messages must sit right of computer, got %q", clean)
		}
		return
	}
	t.Fatal("MESSAGES header row not found")
}

func TestRailFootNarrowDropsMessages(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(34)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Contains(joined, "messages") {
		t.Fatalf("narrow rail should keep only the meters, got %q", joined)
	}
	if !strings.Contains(joined, "computer") {
		t.Fatalf("meters must survive, got %q", joined)
	}
}

func TestRailFootAllDismissedShowsOnlyMeters(t *testing.T) {
	m := footModel(t)
	for _, n := range m.activeNotices() {
		m.dismissNotice(n.id)
	}
	joined := ansi.Strip(strings.Join(m.railFootLines(70), "\n"))
	if strings.Contains(joined, "messages") {
		t.Fatalf("no notices means no panel, got %q", joined)
	}
}

func TestRailFootCardBorderAndFit(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(90)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(joined, corner) {
			t.Fatalf("card border missing %q:\n%s", corner, joined)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), bgSeq(noticeCardHex())) {
		t.Fatal("card interior missing its fill")
	}
	for i, line := range lines {
		if !strings.Contains(ansi.Strip(line), "│") {
			t.Fatalf("row %d missing the separator: %q", i, ansi.Strip(line))
		}
	}

	var top string
	for _, line := range lines {
		if strings.Contains(ansi.Strip(line), "╭") {
			top = line
		}
	}
	if got := lipgloss.Width(top); got >= 90 {
		t.Fatalf("card must hug its content, top border spans %d of 90", got)
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
	m.width, m.height = 100, 34
	m.mode = modeNotices
	return m
}

func TestOpenNoticesFromList(t *testing.T) {
	m := footModel(t)
	m.width, m.height = 100, 34
	m.mode = modeList
	m.handleKey(key("M"))
	if m.mode != modeNotices {
		t.Fatalf("M should open the notices modal, mode=%v", m.mode)
	}
}

func TestNoticesViewListsAndDetails(t *testing.T) {
	m := modalModel(t)
	frame := ansi.Strip(m.View())
	for _, want := range []string{"messages", "Welcome to agent-manager", "Found a bug?", "dismiss"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("modal missing %q:\n%s", want, frame)
		}
	}

	for _, terminal := range []int{100, 40} {
		m.width = terminal
		frame := ansi.Strip(m.View())
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
	m.feedMessages = []feed.Message{{
		ID:     "feed-long",
		Banner: "long banner",
		Title:  "Long body message",
		Body: []string{
			"Each group can choose inherit, on, or off for spawning sessions into their own git worktree, set when creating a group or editing it later; children inherit from their parents, and the global setting stays the root fallback.",
		},
	}}
	m.openNotices("feed-long")
	frame := ansi.Strip(m.View())
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
		if got := lipgloss.Width(line); got > m.width {
			t.Fatalf("line overflows terminal at %d: %q", got, line)
		}
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

	m.handleNoticesKey(key("down"))
	m.handleNoticesKey(key("enter"))
	if opened == "" {
		t.Fatal("enter should open the selected notice's url")
	}
	want := m.activeNotices()[1].url
	if opened != want {
		t.Fatalf("opened %q, want %q", opened, want)
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
	if selected := m.activeNotices()[m.noticeCursor]; selected.id != noticeWelcome {
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
	selected := m.activeNotices()[m.noticeCursor].id

	m.Update(updateMsg{latest: "v9.9.9", url: "https://example.com"})
	if got := m.activeNotices()[m.noticeCursor].id; got != selected {
		t.Fatalf("selection moved from %q to %q when the release arrived", selected, got)
	}
}

func TestLateDismissedReleaseKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	m.dismissNotice("update-v9.9.9")
	selected := m.activeNotices()[m.noticeCursor].id

	m.Update(updateMsg{latest: "v9.9.9", url: "https://example.com"})
	if got := m.activeNotices()[m.noticeCursor].id; got != selected {
		t.Fatalf("a dismissed release must not move the selection, went from %q to %q", selected, got)
	}
}

func TestLateFeedKeepsModalSelection(t *testing.T) {
	m := modalModel(t)
	m.handleNoticesKey(key("down"))
	selected := m.activeNotices()[m.noticeCursor].id

	m.Update(feedMsg{messages: []feed.Message{{ID: "feed-late", Banner: "late", Title: "Late"}}})
	if !contains(noticeIDs(m.activeNotices()), "feed-late") {
		t.Fatal("feed message should have landed")
	}
	if got := m.activeNotices()[m.noticeCursor].id; got != selected {
		t.Fatalf("selection moved from %q to %q when the feed arrived", selected, got)
	}
}

func TestUpdateNoticeAppliesOnU(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.update.latest = "v0.3.0"
	m.openNotices("update-v0.3.0")
	if m.mode != modeNotices || m.noticeCursor != 0 {
		t.Fatalf("update notice should open selected, mode=%v cursor=%d", m.mode, m.noticeCursor)
	}

	applied := ""
	orig := applyUpdate
	defer func() { applyUpdate = orig }()
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
	if !strings.HasPrefix(applied, "v0.3.0 ") {
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

func TestUOutsideUpdateNoticeDoesNothing(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.openNotices(noticeWelcome)
	_, cmd := m.handleNoticesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd != nil || m.update.applying {
		t.Fatal("u on a plain notice must not start an update")
	}
}
