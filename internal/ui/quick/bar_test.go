package quick

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// fakePrompt is a one-line prompt box holding typed text.
type fakePrompt struct {
	text     string
	released bool
}

func (p *fakePrompt) Focus()                             {}
func (p *fakePrompt) Blur()                              {}
func (p *fakePrompt) Fit(int, int)                       {}
func (p *fakePrompt) View() string                       { return p.text }
func (p *fakePrompt) StepRow(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (p *fakePrompt) ChipKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }
func (p *fakePrompt) Type(msg tea.KeyMsg) tea.Cmd        { p.text += string(msg.Runes); return nil }
func (p *fakePrompt) Release()                           { p.released = true }
func (p *fakePrompt) Pasting() bool                      { return false }
func (p *fakePrompt) Message() string                    { return strings.TrimSpace(p.text) }

// fakeHost drives the quick bar without a root model, on a CLI with no
// choices to pick.
type fakeHost struct {
	prompt   *fakePrompt
	target   Target
	selected bool
	settings map[string]string
	errs     []string
}

func (h *fakeHost) ChoiceCaps(string) spawn.Caps        { return spawn.Caps{} }
func (h *fakeHost) ChoiceRemoteHost() string            { return "" }
func (h *fakeHost) ChoiceAnswer(string) *spawn.Answer   { return nil }
func (h *fakeHost) ChoiceSetting(key string) string     { return h.settings[key] }
func (h *fakeHost) SaveChoiceSetting(key, value string) { h.settings[key] = value }
func (h *fakeHost) ReportErr(text string)               { h.errs = append(h.errs, text) }
func (h *fakeHost) ClearErr()                           {}
func (h *fakeHost) Prompt() spawn.Prompt                { return h.prompt }
func (h *fakeHost) Target() (Target, bool)              { return h.target, h.selected }

// The bar types into its prompt and hands the root a request for the
// rest: a cursor step, a worktree toggle, an answer, or a spawn.
func TestQuickBarThroughAFakeHost(t *testing.T) {
	h := &fakeHost{prompt: &fakePrompt{}, settings: map[string]string{}}
	q := NewBar(h, []string{"claude"}, 0, false)
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); request != (Request{}) || len(h.errs) != 1 {
		t.Fatalf("enter with nothing selected asked for %+v: %v", request, h.errs)
	}
	q.HandleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ship it"), Paste: true})
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyUp}); request.Move != -1 {
		t.Fatalf("up on a one-row prompt asked for %+v", request)
	}
	if _, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyCtrlT}); !request.Toggle {
		t.Fatalf("ctrl+t asked for %+v", request)
	}
	h.target, h.selected = Target{IsGroup: true, Group: "backend"}, true
	_, request := q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if got := request.Spawn; got == nil || *got != (spawn.Request{Tool: "claude", Group: "backend", Prompt: "ship it"}) {
		t.Fatalf("spawn = %+v", request.Spawn)
	}
	q.SetWorktree(true)
	if _, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); !request.Spawn.WorktreeSet || !request.Spawn.Worktree {
		t.Fatalf("a toggled worktree was not asked for: %+v", request.Spawn)
	}
	if q.Legend(h)[0] != [2]string{"↵", "send"} {
		t.Fatalf("legend = %v", q.Legend(h))
	}
	h.target = Target{Group: "backend"}
	_, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request != (Request{Send: true, Text: "ship it"}) {
		t.Fatalf("send = %+v", request)
	}
	if _, request = q.HandleKey(h, tea.KeyMsg{Type: tea.KeyEsc}); request != (Request{Close: true}) || !h.prompt.released {
		t.Fatalf("esc asked for %+v", request)
	}
}
