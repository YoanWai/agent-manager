package onboarding

import (
	"maps"
	"slices"
	"testing"

	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

var agents = []string{"claude", "codex", "gemini"}

func press(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func defaults() Choices { return Choices{Theme: uitheme.Palettes[0].Name} }

func walkTo(t *testing.T, m *Model, step Step) {
	t.Helper()
	for m.Step() != step {
		m.Key(press("enter"))
	}
}

func TestConfirmingAStepSavesItsChoiceAlone(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Agents)
	m.Key(press("down"))
	m.Key(press("space"))
	if out := m.Key(press("enter")); out.Action != Save || !out.Choices.Hidden["codex"] || len(out.Choices.Hidden) != 1 {
		t.Fatalf("enter on the agents step = %+v", out)
	}
	m.Key(press("down"))
	if out := m.Key(press("backspace")); out.Action != Stay {
		t.Fatalf("back saved = %+v", out)
	}
	m.Key(press("enter"))
	if m.Step() != Layout {
		t.Fatalf("enter from agents went to %v", m.Step())
	}
	out := m.Key(press("enter"))
	if out.Action != Save || !out.Choices.FullLayout || !out.Choices.Hidden["codex"] {
		t.Fatalf("enter on the layout step = %+v", out)
	}
}

// esc asks on every step, and leaving keeps only what the confirmed steps
// saved.
func TestLeavingAsksAndKeepsTheConfirmedSteps(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Layout)
	m.Key(press("down"))
	if out := m.Key(press("esc")); out.Action != Stay || !m.Leaving() {
		t.Fatalf("esc on a choice = %+v, leaving %v", out, m.Leaving())
	}
	if out := m.Key(press("n")); out.Action != Stay || m.Leaving() || m.Step() != Layout {
		t.Fatalf("n = %+v on %v, leaving %v", out, m.Step(), m.Leaving())
	}
	m.Key(press("enter"))
	walkTo(t, &m, Sessions)
	m.Key(press("esc"))
	out := m.Key(press("enter"))
	if out.Action != Close || !out.Choices.FullLayout || out.Choices.Theme != uitheme.Palettes[0].Name {
		t.Fatalf("enter on the question = %+v", out)
	}
}

func TestTheQuestionHoldsTheStep(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Agents)
	m.Key(press("esc"))
	for _, key := range []string{"down", "space", "right", "q"} {
		if out := m.Key(press(key)); out.Action != Stay || !m.Leaving() {
			t.Fatalf("%s while asking = %+v, leaving %v", key, out, m.Leaving())
		}
	}
	if len(m.chosen.Hidden) != 0 || m.Step() != Agents {
		t.Fatalf("keys behind the question moved the setup: %+v on %v", m.chosen.Hidden, m.Step())
	}
	m.Key(press("esc"))
	if m.Leaving() {
		t.Fatal("a second esc did not cancel the question")
	}
}

func TestLeavingTheThemeStepShowsTheSavedTheme(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Theme)
	m.Key(press("down"))
	m.Key(press("backspace"))
	if shown := m.Shown(); shown.Theme != uitheme.Palettes[0].Name {
		t.Fatalf("stepping back shows %q, want the saved theme", shown.Theme)
	}
	m.Key(press("enter"))
	if shown := m.Shown(); shown.Theme != uitheme.Palettes[1].Name {
		t.Fatalf("returning shows %q, want the cursor's theme again", shown.Theme)
	}
}

// Picking a palette by hand turns following the OS off, as stepping the
// theme in Settings does. Space turns it back on from any palette, so the
// arrows never walk through other palettes to reach it.
func TestPickingAPaletteStopsFollowingTheOS(t *testing.T) {
	m := New(agents, Choices{Theme: uitheme.Palettes[0].Name, ThemeAuto: true}, false)
	walkTo(t, &m, Theme)
	m.Key(press("down"))
	if m.Shown().ThemeAuto {
		t.Fatal("a palette picked by hand still follows the OS")
	}
	m.Key(press("space"))
	if shown := m.Shown(); !shown.ThemeAuto || shown.Theme != uitheme.Palettes[1].Name {
		t.Fatalf("space on a palette = %+v", shown)
	}
	m.Key(press("up"))
	m.Key(press("up"))
	if shown := m.Shown(); shown.Theme != uitheme.Palettes[len(uitheme.Palettes)-1].Name {
		t.Fatalf("up from the first palette wraps to the last, got %q", shown.Theme)
	}
}

func TestOneAgentStaysChecked(t *testing.T) {
	m := New([]string{"claude", "codex"}, defaults(), false)
	walkTo(t, &m, Agents)
	m.Key(press("space"))
	m.Key(press("down"))
	m.Key(press("space"))
	if got := m.Enabled(); !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("enabled = %v", got)
	}
	if m.notice == "" {
		t.Fatal("unchecking the last agent said nothing")
	}
}

func TestFirstRunChecksTheInstalledAgents(t *testing.T) {
	m := New(agents, defaults(), true)
	m.SetInstalled(map[string]bool{"codex": true})
	if got := m.Enabled(); !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("first run enabled = %v", got)
	}

	nothing := New(agents, defaults(), true)
	nothing.SetInstalled(map[string]bool{})
	if got := nothing.Enabled(); !slices.Equal(got, agents) {
		t.Fatalf("nothing found should keep every agent, got %v", got)
	}

	touched := New(agents, defaults(), true)
	walkTo(t, &touched, Agents)
	touched.Key(press("space"))
	touched.SetInstalled(map[string]bool{"codex": true})
	if got := touched.Enabled(); !slices.Equal(got, []string{"codex", "gemini"}) {
		t.Fatalf("a toggle before the lookup should win, got %v", got)
	}

	reopened := New(agents, Choices{Hidden: map[string]bool{"gemini": true}}, false)
	reopened.SetInstalled(map[string]bool{"codex": true})
	if got := reopened.Enabled(); !slices.Equal(got, []string{"claude", "codex"}) {
		t.Fatalf("a reopened setup should keep the saved agents, got %v", got)
	}
}

// The last step ends only by starting an import, or by leaving.
func TestTheLastStepNeedsAnImport(t *testing.T) {
	m := New(agents, defaults(), false)
	saves := 0
	for range len(Steps) - 1 {
		if m.Key(press("enter")).Action == Save {
			saves++
		}
	}
	if saves != 4 || m.Step() != Steps[len(Steps)-1] {
		t.Fatalf("walked to %v with %d saves", m.Step(), saves)
	}
	for _, key := range []string{"enter", "right", "tab"} {
		if out := m.Key(press(key)); out.Action != Stay || m.Step() != Import {
			t.Fatalf("%s on the last step with nothing checked = %+v", key, out)
		}
	}
	if m.Key(press("enter")); m.notice == "" {
		t.Fatal("enter with nothing checked said nothing")
	}
}

func TestHintsFollowTheStep(t *testing.T) {
	m := New(agents, defaults(), false)
	labels := func() []string {
		var out []string
		for _, hint := range m.Hints() {
			out = append(out, hint[1])
		}
		return out
	}
	if got := labels(); !slices.Equal(got, []string{"set it up", "skip setup"}) {
		t.Fatalf("welcome hints = %v", got)
	}
	walkTo(t, &m, Agents)
	if got := labels(); !slices.Equal(got, []string{"move", "check", "next", "back", "leave setup"}) {
		t.Fatalf("agents step hints = %v", got)
	}
	walkTo(t, &m, Import)
	if got := labels(); !slices.Equal(got, []string{"back", "leave setup"}) {
		t.Fatalf("last step hints with nothing to import = %v", got)
	}
}

func TestSavedChoicesAreCopies(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Agents)
	m.Key(press("space"))
	out := m.Key(press("enter"))
	before := maps.Clone(out.Choices.Hidden)
	m.Key(press("backspace"))
	m.Key(press("down"))
	m.Key(press("space"))
	if !maps.Equal(out.Choices.Hidden, before) {
		t.Fatal("a later toggle changed the choices already handed out")
	}
}

func TestEveryAgentHiddenStartsWithAllChecked(t *testing.T) {
	hidden := map[string]bool{"claude": true, "codex": true, "gemini": true}
	m := New(agents, Choices{Hidden: hidden, Theme: uitheme.Palettes[0].Name}, false)
	if len(m.Enabled()) != len(agents) {
		t.Fatalf("enabled = %v, want every agent", m.Enabled())
	}
}
