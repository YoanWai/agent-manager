package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

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

	var widths []int
	for _, line := range strings.Split(frame, "\n") {
		trimmed := strings.TrimRight(line, " ")
		if strings.ContainsAny(trimmed, "╭╰│") {
			widths = append(widths, len([]rune(trimmed)))
		}
	}
	if len(widths) == 0 {
		t.Fatal("no frame rows found")
	}
	for i, width := range widths {
		if width != widths[0] {
			t.Fatalf("frame rows must align: row %d is %d, first is %d\n%s", i, width, widths[0], frame)
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

func TestSettingsBugReportRowOpensIssue(t *testing.T) {
	m := footModel(t)
	m.cfg = config.Config{Tools: map[string]config.Tool{"claude": {Command: "cat"}}}
	m.openSettings()
	if m.mode != modeSettings {
		t.Fatalf("settings should open, mode=%v", m.mode)
	}

	var opened string
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	m.settings.field = settingsFieldBugReport
	m.handleSettingsKey(key("enter"))
	if !strings.Contains(opened, "issues/new") {
		t.Fatalf("enter should open the issue page, got %q", opened)
	}
	if m.mode != modeSettings {
		t.Fatal("the action row must not close settings")
	}

	m.handleSettingsKey(key("esc"))
	if m.mode != modeList {
		t.Fatal("esc should still save and close")
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
