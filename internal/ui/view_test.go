package ui

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func TestScrollWindow(t *testing.T) {
	cases := []struct {
		name                  string
		total, cursor, height int
		wantStart, wantEnd    int
	}{
		{"fits entirely", 5, 2, 10, 0, 5},
		{"cursor at top", 100, 0, 20, 0, 18},
		{"cursor centered", 100, 50, 20, 41, 59},
		{"cursor at bottom", 100, 99, 20, 82, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, end := scrollWindow(tc.total, tc.cursor, tc.height)
			if start != tc.wantStart || end != tc.wantEnd {
				t.Fatalf("scrollWindow(%d,%d,%d) = %d,%d want %d,%d",
					tc.total, tc.cursor, tc.height, start, end, tc.wantStart, tc.wantEnd)
			}
			if tc.cursor < start || tc.cursor >= end {
				t.Fatalf("cursor %d outside window [%d,%d)", tc.cursor, start, end)
			}
		})
	}
}

func TestPaneExactPreservesBlanks(t *testing.T) {
	pane := "one\n\n\ntwo\n"
	got := paneExact(pane, 10)
	if len(got) != 4 || got[1] != "" || got[2] != "" {
		t.Fatalf("paneExact should keep blank rows: %q", got)
	}
	got = paneExact("a\nb\nc\nd", 2)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Fatalf("paneExact oversize should take bottom lines: %q", got)
	}
}

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

func stripPreviewMarks(s string) string {
	return strings.NewReplacer("\u200e", "", "\u2066", "", "\u2069", "").Replace(s)
}

func TestPreviewLine(t *testing.T) {
	colored := "\x1b[38;5;42mgreen text\x1b[39m"
	got := previewLine(colored, 80)
	if !strings.Contains(got, "\x1b[38;5;42m") {
		t.Fatalf("color escapes should survive: %q", got)
	}
	if !strings.Contains(got, "\x1b[0m") || !strings.HasSuffix(got, "\u2069") {
		t.Fatalf("line with ANSI must reset SGR and close the isolate: %q", got)
	}

	erased := "abc\x1b[K\x1b[2Jdef"
	if got := stripPreviewMarks(ansi.Strip(previewLine(erased, 80))); strings.TrimRight(got, " ") != "abcdef" {
		t.Fatalf("erase sequences should be stripped: %q", got)
	}
	scrolled := "a\x1b[1Sb\x1bMc\x1b[2Td"
	if got := stripPreviewMarks(ansi.Strip(previewLine(scrolled, 80))); strings.TrimRight(got, " ") != "abcd" {
		t.Fatalf("scroll sequences should be stripped: %q", got)
	}

	control := "a\rb\bc"
	if got := stripPreviewMarks(ansi.Strip(previewLine(control, 80))); strings.TrimRight(got, " ") != "abc" {
		t.Fatalf("control chars should be dropped: %q", got)
	}

	wide := "\x1b[31m" + strings.Repeat("x", 100) + "\x1b[0m"
	clipped := previewLine(wide, 20)
	if w := lipglossWidth(clipped); w > 20 {
		t.Fatalf("clipped ANSI line renders %d cells, want <= 20", w)
	}

	plain := previewLine("plain", 80)
	if strings.Contains(plain, "\x1b") {
		t.Fatalf("plain line should gain no escapes: %q", plain)
	}
	if !strings.HasPrefix(plain, "\u200e\u2066") || !strings.HasSuffix(plain, "\u2069") {
		t.Fatalf("pane row should open LTR and stay isolated: %q", plain)
	}
	if w := ansi.StringWidth(plain); w != 80 {
		t.Fatalf("pane row should fill its column, width %d", w)
	}
	mixed := stripPreviewMarks(ansi.Strip(previewLine("קודינג\",\"slowly\":false", 80)))
	if strings.TrimRight(mixed, " ") != "קודינג\",\"slowly\":false" {
		t.Fatalf("mixed-direction pane row lost its text: %q", mixed)
	}
	hebrew := previewLine("  העצמון 25 חולון", 40)
	if !strings.HasPrefix(hebrew, "\u200e\u2066") {
		t.Fatalf("pure RTL row needs a leading LRM so the host keeps LTR: %q", hebrew)
	}
	if w := ansi.StringWidth(hebrew); w != 40 {
		t.Fatalf("pure RTL row must pad inside the isolate to column width, got %d", w)
	}
	if body := strings.TrimRight(stripPreviewMarks(ansi.Strip(hebrew)), " "); body != "  העצמון 25 חולון" {
		t.Fatalf("pure RTL row text changed: %q", body)
	}
}

func TestTruncateRuneSafe(t *testing.T) {
	hebrew := "/home/dev/פרויקטים/agent-manager"
	tail := truncateTail(hebrew, 10)
	if !strings.HasPrefix(tail, "…") || len([]rune(tail)) != 10 {
		t.Fatalf("truncateTail broken: %q (%d runes)", tail, len([]rune(tail)))
	}
	if truncateTail("short", 10) != "short" {
		t.Fatal("short strings should pass through")
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
		m.bannerPhase = n
	}
	if err := os.WriteFile(out, []byte(m.View()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func shotModel() *Model {
	now := time.Now()
	sess := func(name, group, tool, st string, age time.Duration) store.Session {
		return store.Session{
			ID: name, Name: name, Group: group, Tool: tool, Status: st,
			Cwd: "/Users/yoan/dev/spaze/api", CreatedAt: now.Add(-24 * time.Hour),
			LastStatusAt: now.Add(-age),
		}
	}
	sessions := []store.Session{
		sess("db-migrations", "", "opencode", status.Waiting, 3*time.Minute),
		sess("notes", "", "grok", status.Idle, 12*time.Minute),
		sess("auth-refresh", "backend", "claude", status.Finished, 2*time.Minute),
		sess("add-rate-limiting", "backend", "claude", status.Working, 41*time.Second),
		sess("ui-polish", "backend/web", "codex", status.Working, 6*time.Minute),
		sess("flaky-e2e", "backend/web", "claude", status.Errored, 22*time.Minute),
	}
	rows := []treeRow{
		{sess: sessions[0]},
		{sess: sessions[1]},
		{isGroup: true, group: "backend"},
		{depth: 1, sess: sessions[2]},
		{depth: 1, sess: sessions[3]},
		{isGroup: true, group: "backend/web", depth: 1},
		{depth: 2, sess: sessions[4]},
		{depth: 2, sess: sessions[5]},
	}
	m := &Model{
		width: 120, height: 34, mode: modeList, cursor: 4,
		sessions: sessions, rows: rows, collapsed: map[string]bool{},
		groupPaths: map[string]string{"backend": "/Users/yoan/dev/spaze/api"},
		split:      splitState{ratio: defaultSplitRatio},
		agents:     agentStats{count: 4, cpu: 12, ram: 9, rss: 1_530_000_000},
		net:        netStats{rates: true, down: 9_400_000, up: 2_100_000},
		snap: sysstat.Snapshot{
			CPUOK: true, CPUPercent: 22,
			MemOK: true, MemPercent: 75, MemUsed: 12_100_000_000, MemTotal: 16_000_000_000,
			SwapOK: true, SwapPercent: 43, SwapUsed: 4_500_000_000, SwapTotal: 8_000_000_000,
			DiskOK: true, DiskPercent: 88, DiskUsed: 400_000_000_000, DiskFree: 100_000_000_000, DiskTotal: 500_000_000_000,
			CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55,
		},
		preview: previewSample,
		proc:    sysstat.ProcStat{OK: true, CPUPercent: 4.2, RamPercent: 3.6, RSS: 612_000_000},
		procFor: "add-rate-limiting",
	}
	return m
}

const previewSample = "\x1b[38;5;110m◆\x1b[0m claude \x1b[38;5;240m·\x1b[0m add-rate-limiting\n" +
	"\n" +
	"\x1b[38;5;250m❯ Add a token bucket limiter to the public API\x1b[0m\n" +
	"\n" +
	"\x1b[38;5;240m●\x1b[0m Read(internal/api/router.go)\n" +
	"  \x1b[38;5;240m└\x1b[0m 214 lines\n" +
	"\n" +
	"\x1b[38;5;240m●\x1b[0m Edit(internal/api/limiter.go)\n" +
	"  \x1b[38;5;240m└\x1b[0m +48 −3\n" +
	"\n" +
	"\x1b[38;5;214m✳\x1b[0m Running tests… (14s · esc to interrupt)\n"

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

func TestHeaderShowsUpdateBadgeBesideWordmark(t *testing.T) {
	m := &Model{width: 120, update: updateInfo{latest: "v0.9.0"}}
	header := ansi.Strip(m.viewHeaderRows()[0])
	if !strings.Contains(header, "v0.9.0") || !strings.Contains(header, "available") {
		t.Errorf("header missing update badge: %q", header)
	}
	if strings.Index(header, "available") > strings.Index(header, "sessions") {
		t.Errorf("badge should sit left, by the wordmark: %q", header)
	}
	m.update.latest = ""
	if header := ansi.Strip(m.viewHeaderRows()[0]); strings.Contains(header, "available") {
		t.Errorf("header should have no badge when up to date: %q", header)
	}
}

func TestUpdateMsgSetsAndClearsBadge(t *testing.T) {
	m := &Model{width: 120}
	m.Update(updateMsg{latest: "v0.11.1", url: "https://example/rel"})
	if m.update.latest != "v0.11.1" || m.update.url != "https://example/rel" {
		t.Errorf("badge not set: %q %q", m.update.latest, m.update.url)
	}
	m.Update(updateMsg{})
	if m.update.latest != "" || m.update.url != "" {
		t.Errorf("an up-to-date result should clear the badge: %q %q", m.update.latest, m.update.url)
	}
}

func TestFailedUpdateCheckKeepsBadge(t *testing.T) {
	m := &Model{width: 120, update: updateInfo{latest: "v0.11.1", url: "https://example/rel"}}
	m.Update(updateMsg{failed: true})
	if m.update.latest != "v0.11.1" || m.update.url != "https://example/rel" {
		t.Errorf("a failed check must leave the badge alone: %q %q", m.update.latest, m.update.url)
	}
}

func TestUpdateTickReArms(t *testing.T) {
	m := &Model{width: 120}
	if _, cmd := m.Update(updateTickMsg{}); cmd == nil {
		t.Error("update tick should re-arm the timer and re-check")
	}
}
