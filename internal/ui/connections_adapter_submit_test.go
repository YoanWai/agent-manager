package ui

import (
	"slices"
	"testing"

	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
)

func TestRemoteQuickBurstKeepsEachPromptAndTarget(t *testing.T) {
	for _, firstSpawn := range []bool{false, true} {
		name := "send then send"
		if firstSpawn {
			name = "spawn then send"
		}
		t.Run(name, func(t *testing.T) {
			fake := &fakeSSH{}
			m := connectedModel(t, fake)
			m.rail.Focus(boxSession("s1"))
			if firstSpawn {
				m.rail.Focus(boxGroup)
			}
			m.openQuickMode()
			m.quick().Input.SetValue("first")
			_, first := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
			if first == nil {
				t.Fatal("first Enter did not accept a request")
			}
			firstRequest := m.effects.Remote.Active.Request.(uilink.Call)
			m.rail.Focus(boxSession("s1"))
			m.applyRailDecision(uirail.Decision{SelectionEffect: uirail.SelectionEffectPreview})
			m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("second")})
			if got := m.quick().Input.Value(); got != "second" {
				t.Fatalf("next prompt = %q", got)
			}
			m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
			if len(m.effects.Remote.Pending) != 1 {
				t.Fatalf("later Enter refused, status=%q", m.errBar.text)
			}
			secondRequest := m.effects.Remote.Pending[0].Request.(uilink.Call)
			if secondRequest.Submission.ID == firstRequest.Submission.ID {
				t.Fatal("submissions share an identity")
			}
			m.runRemoteCmd(t, first)
			calls := fake.taken()
			if len(calls) != 2 || !slices.Equal(calls[1], []string{"type", "--json", "--", "s1", "second"}) {
				t.Fatalf("remote calls = %q", calls)
			}
		})
	}
}
