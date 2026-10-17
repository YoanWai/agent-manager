package ui

import (
	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestComputerLinesTemperatures(t *testing.T) {
	cases := []struct {
		name string
		snap sysstat.Snapshot
		want string
	}{
		{
			name: "cpu and gpu",
			snap: sysstat.Snapshot{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55},
			want: "temp cpu 61°C gpu 55°C",
		},
		{
			name: "soc alone",
			snap: sysstat.Snapshot{SoCTempOK: true, SoCTemp: 60.4},
			want: "temp soc 60°C",
		},
		{
			name: "no sensors",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{layout: layoutState{width: 120, height: 34}, workspace: workspace{snap: tc.snap}}
			var temp string
			for _, line := range m.computerLines(40) {
				plain := strings.TrimSpace(ansi.Strip(line))
				if strings.HasPrefix(plain, "temp") {
					temp = strings.Join(strings.Fields(plain), " ")
				}
			}
			if tc.want == "" {
				if temp != "" {
					t.Fatalf("expected no temp row, got %q", temp)
				}
				return
			}
			if temp != tc.want {
				t.Fatalf("temp row = %q, want %q", temp, tc.want)
			}
		})
	}
}

func TestComputerLinesBattery(t *testing.T) {
	cases := []struct {
		name string
		snap sysstat.Snapshot
		want string
	}{
		{
			name: "discharging",
			snap: sysstat.Snapshot{BatteryOK: true, BatteryPercent: 84},
			want: "batt " + strings.Repeat("━", 10) + " 84%",
		},
		{
			name: "charging",
			snap: sysstat.Snapshot{BatteryOK: true, BatteryPercent: 50, BatteryCharging: true},
			want: "batt " + strings.Repeat("━", 10) + " 50% charging",
		},
		{
			name: "no battery",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &Model{layout: layoutState{width: 120, height: 34}, workspace: workspace{snap: tc.snap}}
			var batt string
			for _, line := range m.computerLines(40) {
				plain := strings.TrimSpace(ansi.Strip(line))
				if strings.HasPrefix(plain, "batt") {
					batt = strings.Join(strings.Fields(plain), " ")
				}
			}
			if tc.want == "" {
				if batt != "" {
					t.Fatalf("expected no battery row, got %q", batt)
				}
				return
			}
			if batt != tc.want {
				t.Fatalf("battery row = %q, want %q", batt, tc.want)
			}
		})
	}
}

// A battery gauge colors off how empty it is, the opposite of every other
// meter: low charge should read as alarming, a full battery as calm.
func TestGaugeInvertFlipsTheColorRamp(t *testing.T) {
	forceANSI256(t)

	lowCharge := sgrOf(gauge(5, 10, true))
	highUsage := sgrOf(gauge(95, 10, false))
	if lowCharge != highUsage {
		t.Fatalf("5%% inverted = %q, want the same alarm color as 95%% uninverted %q", lowCharge, highUsage)
	}

	highCharge := sgrOf(gauge(95, 10, true))
	lowUsage := sgrOf(gauge(5, 10, false))
	if highCharge != lowUsage {
		t.Fatalf("95%% inverted = %q, want the same calm color as 5%% uninverted %q", highCharge, lowUsage)
	}

	if lowCharge == highCharge {
		t.Fatal("low and high battery charge rendered the same color")
	}
}

// The separator carries its own reset, so a reading cannot inherit color.
func TestTemperatureReadingsEachKeepTheirColor(t *testing.T) {
	forceANSI256(t)

	row := tempReadings(sysstat.Snapshot{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55})
	want := sgrOf(valueStyle().Render("x"))
	for _, reading := range []string{"cpu 61°C", "gpu 55°C"} {
		before, _, found := strings.Cut(row, reading)
		if !found {
			t.Fatalf("row %q is missing %q", row, reading)
		}
		if got := lastSGR(before); got != want {
			t.Fatalf("%q renders under %q, want %q", reading, got, want)
		}
	}
}

func lastSGR(s string) string {
	idx := strings.LastIndex(s, "\x1b[")
	if idx < 0 {
		return ""
	}
	code, _, _ := strings.Cut(s[idx+2:], "m")
	return code
}

func TestComputerDiskAvailable(t *testing.T) {
	m := &Model{workspace: workspace{snap: sysstat.Snapshot{
		DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000,
	}}}
	for _, width := range []int{29, 34, 40, 55} {
		lines := m.computerLines(width)
		plain := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(plain, "66%") || !strings.Contains(plain, "167.9GB available") {
			t.Fatalf("disk reading = %q", plain)
		}
		for _, line := range lines {
			if got := ansi.StringWidth(line); got > width {
				t.Fatalf("disk line width %d exceeds %d: %q", got, width, plain)
			}
		}
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
		if got := diskBytes(tc.bytes); got != tc.want {
			t.Errorf("diskBytes(%d) = %q, want %q", tc.bytes, got, tc.want)
		}
	}
}

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
	m.update.latest = "v0.9.9"
	m.update.url = "https://example.com"
	for _, width := range []int{40, 55, 70} {
		for _, line := range m.railFootLines(width) {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line overflows at %d: %q", width, got, ansi.Strip(line))
			}
		}
	}
}

// Full screen condenses the machine readings to one line.
func TestFullLayoutFootLine(t *testing.T) {
	m := shotModel()
	m.prefs.fullLayout = true
	foot := ansi.Strip(strings.Join(m.railFootLines(m.layout.width-1), "\n"))
	for _, want := range []string{"cpu 22%", "mem 75%", "net"} {
		if !strings.Contains(foot, want) {
			t.Errorf("condensed foot line misses %q:\n%s", want, foot)
		}
	}
	if strings.Contains(foot, "messages") {
		t.Fatalf("no notices should mean no messages count:\n%s", foot)
	}
	if lines := m.railFootLines(m.layout.width - 1); len(lines) != 1 {
		t.Fatalf("full screen foot should be one line, got %d", len(lines))
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
