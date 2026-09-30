package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestClampFrame(t *testing.T) {
	tall := "a\nb\nc\nd\ne"
	got := clampFrame(tall, 3)
	if got != "a\nb\nc" {
		t.Fatalf("clampFrame trim = %q", got)
	}
	short := "x"
	got = clampFrame(short, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || lines[0] != "x" {
		t.Fatalf("clampFrame pad = %q", got)
	}
	for _, line := range lines[1:] {
		// Padding rows wear the backdrop fill rather than being bare, so
		// a short frame cannot show the terminal's background through its
		// bottom rows.
		if !strings.Contains(line, " ") {
			t.Fatalf("padding row is bare: %q", line)
		}
	}
}

// TestZZShot renders a full frame to disk for visual review. Skipped
// unless AM_SHOT names an output path.
func TestZZShot(t *testing.T) {
	out := os.Getenv("AM_SHOT")
	if out == "" {
		t.Skip("set AM_SHOT to render a frame")
	}
	if name := os.Getenv("AM_SHOT_THEME"); name != "" {
		applyTheme(themes[themeIndex(name)])
		t.Cleanup(func() { applyTheme(themes[0]) })
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	switch os.Getenv("AM_SHOT_MODE") {
	case "settings":
		m.mode = modeSettings
		m.settings = settingsState{
			toolNames: []string{"claude", "codex", "grok", "opencode"},
			toolIndex: 0, themeIndex: 1, field: settingsFieldTheme, layoutSplit: true,
		}
		m.update.version = "0.9.2"
	case "help":
		m.mode = modeHelp
	}
	if phase := os.Getenv("AM_SHOT_PHASE"); phase != "" {
		n, err := strconv.Atoi(phase)
		if err != nil {
			t.Fatal(err)
		}
		m.startup.bannerPhase = n
	}
	if err := os.WriteFile(out, []byte(m.View()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A frame line wider than the terminal wraps, which pushes the whole
// layout down a row and tears the panels. A frame with more rows than the
// terminal loses its footer to the clamp. Both have to hold at every size
// the layout still makes sense at, including the width where the header
// swaps between the wordmark and its one-line form.
func TestFrameFitsTerminal(t *testing.T) {
	for _, width := range []int{80, 100, 120, 160, 200} {
		for _, height := range []int{24, 34, 50} {
			m := shotModel()
			m.width, m.height = width, height
			// View clamps and pads, which would hide a frame that is a row
			// short or a row long, so the raw frame is what gets measured.
			raw := strings.Split(m.viewListFrame(), "\n")
			if len(raw) != height {
				t.Errorf("%dx%d: frame paints %d rows", width, height, len(raw))
			}
			lines := strings.Split(m.View(), "\n")
			for i, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d: line %d is %d wide: %q", width, height, i, got, ansi.Strip(line))
				}
			}
		}
	}
}

// The pane's soft edges: the opening row is drawn in sextants and stops a
// third of a cell under the wordmark, the closing row bleeds up by half, and
// every body row ends with the ▐ half-block edge one column past the seam.
// The end cells of the edge rows are the corners: the first is a foreground
// block so the window margin cannot inherit pane tone and smear past the
// corner, and the last is narrowed to the bleed's half width so the fill
// closes at the corner instead of jutting right. The top row's corner is
// also a sextant, which is what keeps its edge level with the run beside it.
func TestPaneSoftEdges(t *testing.T) {
	m := shotModel()
	rows := strings.Split(m.View(), "\n")
	leftWidth, _ := m.splitWidths()

	top := []rune(ansi.Strip(rows[m.headerRows()]))
	bottom := []rune(ansi.Strip(rows[m.headerRows()+1+m.listBodyHeight()]))
	if top[0] != '🬹' || bottom[0] != '▀' {
		t.Fatalf("edge rows should open with foreground blocks, got %q and %q",
			string(top[0]), string(bottom[0]))
	}
	for col := 1; col <= leftWidth; col++ {
		if top[col] != '🬂' {
			t.Fatalf("top edge col %d is %q, want 🬂:\n%s", col, string(top[col]), string(top))
		}
		if bottom[col] != '▄' {
			t.Fatalf("bottom edge col %d is %q, want ▄:\n%s", col, string(bottom[col]), string(bottom))
		}
	}
	if top[leftWidth+1] != '🬨' || bottom[leftWidth+1] != '▟' {
		t.Fatalf("edge rows should close on the bleed's half width, got %q and %q",
			string(top[leftWidth+1]), string(bottom[leftWidth+1]))
	}
	if top[leftWidth+2] == '🬂' || bottom[leftWidth+2] == '▄' {
		t.Fatalf("the bleed should stop after the seam's edge column")
	}
	for i := m.headerRows() + 1; i < m.headerRows()+1+m.listBodyHeight(); i++ {
		row := []rune(ansi.Strip(rows[i]))
		if cell := row[0]; cell != '█' {
			t.Fatalf("row %d: first column is %q, want █:\n%s", i, string(cell), string(row))
		}
		if cell := row[leftWidth+1]; cell != '▐' && cell != '─' {
			t.Fatalf("row %d: edge column is %q, want ▐:\n%s", i, string(cell), string(row))
		}
		// The seam is fill, not a drawn line: the glyphs of the old line
		// seam appearing here mean a stale seam renderer shipped again.
		if cell := row[leftWidth]; cell != ' ' && cell != '─' {
			t.Fatalf("row %d: seam column is %q, want pane fill:\n%s", i, string(cell), string(row))
		}
	}
}
