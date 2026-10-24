package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestViewPinsTheFrameToTheTerminalHeight(t *testing.T) {
	tall := "a\nb\nc\nd\ne"
	got := uiframe.Clamp(tall, 3, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) })
	if got != "a\nb\nc" {
		t.Fatalf("clamp trim = %q", got)
	}
	short := "x"
	got = uiframe.Clamp(short, 3, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) })
	lines := strings.Split(got, "\n")
	if len(lines) != 3 || lines[0] != "x" {
		t.Fatalf("clamp pad = %q", got)
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
		applyTheme(uitheme.Palettes[uitheme.Index(name)])
		t.Cleanup(func() { applyTheme(uitheme.Palettes[0]) })
	}
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := shotModel()
	switch os.Getenv("AM_SHOT_MODE") {
	case "settings":
		m.push(newSettingsSurface(m, uisettings.State{
			ToolNames: []string{"claude", "codex", "grok", "opencode"},
			ToolIndex: 0, ThemeIndex: 1, Field: uisettings.FieldTheme, LayoutSplit: true,
		}))
		m.release.Version = "0.9.2"
	case "help":
		m.openHelp()
	}
	if phase := os.Getenv("AM_SHOT_PHASE"); phase != "" {
		n, err := strconv.Atoi(phase)
		if err != nil {
			t.Fatal(err)
		}
		m.startup.bannerPhase = n
	}
	if err := os.WriteFile(out, []byte(preparedView(m)), 0o644); err != nil {
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
			m.layout.width, m.layout.height = width, height
			// View clamps and pads, which would hide a frame that is a row
			// short or a row long, so the raw frame is what gets measured.
			raw := strings.Split(m.viewListFrame(), "\n")
			if len(raw) != height {
				t.Errorf("%dx%d: frame paints %d rows", width, height, len(raw))
			}
			lines := strings.Split(preparedView(m), "\n")
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
	rows := strings.Split(preparedView(m), "\n")
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

func TestViewDoesNotPublishCursorState(t *testing.T) {
	m := &Model{}
	m.focus.imeCursor = &uiframe.CursorAnchor{}
	m.focus.imeCursor.Set(8, 4, true)
	if got := m.View(); got != "loading..." {
		t.Fatalf("initial view = %q", got)
	}
	col, row, ok := m.focus.imeCursor.Get()
	if col != 8 || row != 4 || !ok {
		t.Fatalf("View changed published cursor to (%d, %d, %t)", col, row, ok)
	}
}

func TestUpdatePublishesPreparedFrame(t *testing.T) {
	m := shotModel()
	m.prepareFrame()
	before := m.View()
	m.openHelp()
	if got := m.View(); got != before {
		t.Fatal("View rendered state that had not been prepared")
	}
	updated, _ := m.Update(struct{}{})
	if updated != m {
		t.Fatal("Update replaced the root model")
	}
	if got := m.View(); got == before || got == "loading..." {
		t.Fatal("Update did not publish the changed frame")
	}
}

func preparedView(m *Model) string {
	m.prepareFrame()
	return m.View()
}

func TestHeaderShowsUpdateBadgeBesideWordmark(t *testing.T) {
	m := &Model{
		layout:  layoutState{width: 120},
		release: uirelease.State{Latest: "v0.9.0"},
	}
	header := ansi.Strip(m.viewHeaderRows()[0])
	if !strings.Contains(header, "v0.9.0") || !strings.Contains(header, "available") {
		t.Errorf("header missing update badge: %q", header)
	}
	if strings.Index(header, "available") > strings.Index(header, "agents") {
		t.Errorf("badge should sit left, by the wordmark: %q", header)
	}
	m.release.Latest = ""
	if header := ansi.Strip(m.viewHeaderRows()[0]); strings.Contains(header, "available") {
		t.Errorf("header should have no badge when up to date: %q", header)
	}
}

func TestViewLegendPaintsWaitingMessagesWarm(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	waiting := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		release:  uirelease.State{Latest: "v0.41.0", Version: "0.40.0"},
	}
	bar := paintLegend([]legendSection{waiting.viewLegend()}, 160).text
	if !strings.Contains(bar, uitheme.LegendAlertStyle().Render("M unread")) {
		t.Fatalf("waiting messages should use the working color:\n%s", bar)
	}
	idle := &Model{
		services: services{store: st, keys: keybind.DefaultSession(), listKeys: keybind.DefaultList()},
		release:  uirelease.State{Version: "0.40.0"},
		notices:  noticesPanel{Panel: uinotices.NewPanel(map[string]bool{uinotices.Welcome: true, uinotices.ArrowStep: true})},
	}
	idleBar := ansi.Strip(paintLegend([]legendSection{idle.viewLegend()}, 160).text)
	if strings.Contains(idleBar, "unread") {
		t.Fatalf("an empty panel should leave the messages key off:\n%s", idleBar)
	}
}

// The root's tier literals reach the painter: every field the row and view
// legends set survives the copy into ui/frame.
func TestRootLegendKeepsItsTiersAndHits(t *testing.T) {
	sections := []legendSection{{
		title:  "Session",
		leads:  [][2]string{{"p", "quick prompt mode"}},
		alerts: [][2]string{{"M", "unread"}},
		hint:   "a hint",
		pairs:  [][2]string{{"r", "rename"}},
	}}
	painted := paintLegend(sections, 200)
	plain := ansi.Strip(painted.text)
	for _, want := range []string{"Session", "p quick prompt mode", "M unread", "a hint", "r rename"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("root legend lost %q:\n%s", want, plain)
		}
	}
	if len(painted.hits) != 3 {
		t.Fatalf("hits = %+v, want one per pressable pair", painted.hits)
	}
	lines := strings.Split(plain, "\n")
	for _, hit := range painted.hits {
		if hit.y < 0 || hit.y >= len(lines) {
			t.Fatalf("hit %+v lands on no painted row", hit)
		}
	}
}
