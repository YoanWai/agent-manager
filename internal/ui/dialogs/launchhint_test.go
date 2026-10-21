package dialogs

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
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
	card := LaunchHint{Text: "why\n\nhow", Command: "run"}.View(40, plain, plain, presentation.GlyphsFor(""))
	if card.Body != "why\n\nhow" || len(card.Hints) != 3 {
		t.Fatalf("card = %+v", card)
	}
	if card := (LaunchHint{Text: "why"}).View(40, plain, plain, presentation.GlyphsFor("")); len(card.Hints) != 1 {
		t.Fatalf("a read-only card offers %v", card.Hints)
	}
}

// A click anywhere on the paragraph that holds the command copies it, even
// when the card wraps the command across lines; the rest of the text reads.
func TestLaunchHintClickCopiesTheCommandParagraph(t *testing.T) {
	fix := LaunchHint{
		Text:    "claude is not installed.\n\ninstall it with: curl -fsSL https://claude.ai/install.sh | bash",
		Command: "curl -fsSL https://claude.ai/install.sh | bash",
	}
	for _, inner := range []int{80, 24} {
		body := strings.Split(fix.View(inner, lipgloss.NewStyle(), lipgloss.NewStyle(), presentation.GlyphsFor("")).Body, "\n")
		for line := range body {
			want := LaunchHintStay
			if line >= 2 {
				want = LaunchHintCopy
			}
			if got := fix.Click(line, inner); got != want {
				t.Errorf("inner %d line %d %q: click = %v, want %v", inner, line, body[line], got, want)
			}
		}
	}
	if got := (LaunchHint{Text: "x is not installed."}).Click(0, 80); got != LaunchHintStay {
		t.Fatalf("a dialog without a recipe copies nothing, got %v", got)
	}
}
