package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeNoticesHost is the whole surface the messages panel reaches: no store,
// no tmux, no root model.
type fakeNoticesHost struct {
	width, height int
	mode          mode
	sources       noticeSources
	refreshes     int
	updates       int
	dismissed     []string
	status        string
}

func (h *fakeNoticesHost) size() (int, int)             { return h.width, h.height }
func (h *fakeNoticesHost) currentMode() mode            { return h.mode }
func (h *fakeNoticesHost) setMode(next mode)            { h.mode = next }
func (h *fakeNoticesHost) noticeSources() noticeSources { return h.sources }
func (h *fakeNoticesHost) refreshNotices() tea.Cmd {
	h.refreshes++
	return func() tea.Msg { return nil }
}
func (h *fakeNoticesHost) startUpdate() tea.Cmd {
	h.updates++
	return func() tea.Msg { return nil }
}
func (h *fakeNoticesHost) dismissNotice(id string) { h.dismissed = append(h.dismissed, id) }
func (h *fakeNoticesHost) statusRow() string       { return h.status }

func fakeNoticeSources(latest string) noticeSources {
	return noticeSources{
		store:       true,
		update:      updateInfo{version: "v0.2.0", latest: latest},
		sessionKeys: keybind.DefaultSession(),
		listKeys:    keybind.DefaultList(),
	}
}

func TestNoticesPanelRunsOnANarrowHost(t *testing.T) {
	h := &fakeNoticesHost{width: 100, height: 40, mode: modeList, sources: fakeNoticeSources("v0.3.0")}
	p := &noticesPanel{noticesState{dismissed: map[string]bool{}}}

	p.open(h, noticeWelcome)
	if h.mode != modeNotices {
		t.Fatalf("open left mode %v", h.mode)
	}
	if got := noticeIDs(p.active(h)); !slices.Equal(got, []string{"update-v0.3.0", noticeWelcome, noticeArrowStep}) {
		t.Fatalf("notices = %v", got)
	}
	if p.noticeCursor != 1 {
		t.Fatalf("open selected %d, want the welcome", p.noticeCursor)
	}

	if cmd := p.handleKey(h, key("u")); cmd != nil || h.updates != 0 {
		t.Fatal("u off the update notice reached the host")
	}
	p.handleKey(h, key("up"))
	if cmd := p.handleKey(h, key("u")); cmd == nil || h.updates != 1 {
		t.Fatal("u on the update notice did not start the update")
	}
	if cmd := p.handleKey(h, key("r")); cmd == nil || h.refreshes != 1 {
		t.Fatal("r did not ask the host to refresh")
	}

	h.status = "refresh failed"
	if frame := strings.Join(p.view(h), "\n"); !strings.Contains(frame, "refresh failed") || !strings.Contains(frame, "v0.3.0 available") {
		t.Fatalf("view missed the host's status or the list:\n%s", frame)
	}

	p.handleKey(h, key("x"))
	if !slices.Equal(h.dismissed, []string{"update-v0.3.0"}) || h.mode != modeNotices {
		t.Fatalf("x: dismissed=%v mode=%v", h.dismissed, h.mode)
	}
	p.handleKey(h, key("esc"))
	if h.mode != modeList {
		t.Fatalf("esc left mode %v", h.mode)
	}
}

func TestNoticesPanelKeepsSelectionOnlyWhileOpen(t *testing.T) {
	h := &fakeNoticesHost{width: 100, height: 40, mode: modeList, sources: fakeNoticeSources("")}
	p := &noticesPanel{noticesState{dismissed: map[string]bool{}}}
	p.open(h, noticeArrowStep)

	p.keepSelection(h, func() { h.sources = fakeNoticeSources("v0.3.0") })
	if notices := p.active(h); notices[p.noticeCursor].id != noticeArrowStep {
		t.Fatalf("selection moved to %q", notices[p.noticeCursor].id)
	}

	h.mode = modeList
	p.noticeCursor = 0
	p.keepSelection(h, func() { h.sources = fakeNoticeSources("") })
	if p.noticeCursor != 0 {
		t.Fatalf("closed panel moved its cursor to %d", p.noticeCursor)
	}
}
