package onboarding

import (
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

type surfaceRoot struct{}

func (surfaceRoot) Paint(*Surface) string { return "setup" }

func TestSurfaceKeepsConfirmedChoicesInInputOrder(t *testing.T) {
	s := NewSurface(surfaceRoot{}, []string{"claude"}, Choices{Theme: "classic"}, true)
	s.SetProbe(map[string]bool{"claude": true}, func(name string) string { return name })
	var commands []tea.Cmd
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyEnter}, {Type: tea.KeyDown}, {Type: tea.KeyEnter}} {
		_, cmd := s.Key(nil, key)
		commands = append(commands, cmd)
	}
	// The last wake can arrive first; all confirmed choices still reach the root in order.
	if msg := commands[len(commands)-1]().(OutcomeMsg); msg.Surface != s {
		t.Fatal("wake lost its surface")
	}
	var saved []Choices
	for _, out := range s.TakeOutcomes() {
		if out.Action == Save {
			saved = append(saved, out.Choices)
		}
	}
	if len(saved) != 2 || saved[0].FullLayout || !saved[1].FullLayout {
		t.Fatalf("confirmed choices out of order: %+v", saved)
	}
	if len(s.TakeOutcomes()) != 0 {
		t.Fatal("a later wake would repeat confirmed choices")
	}
}

func TestSurfaceAutoPaletteUsesTheCurrentThemeFamily(t *testing.T) {
	s := NewSurface(surfaceRoot{}, []string{"claude"}, Choices{Theme: "classic"}, false)
	s.SetProbe(nil, func(name string) string { return uitheme.AutoName(name, true) })
	for _, tc := range []struct{ theme, want string }{{"catppuccin mocha", "catppuccin latte"}, {"kanagawa wave", "kanagawa lotus"}} {
		if got := s.Palette(Choices{Theme: tc.theme, ThemeAuto: true}, "classic"); got != tc.want {
			t.Fatalf("auto palette for %s = %s, want %s", tc.theme, got, tc.want)
		}
	}
}
