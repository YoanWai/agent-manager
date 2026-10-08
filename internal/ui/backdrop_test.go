package ui

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/termseq"
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
	r, g, b := hexRGB(hex)
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

// Every cell the frame left on the terminal's colors takes the theme's,
// whichever way the reset was spelled, and every cell that carried a color
// of its own keeps it.
func TestFillBackdropPaintsOnlyDefaultCells(t *testing.T) {
	useTrueColor(t)
	const width = 24
	chip := "\x1b[48;2;1;2;3m"
	frame := strings.Join([]string{
		"plain text",
		lipgloss.NewStyle().Foreground(lipgloss.Color("#123456")).Render("styled") + " tail",
		chip + "chip\x1b[49m after",
		chip + "chip\x1b[m after",
		chip + "chip\x1b[0;1;38;2;0;0;0m black on default\x1b[0m",
		chip + "a\x1b[38;5;0mb\x1b[38;2;0;0;0mc stays on chip\x1b[0m",
		chip + "A\x1b[4:0mB\x1b[4:3mC stays on chip\x1b[0m",
		"\x1b[48:2::9:8:7mcolon\x1b[38:2::0:0:0m fg only\x1b[0m",
		"\x1b[44mindexed\x1b[0m wide 界 cell",
		"\x1b[31mred\x1b[39m default ink \x1b[1;91mbright\x1b[22;39m ink",
		"",
	}, "\n")
	height := strings.Count(frame, "\n") + 1

	beforeBg, beforeFg := cellColors(frame, width, height)
	afterBg, afterFg := cellColors(fillBackdrop(frame, width, "#f7f7f5", "#33333a"), width, height)
	for y := range height {
		for x := range width {
			if want := orTheme(beforeBg[y][x], "#f7f7f5"); !sameColor(afterBg[y][x], want) {
				t.Errorf("row %d col %d background = %v, want %v", y, x, afterBg[y][x], want)
			}
			if want := orTheme(beforeFg[y][x], "#33333a"); !sameColor(afterFg[y][x], want) {
				t.Errorf("row %d col %d foreground = %v, want %v", y, x, afterFg[y][x], want)
			}
		}
	}
}

func orTheme(own color.Color, theme string) color.Color {
	if own == nil {
		return hexColor(theme)
	}
	return own
}

func TestFillBackdropKeepsText(t *testing.T) {
	useTrueColor(t)
	frame := "one \x1b[1mtwo\x1b[m\n\x1b[38;5;42mthree\x1b[39m"
	got := strings.Split(ansi.Strip(fillBackdrop(frame, 8, "#f7f7f5", "#33333a")), "\n")
	want := []string{"one two ", "three   "}
	if !slices.Equal(got, want) {
		t.Fatalf("filled text = %q, want %q", got, want)
	}
}

// The frame a terminal that ignores OSC 10 and 11 shows: light chrome and
// the captured agent output must carry the theme's colors in every cell.
func TestViewPaintsEveryCellWithTheThemeColors(t *testing.T) {
	useTrueColor(t)
	usePaper(t)
	m := shotModel()
	backgrounds, foregrounds := cellColors(preparedView(m), m.width, m.height)
	for y := range m.height {
		for x := range m.width {
			if backgrounds[y][x] == nil {
				t.Fatalf("row %d col %d shows the terminal's own background", y, x)
			}
			if foregrounds[y][x] == nil {
				t.Fatalf("row %d col %d shows the terminal's own foreground", y, x)
			}
		}
	}
	if corner := backgrounds[m.height-1][m.width-1]; !sameColor(corner, hexColor(current.Bg)) {
		t.Errorf("bottom right cell = %v, want the backdrop %s", corner, current.Bg)
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
	for m.settings.field != settingsFieldBackground {
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
	m.settings.field = settingsFieldBackground
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
