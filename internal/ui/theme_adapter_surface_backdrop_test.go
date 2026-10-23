package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/termseq"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/cellbuf"
	"github.com/muesli/termenv"
)

// cellColors lays a frame out the way a terminal would and reports each
// cell's background and foreground, nil where the terminal's own color shows.
func cellColors(frame string, width, height int) (backgrounds, foregrounds [][]color.Color) {
	buffer := cellbuf.NewBuffer(width, height)
	cellbuf.SetContent(buffer, frame)
	backgrounds = make([][]color.Color, height)
	foregrounds = make([][]color.Color, height)
	for y := range height {
		backgrounds[y] = make([]color.Color, width)
		foregrounds[y] = make([]color.Color, width)
		for x := range width {
			cell := buffer.Cell(x, y)
			if cell.Width == 0 && x > 0 {
				// The trailing half of a wide character wears its lead's colors.
				backgrounds[y][x], foregrounds[y][x] = backgrounds[y][x-1], foregrounds[y][x-1]
				continue
			}
			backgrounds[y][x], foregrounds[y][x] = cell.Style.Bg, cell.Style.Fg
		}
	}
	return backgrounds, foregrounds
}

func sameColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

func hexColor(hex string) color.Color {
	r, g, b := uitheme.HexRGB(hex)
	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 0xff}
}

func useTrueColor(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func usePaper(t *testing.T) {
	t.Helper()
	applyTheme(themes[themeIndex("paper")])
	t.Cleanup(func() { applyTheme(themes[0]) })
}

// The frame a terminal that ignores OSC 10 and 11 shows: light chrome and
// the captured agent output must carry the theme's colors in every cell.
func TestViewPaintsEveryCellWithTheThemeColors(t *testing.T) {
	useTrueColor(t)
	usePaper(t)
	m := shotModel()
	backgrounds, foregrounds := cellColors(preparedView(m), m.layout.width, m.layout.height)
	for y := range m.layout.height {
		for x := range m.layout.width {
			if backgrounds[y][x] == nil {
				t.Fatalf("row %d col %d shows the terminal's own background", y, x)
			}
			if foregrounds[y][x] == nil {
				t.Fatalf("row %d col %d shows the terminal's own foreground", y, x)
			}
		}
	}
	if corner := backgrounds[m.layout.height-1][m.layout.width-1]; !sameColor(corner, hexColor(current().Bg)) {
		t.Errorf("bottom right cell = %v, want the backdrop %s", corner, current().Bg)
	}
}

func TestSyncTerminalColorsSetsTextAndBackground(t *testing.T) {
	usePaper(t)
	var sink strings.Builder
	previous := termseq.Out
	termseq.Out = &sink
	t.Cleanup(func() { termseq.Out = previous })

	SyncTerminalColors()
	if got, want := sink.String(), "\x1b]10;#33333a\x07\x1b]11;#f7f7f5\x07"; got != want {
		t.Fatalf("sync wrote %q, want %q", got, want)
	}
	sink.Reset()
	ResetTerminalColors()
	if got, want := sink.String(), "\x1b]110\x07\x1b]111\x07"; got != want {
		t.Fatalf("reset wrote %q, want %q", got, want)
	}
}

func TestTerminalBackgroundLeavesTheFrameAsDrawn(t *testing.T) {
	useTrueColor(t)
	usePaper(t)
	m := shotModel()
	m.prefs.terminalBackground = true
	if got, want := preparedView(m), m.renderFrame(); got != want {
		t.Fatalf("terminal background repainted the frame:\n%q\nwant\n%q", got, want)
	}
}

// Without colors the backdrop has no sequence of its own, and a bare reset
// in its place would strip the bold and reverse cells around it.
func TestColorlessTerminalLeavesTheFrameAsDrawn(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	m := shotModel()
	if got, want := preparedView(m), m.renderFrame(); got != want {
		t.Fatalf("colorless frame repainted:\n%q\nwant\n%q", got, want)
	}
}

func TestBackgroundSettingAppliesLiveAndPersists(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	for m.settingsCard().State().Field != uisettings.FieldBackground {
		m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if row := settingsRow(t, m, "background"); !strings.Contains(row, "theme") {
		t.Fatalf("background should default to the theme's: %q", row)
	}

	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRight})
	if row := settingsRow(t, m, "background"); !strings.Contains(row, "terminal") {
		t.Fatalf("stepped background row = %q, want terminal", row)
	}
	if !m.prefs.terminalBackground {
		t.Fatal("the terminal background should apply while the picker is open")
	}
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if !m.storedTerminalBackground() {
		t.Fatal("terminal background not persisted")
	}

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldBackground
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyLeft})
	m.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.drainEffects(t)
	if m.prefs.terminalBackground || m.storedTerminalBackground() {
		t.Fatal("stepping back should return to the theme's background")
	}
}

func settingsRow(t *testing.T, m *Model, label string) string {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(m.viewSettings()), "\n") {
		if strings.Contains(line, " "+label+" ") {
			return line
		}
	}
	t.Fatalf("settings missing the %s row:\n%s", label, ansi.Strip(m.viewSettings()))
	return ""
}
