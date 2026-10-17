package dialogs

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestLaunchHintKeysNeedACommand(t *testing.T) {
	readOnly := LaunchHint{Text: "x is not installed."}
	for _, key := range []string{"c", "i"} {
		if got := readOnly.Key(runes(key)); got != LaunchHintStay {
			t.Fatalf("%s without a command = %v", key, got)
		}
	}
	fix := LaunchHint{Text: "x is not installed.", Command: "brew install x"}
	cases := map[string]LaunchHintAction{"c": LaunchHintCopy, "i": LaunchHintInstall, "q": LaunchHintClose, "j": LaunchHintStay}
	for key, want := range cases {
		if got := fix.Key(runes(key)); got != want {
			t.Fatalf("%s = %v, want %v", key, got, want)
		}
	}
	if got := fix.Key(tea.KeyMsg{Type: tea.KeyCtrlC}); got != LaunchHintQuit {
		t.Fatalf("ctrl+c = %v", got)
	}
}

func TestLaunchHintViewSetsParagraphsApart(t *testing.T) {
	plain := lipgloss.NewStyle()
	card := LaunchHint{Text: "why\n\nhow", Command: "run"}.View(40, plain, plain)
	if card.Body != "why\n\nhow" || len(card.Hints) != 3 {
		t.Fatalf("card = %+v", card)
	}
	if card := (LaunchHint{Text: "why"}).View(40, plain, plain); len(card.Hints) != 1 {
		t.Fatalf("a read-only card offers %v", card.Hints)
	}
}
