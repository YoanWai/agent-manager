package ui

import (
	"github.com/YoanWai/agent-manager/internal/sysstat"
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
			m := &Model{width: 120, height: 34, workspace: workspace{snap: tc.snap}}
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
			m := &Model{width: 120, height: 34, workspace: workspace{snap: tc.snap}}
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
	want := sgrOf(valueStyle.Render("x"))
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
