package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sysstat"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

// A battery gauge colors off how empty it is, the opposite of every other
// meter: low charge should read as alarming, a full battery as calm.
func TestGaugeInvertFlipsTheColorRamp(t *testing.T) {
	forceANSI256(t)

	lowCharge := sgrOf(uitheme.Gauge(5, 10, true))
	highUsage := sgrOf(uitheme.Gauge(95, 10, false))
	if lowCharge != highUsage {
		t.Fatalf("5%% inverted = %q, want the same alarm color as 95%% uninverted %q", lowCharge, highUsage)
	}

	highCharge := sgrOf(uitheme.Gauge(95, 10, true))
	lowUsage := sgrOf(uitheme.Gauge(5, 10, false))
	if highCharge != lowUsage {
		t.Fatalf("95%% inverted = %q, want the same calm color as 5%% uninverted %q", highCharge, lowUsage)
	}

	if lowCharge == highCharge {
		t.Fatal("low and high battery charge rendered the same color")
	}
}

func TestDiskBytes(t *testing.T) {
	for _, tc := range []struct {
		bytes uint64
		want  string
	}{
		{0, "0B"},
		{999, "999B"},
		{1000, "1.0KB"},
		{1_000_000, "1.0MB"},
		{169_850_000_000, "169.8GB"},
		{1_000_000_000_000, "1.0TB"},
	} {
		if got := uiframe.DiskBytes(tc.bytes); got != tc.want {
			t.Errorf("uiframe.DiskBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

// The foot is the machine readings alone: a notice floats over the frame
// rather than taking a row here.
func TestRailFootOnlyShowsComputerStats(t *testing.T) {
	for _, full := range []bool{false, true} {
		for _, width := range []int{34, 70, 120} {
			m := footModel(t)
			m.prefs.fullLayout = full
			foot := strings.Join(m.railFootLines(width), "\n")
			if !strings.Contains(ansi.Strip(foot), "cpu") || strings.Contains(ansi.Strip(foot), "messages") {
				t.Fatalf("full=%v width=%d: stats foot contains messages or loses readings:\n%s", full, width, ansi.Strip(foot))
			}
			for _, n := range m.activeNotices() {
				m.dismissNotice(n.ID)
			}
			if got := strings.Join(m.railFootLines(width), "\n"); got != foot {
				t.Fatalf("full=%v width=%d: notices changed the stats foot", full, width)
			}
		}
	}
}

func TestRailFootLinesFitWidth(t *testing.T) {
	m := footModel(t)
	m.release.Latest = "v0.9.9"
	m.release.URL = "https://example.com"
	for _, width := range []int{40, 55, 70} {
		for _, line := range m.railFootLines(width) {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("width %d: line overflows at %d: %q", width, got, ansi.Strip(line))
			}
		}
	}
}

func TestLayoutsCanHideStats(t *testing.T) {
	for _, tc := range []struct {
		name  string
		full  bool
		width int
	}{
		{name: "split", width: 36},
		{name: "full", full: true, width: 119},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := footModel(t)
			m.layout.width, m.layout.height = 120, 34
			m.prefs.fullLayout = tc.full
			m.prefs.hideStats = true
			if foot := m.railFootLines(tc.width); len(foot) > 0 {
				t.Fatalf("hidden stats still paint %d lines:\n%s", len(foot), ansi.Strip(strings.Join(foot, "\n")))
			}
			rail := m.railLines(tc.width, m.listBodyHeight())
			if len(rail) != m.listBodyHeight() {
				t.Fatalf("footerless rail = %d rows, body is %d", len(rail), m.listBodyHeight())
			}
			for _, line := range rail {
				if line.rule {
					t.Fatal("hidden stats left their separator behind")
				}
			}
		})
	}
}

func TestFullFootDiskAvailable(t *testing.T) {
	m := &Model{workspace: workspace{snap: sysstat.Snapshot{
		DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000,
	}}}
	line := ansi.Strip(strings.Join(m.fullFootLine(200), "\n"))
	if !strings.Contains(line, "disk 66% 167.9GB available") {
		t.Fatalf("disk reading = %q", line)
	}
}
