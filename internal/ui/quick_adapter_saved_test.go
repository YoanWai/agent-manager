package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// ctrl+o in the quick prompt puts a saved prompt at the caret as a chip,
// and the send carries its text where the chip stood.
func TestQuickSavedPromptChipSendsItsText(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Role", " \n instruct\n")
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	var sent string
	m.services.quickSendText = func(_ *tmux.Driver, _ string, text string) (tmux.SendResult, error) {
		sent = text
		return tmux.SendResult{}, nil
	}
	m.quick.input.SetValue("then ")
	_, load := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	m.runBatch(t, load)
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.quick.input.Value(); got != "then "+savedToken("Role")+" " {
		t.Fatalf("chip landed as %q", got)
	}
	if !strings.Contains(ansi.Strip(m.quick.view()), "[saved prompt:Role]") {
		t.Fatal("the chip is not painted")
	}
	for _, r := range "go" {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if sent != "then  \n instruct\n go" || m.errBar.text != "" {
		t.Fatalf("sent %q, err %q", sent, m.errBar.text)
	}
	if m.quick.input.Value() != "" || len(m.quick.saved) != 0 {
		t.Fatal("a confirmed send kept the chip")
	}
}
