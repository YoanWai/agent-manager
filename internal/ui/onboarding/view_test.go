package onboarding

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func viewContext(width, height int) ViewContext {
	return ViewContext{
		Width:     width,
		Height:    height,
		GlyphMode: presentation.GlyphModeAdvanced,
		Theme:     uitheme.Palettes[0],
		Wordmark:  "agent-manager",
		Keys:      Keys{Prompt: "space", Focus: "↵", Attach: "A", Detach: "ctrl+q", Settings: "s", Messages: "M", Help: "?"},
		Preview: func(p Preview) []string {
			lines := make([]string, p.Height)
			for i := range lines {
				lines[i] = strings.Repeat("#", p.Width)
			}
			return lines
		},
	}
}

func TestViewFitsCommonTerminals(t *testing.T) {
	for _, size := range [][2]int{{80, 21}, {120, 33}, {160, 42}, {200, 47}} {
		for _, step := range Steps {
			m := New(agents, defaults(), false)
			walkTo(t, &m, step)
			frame := m.View(viewContext(size[0], size[1]))
			if len(frame.Lines) != size[1] {
				t.Fatalf("%dx%d %v painted %d rows", size[0], size[1], step, len(frame.Lines))
			}
			for i, line := range frame.Lines {
				if width := ansi.StringWidth(line); width > size[0] {
					t.Fatalf("%dx%d %v row %d is %d wide:\n%s", size[0], size[1], step, i, width, ansi.Strip(line))
				}
			}
		}
	}
}

func TestNarrowTerminalsDropThePreview(t *testing.T) {
	m := New(agents, defaults(), false)
	frame := m.View(viewContext(80, 21))
	if strings.Contains(strings.Join(frame.Lines, "\n"), "#") {
		t.Fatal("an 80 column terminal drew the preview")
	}
	frame = m.View(viewContext(120, 33))
	if !strings.Contains(strings.Join(frame.Lines, "\n"), "#") {
		t.Fatal("a 120 column terminal left the preview out")
	}
}

func TestClickActsOnThePaintedOption(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Agents)
	frame := m.View(viewContext(120, 33))
	row := rowOf(t, frame, "codex")
	m.Click(row)
	if !m.chosen.Hidden["codex"] {
		t.Fatal("a click on codex did not uncheck it")
	}
	m.Click(0)
	if len(m.chosen.Hidden) != 1 {
		t.Fatal("a click off the options changed them")
	}

	walkTo(t, &m, Theme)
	frame = m.View(viewContext(120, 33))
	m.Click(rowOf(t, frame, "nord"))
	if m.chosen.Theme != "nord" {
		t.Fatalf("a click on nord chose %q", m.chosen.Theme)
	}
}

// The guide names the keys as bound now, and leaves out an action that is
// turned off.
func TestGuideReadsTheKeyTables(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, Sessions)
	ctx := viewContext(120, 33)
	ctx.Keys.Focus, ctx.Keys.Attach, ctx.Keys.Detach = "A", "↵", "f9"
	ctx.Keys.Prompt = ""
	text := ansi.Strip(strings.Join(m.View(ctx).Lines, "\n"))
	for _, want := range []string{" A    focus it", " ↵    attach it", " f9   come back"} {
		if !strings.Contains(text, want) {
			t.Errorf("guide missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "answer the selected agent") {
		t.Errorf("an unbound key kept its line:\n%s", text)
	}
}

func TestCompatibleSymbolsSpellTheKeys(t *testing.T) {
	m := New(agents, defaults(), false)
	walkTo(t, &m, QuickPrompt)
	ctx := viewContext(120, 33)
	ctx.GlyphMode = presentation.GlyphModeBasic
	text := ansi.Strip(strings.Join(m.View(ctx).Lines, "\n"))
	if strings.ContainsAny(text, "↵●✓") || !strings.Contains(text, "enter") {
		t.Fatalf("compatible symbols drew advanced glyphs:\n%s", text)
	}
}

func rowOf(t *testing.T, frame Frame, text string) int {
	t.Helper()
	for i, line := range frame.Lines {
		if strings.Contains(ansi.Strip(line), text) {
			return i
		}
	}
	t.Fatalf("no row paints %q", text)
	return -1
}
