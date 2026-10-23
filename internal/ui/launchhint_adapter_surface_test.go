package ui

import (
	"os"
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

func hintOpen(m *Model) bool {
	_, ok := surfaceOf[*uidialogs.LaunchHintSurface](m)
	return ok
}

// hintFix is the fix the open card holds, zero when none is open.
func hintFix(m *Model) launchFix {
	if fix, ok := m.launchHintFix(); ok {
		return *fix
	}
	return launchFix{}
}

// hintFixPtr is the open card's fix, for a test that sets up what a
// completion would have attached.
func hintFixPtr(m *Model) *launchFix {
	fix, ok := m.launchHintFix()
	if !ok {
		panic("no launch hint is open")
	}
	return fix
}

func hintKey(m *Model, msg tea.KeyMsg) tea.Cmd {
	_, cmd := m.surfaces[len(m.surfaces)-1].Key(m, msg)
	return cmd
}

// hintStartInstall asks for the install the way the card's key does, with
// or without the card still open.
func hintStartInstall(m *Model) { launchHintRoot{m}.StartInstall() }

func openHintFor(t *testing.T) (*Model, string) {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	image := tempImage(t, "refused.png")
	m.openLaunchHint(launchFix{text: "tool is missing", command: "true", script: "true", binary: "tool", images: []imageAttachment{{id: 1, path: image}}})
	if !hintOpen(m) {
		t.Fatal("test setup did not open the hint")
	}
	return m, image
}

// esc and a click off the card both give the launch up, and Leave drops
// the refused prompt's images exactly once.
func TestLaunchHintClosesOnEscAndBackdrop(t *testing.T) {
	for name, closeIt := range map[string]func(*Model) *Model{
		"esc":      func(m *Model) *Model { m.Update(tea.KeyMsg{Type: tea.KeyEsc}); return m },
		"backdrop": func(m *Model) *Model { return clickBackdrop(t, m) },
	} {
		m, image := openHintFor(t)
		if !mustTop(t, m).Dismissible() {
			t.Fatalf("%s: the hint holds no draft and must be dismissible", name)
		}
		m = closeIt(m)
		if hintOpen(m) || m.effects.quitting || !m.onList() {
			t.Errorf("%s: open=%t quitting=%t screen = %v", name, hintOpen(m), m.effects.quitting, m.screenID())
		}
		if _, err := os.Stat(image); !os.IsNotExist(err) {
			t.Errorf("%s: Leave left the refused prompt's image behind", name)
		}
	}
}

// The update dialog opens over the form it came from and gives the screen
// back on close, with no return mode to keep.
func TestLaunchHintOverTheFormLeavesItsModeAlone(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openForm())
	m.presentLaunchHint(launchFix{text: "pi is older", command: "true", binary: "pi", update: true})
	if !formOpen(m) || !hintOpen(m) {
		t.Fatalf("mode %v open %t, want the hint over the form", m.screenID(), hintOpen(m))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !formOpen(m) || hintOpen(m) {
		t.Fatalf("mode %v open %t, want the form back", m.screenID(), hintOpen(m))
	}
}

// A completion for an install the user has since replaced lands on nothing:
// with no hint open, the install result keeps its tracker and the images
// are cleaned up by the returned command.
func TestInstallStartAfterTheHintClosedKeepsTheTracker(t *testing.T) {
	m, _ := openHintFor(t)
	gen := m.gens.dialog
	m.Pop()
	cmd := m.applyInstallStart(installStartRequest{gen: gen, binary: "tool"}, installStartResult{install: &pendingInstall{name: "install-tool", sessionID: "s"}}, nil)
	if m.ledger.install == nil || hintOpen(m) {
		t.Fatalf("install %+v hint open %t, want the tracker kept and no card revived", m.ledger.install, hintOpen(m))
	}
	_ = cmd
	if _, quit := m.requestQuit(); m.effects.quitting {
		t.Fatalf("quit accepted with an install pending: %v", quit)
	}
}
