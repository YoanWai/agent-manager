package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestRemoteReviewBlocksLocalMutationsAndEditor(t *testing.T) {
	for _, key := range []string{"b", "B", " ", "c", "d", "C", "o"} {
		t.Run(key, func(t *testing.T) {
			m := &Model{federation: &nativeFederation{remoteNames: map[string]bool{"remote": true}}}
			m.diff.sessID = "remote::session"
			_, cmd := m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			if cmd != nil || !strings.Contains(m.diff.notice, "read-only") {
				t.Fatalf("unsafe remote key %q: %q", key, m.diff.notice)
			}
			if m.saveReviewStateCmd() != nil {
				t.Fatal("remote review writes local state")
			}
		})
	}
}
