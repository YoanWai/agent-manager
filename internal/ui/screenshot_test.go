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
		m.version = "0.9.2"
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
		splitRatio: defaultSplitRatio,
		agents:     agentStats{count: 4, cpu: 37, rss: 1_530_000_000},
		netRates:   true, netDown: 9_400_000, netUp: 2_100_000,
		snap: sysstat.Snapshot{
			CPUOK: true, CPUPercent: 22,
			MemOK: true, MemPercent: 75, MemUsed: 12_100_000_000, MemTotal: 16_000_000_000,
			SwapOK: true, SwapPercent: 43, SwapUsed: 4_500_000_000, SwapTotal: 8_000_000_000,
			DiskOK: true, DiskPercent: 88, DiskUsed: 400_000_000_000, DiskTotal: 500_000_000_000,
			CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55,
		},
		preview: previewSample,
		proc:    sysstat.ProcStat{OK: true, CPUPercent: 4.2, RSS: 612_000_000},
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
			lines := strings.Split(m.View(), "\n")
			if len(lines) != height {
				t.Errorf("%dx%d: frame has %d rows", width, height, len(lines))
			}
			for i, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("%dx%d: line %d is %d wide: %q", width, height, i, got, ansi.Strip(line))
				}
			}
		}
	}
}
