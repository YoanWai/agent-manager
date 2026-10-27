package ui

import (
	"strings"
	"testing"

	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDismissNoticeDeduplicatesPendingEffect(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.dismissNotice(uinotices.ArrowStep)
	m.dismissNotice(uinotices.ArrowStep)

	if len(m.effects.main.pending) != 1 {
		t.Fatalf("duplicate dismissal queued %d effects, want 1", len(m.effects.main.pending))
	}
	request, ok := m.effects.main.pending[0].request.(noticeDismissRequest)
	if !ok || request.id != uinotices.ArrowStep {
		t.Fatalf("queued request = %#v", m.effects.main.pending[0].request)
	}
	m.drainEffects(t)
}

func TestDismissNoticeFailureRestoresNotice(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(dismissedNoticesSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	m.openNotices(uinotices.ArrowStep)

	_, _ = m.handleNoticesKey(key("x"))
	if contains(noticeIDs(m.activeNotices()), uinotices.ArrowStep) {
		t.Fatal("accepted dismissal stayed visible while its write was pending")
	}
	cmd := m.nextEffectCmd()
	if cmd == nil {
		t.Fatal("dismissal did not queue persistence")
	}
	m.applyCmd(t, cmd)

	if !contains(noticeIDs(m.activeNotices()), uinotices.ArrowStep) {
		t.Fatal("failed persistence did not restore the notice")
	}
	if m.mode != modeNotices || m.activeNotices()[m.notices.Cursor()].ID != uinotices.ArrowStep {
		t.Fatalf("failed dismissal did not restore its modal selection: mode=%v cursor=%d", m.mode, m.notices.Cursor())
	}
	if !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("failure reason = %q", m.errBar.text)
	}
}

func TestDismissNoticeFailureDoesNotReopenAfterNewerInput(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(dismissedNoticesSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	m.openNotices(uinotices.ArrowStep)
	_, _ = m.handleNoticesKey(key("x"))
	cmd := m.nextEffectCmd()

	updated, _ := m.handleKey(key("esc"))
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if m.mode != modeList {
		t.Fatalf("failed old dismissal stole focus from newer input: mode=%v", m.mode)
	}
	if !contains(noticeIDs(m.activeNotices()), uinotices.ArrowStep) {
		t.Fatal("failed old dismissal did not restore the notice card")
	}
}

func TestDismissNoticeFailureDuringQuitRestoresWithoutReopening(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(dismissedNoticesSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	m.openNotices(uinotices.ArrowStep)

	_, _ = m.handleNoticesKey(key("x"))
	cmd := m.nextEffectCmd()
	if cmd == nil || m.mode != modeList {
		t.Fatalf("last dismissal was not accepted: cmd=%v mode=%v", cmd != nil, m.mode)
	}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(*Model)
	if !m.effects.quitting {
		t.Fatal("ctrl+c did not start draining the accepted dismissal")
	}
	m.applyCmd(t, cmd)

	if m.mode != modeList {
		t.Fatalf("failed dismissal reopened over quit: mode=%v", m.mode)
	}
	if !contains(noticeIDs(m.activeNotices()), uinotices.ArrowStep) {
		t.Fatal("failed dismissal did not restore the notice data")
	}
	if !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("failure reason = %q", m.errBar.text)
	}
}
