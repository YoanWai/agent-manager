package ui

import (
	"strings"
	"testing"

	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDismissNoticeDeduplicatesPendingEffect(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.dismissNotice(uinotices.Welcome)
	m.dismissNotice(uinotices.Welcome)

	if len(m.effects.Main.Pending) != 1 {
		t.Fatalf("duplicate dismissal queued %d effects, want 1", len(m.effects.Main.Pending))
	}
	request, ok := m.effects.Main.Pending[0].Request.(noticeDismissRequest)
	if !ok || request.id != uinotices.Welcome {
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
	m.openNotices(uinotices.Welcome)

	_, _ = m.handleKey(key("x"))
	if contains(noticeIDs(m.activeNotices()), uinotices.Welcome) {
		t.Fatal("accepted dismissal stayed visible while its write was pending")
	}
	cmd := m.nextEffectCmd()
	if cmd == nil {
		t.Fatal("dismissal did not queue persistence")
	}
	m.applyCmd(t, cmd)

	if !contains(noticeIDs(m.activeNotices()), uinotices.Welcome) {
		t.Fatal("failed persistence did not restore the notice")
	}
	if !m.noticesOpen() || m.activeNotices()[noticesSurfaceOf(m).Panel().Cursor()].ID != uinotices.Welcome {
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
	m.openNotices(uinotices.Welcome)
	_, _ = m.handleKey(key("x"))
	cmd := m.nextEffectCmd()

	updated, _ := m.handleKey(key("esc"))
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if !m.onList() {
		t.Fatalf("failed old dismissal stole focus from newer input: screen = %v", m.screenID())
	}
	if !contains(noticeIDs(m.activeNotices()), uinotices.Welcome) {
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
	m.notices.Dismiss(uinotices.ArrowStep)
	m.openNotices(uinotices.Welcome)

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
	if !contains(noticeIDs(m.activeNotices()), uinotices.Welcome) {
		t.Fatal("failed dismissal did not restore the notice data")
	}
	if !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("failure reason = %q", m.errBar.text)
	}
}
