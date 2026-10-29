package quick

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// effectsHost records the root work a bar key asked for.
type effectsHost struct {
	fakeHost
	moved   int
	toggled int
	closed  int
	sent    []string
	spawned []spawn.Request
	loaded  int
}

func (h *effectsHost) MoveCursor(delta int) tea.Cmd { h.moved += delta; return nil }
func (h *effectsHost) ToggleWorktree() tea.Cmd      { h.toggled++; return nil }
func (h *effectsHost) Close()                       { h.closed++ }
func (h *effectsHost) LoadSaved() tea.Cmd           { h.loaded++; return nil }
func (h *effectsHost) Send(text string) tea.Cmd     { h.sent = append(h.sent, text); return nil }
func (h *effectsHost) Spawn(request spawn.Request) tea.Cmd {
	h.spawned = append(h.spawned, request)
	return nil
}

func (h *effectsHost) Key(tea.KeyMsg) (tea.Cmd, bool) { return nil, false }

func (h *effectsHost) Mouse(tea.MouseMsg) tea.Cmd { return nil }

// A key the bar cannot finish by itself comes back as the one thing the
// root runs, and a key the bar consumed asks for nothing.
func TestDecideHandsTheRootOneEffectPerKey(t *testing.T) {
	t.Parallel()
	h := &effectsHost{fakeHost: fakeHost{prompt: &fakePrompt{}, settings: map[string]string{}, selected: true, target: Target{IsGroup: true, Group: "web"}}}
	q := NewSurface(h)
	q.Bar = NewBar(h, []string{"claude"}, 0, false)

	Decide(h, q, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Paste: true})
	if h.moved != 0 || h.toggled != 0 || h.closed != 0 || len(h.sent) != 0 {
		t.Fatalf("typing asked the root for work: %+v", h)
	}

	Decide(h, q, tea.KeyMsg{Type: tea.KeyUp})
	if h.moved != -1 {
		t.Fatalf("up asked for a move of %d", h.moved)
	}

	Decide(h, q, tea.KeyMsg{Type: tea.KeyCtrlT})
	if h.toggled != 1 {
		t.Fatalf("ctrl+t asked for %d toggles", h.toggled)
	}

	Decide(h, q, tea.KeyMsg{Type: tea.KeyEsc})
	if h.closed != 1 {
		t.Fatalf("esc asked for %d closes", h.closed)
	}

	Decide(h, q, tea.KeyMsg{Type: tea.KeyCtrlO})
	if h.loaded != 1 {
		t.Fatalf("ctrl+o asked for %d loads", h.loaded)
	}
}

// Submit on a group target asks the root to spawn with the typed prompt.
func TestDecideSpawnsOnEnterOverAGroup(t *testing.T) {
	t.Parallel()
	prompt := &fakePrompt{}
	h := &effectsHost{fakeHost: fakeHost{prompt: prompt, settings: map[string]string{}, selected: true, target: Target{IsGroup: true, Group: "web"}}}
	q := NewSurface(h)
	q.Bar = NewBar(h, []string{"claude"}, 0, false)

	Decide(h, q, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ship it"), Paste: true})
	Decide(h, q, tea.KeyMsg{Type: tea.KeyEnter})

	if len(h.spawned) != 1 || h.spawned[0].Prompt != "ship it" || h.spawned[0].Group != "web" {
		t.Fatalf("enter asked to spawn %+v", h.spawned)
	}
	if len(h.sent) != 0 {
		t.Fatalf("a group target must spawn, not answer: %v", h.sent)
	}
}
