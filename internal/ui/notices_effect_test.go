package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDismissNoticeDeduplicatesPendingEffect(t *testing.T) {
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.dismissNotice(noticeWelcome)
	m.dismissNotice(noticeWelcome)

	if len(m.effects.main.pending) != 1 {
		t.Fatalf("duplicate dismissal queued %d effects, want 1", len(m.effects.main.pending))
	}
	request, ok := m.effects.main.pending[0].request.(noticeDismissRequest)
	if !ok || request.id != noticeWelcome {
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
	m.notices.open(m, noticeWelcome)

	_ = m.notices.handleKey(m, key("x"))
	if contains(noticeIDs(m.notices.active(m)), noticeWelcome) {
		t.Fatal("accepted dismissal stayed visible while its write was pending")
	}
	cmd := m.nextEffectCmd()
	if cmd == nil {
		t.Fatal("dismissal did not queue persistence")
	}
	m.applyCmd(t, cmd)

	if !contains(noticeIDs(m.notices.active(m)), noticeWelcome) {
		t.Fatal("failed persistence did not restore the notice")
	}
	if m.mode != modeNotices || m.notices.active(m)[m.notices.noticeCursor].id != noticeWelcome {
		t.Fatalf("failed dismissal did not restore its modal selection: mode=%v cursor=%d", m.mode, m.notices.noticeCursor)
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
	m.notices.open(m, noticeWelcome)
	_ = m.notices.handleKey(m, key("x"))
	cmd := m.nextEffectCmd()

	updated, _ := m.handleKey(key("esc"))
	m = updated.(*Model)
	m.applyCmd(t, cmd)

	if m.mode != modeList {
		t.Fatalf("failed old dismissal stole focus from newer input: mode=%v", m.mode)
	}
	if !contains(noticeIDs(m.notices.active(m)), noticeWelcome) {
		t.Fatal("failed old dismissal did not restore the notice card")
	}
}

func TestDismissNoticeFailureDuringQuitRestoresWithoutReopening(t *testing.T) {
	st := noticeStore(t)
	if err := st.SetSetting(dismissedNoticesSetting, `{broken`); err != nil {
		t.Fatalf("seed malformed setting: %v", err)
	}
	m := noticeModel(st, "v0.2.0")
	// Leave one visible card so accepting its dismissal closes the modal.
	m.notices.dismissed[noticeArrowStep] = true
	m.notices.open(m, noticeWelcome)

	_ = m.notices.handleKey(m, key("x"))
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
	if !contains(noticeIDs(m.notices.active(m)), noticeWelcome) {
		t.Fatal("failed dismissal did not restore the notice data")
	}
	if !strings.Contains(m.errBar.text, "decode setting") {
		t.Fatalf("failure reason = %q", m.errBar.text)
	}
}
