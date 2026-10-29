package workspace

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func forceANSI256(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// sgrOf returns the color sequence a style emits under the live theme, so a
// contrast assertion tracks the palette instead of a hardcoded index.
func sgrOf(rendered string) string {
	_, seq, found := strings.Cut(rendered, "\x1b[")
	if !found {
		return ""
	}
	code, _, _ := strings.Cut(seq, "m")
	return code
}

func lastSGR(s string) string {
	idx := strings.LastIndex(s, "\x1b[")
	if idx < 0 {
		return ""
	}
	code, _, _ := strings.Cut(s[idx+2:], "m")
	return code
}

func TestComputerLinesTemperatures(t *testing.T) {
	cases := []struct {
		name string
		r    Readings
		want string
	}{
		{
			name: "cpu and gpu",
			r:    Readings{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55},
			want: "temp cpu 61°C gpu 55°C",
		},
		{
			name: "soc alone",
			r:    Readings{SoCTempOK: true, SoCTemp: 60.4},
			want: "temp soc 60°C",
		},
		{
			name: "no sensors",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var temp string
			for _, line := range ComputerLines(tc.r, Net{}, 40, presentation.GlyphModeAdvanced) {
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
		r    Readings
		want string
	}{
		{
			name: "discharging",
			r:    Readings{BatteryOK: true, BatteryPercent: 84},
			want: "batt " + strings.Repeat("━", 10) + " 84%",
		},
		{
			name: "charging",
			r:    Readings{BatteryOK: true, BatteryPercent: 50, BatteryCharging: true},
			want: "batt " + strings.Repeat("━", 10) + " 50% charging",
		},
		{
			name: "no battery",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var batt string
			for _, line := range ComputerLines(tc.r, Net{}, 40, presentation.GlyphModeAdvanced) {
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

// The separator carries its own reset, so a reading cannot inherit color.
func TestTemperatureReadingsEachKeepTheirColor(t *testing.T) {
	forceANSI256(t)

	row := tempReadings(Readings{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55})
	want := sgrOf(uitheme.ValueStyle().Render("x"))
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

func TestComputerDiskAvailable(t *testing.T) {
	r := Readings{DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000}
	for _, width := range []int{29, 34, 40, 55} {
		lines := ComputerLines(r, Net{}, width, presentation.GlyphModeAdvanced)
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

// Full screen condenses the machine readings to one line.
func TestFootLineCondensesEveryReading(t *testing.T) {
	net := Net{Rates: true, Down: 1000, Up: 500}
	line := ansi.Strip(strings.Join(FootLine(Readings{
		CPUPercent: 22, CPUOK: true,
		MemPercent: 75, MemOK: true,
		DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000,
	}, net, 119), "\n"))
	for _, want := range []string{"cpu 22%", "mem 75%", "disk 66% 167.9GB available", "net"} {
		if !strings.Contains(line, want) {
			t.Errorf("condensed foot line misses %q:\n%s", want, line)
		}
	}
}

func TestFootLineFitsItsWidth(t *testing.T) {
	line := FootLine(Readings{
		CPUPercent: 22, CPUOK: true, MemPercent: 75, MemOK: true,
		DiskOK: true, DiskPercent: 66, DiskAvailable: 167_950_000_000,
	}, Net{Rates: true, Down: 1_000_000, Up: 500_000}, 40)
	if got := ansi.StringWidth(line[0]); got > 39 {
		t.Fatalf("foot line %d cells wide: %q", got, ansi.Strip(line[0]))
	}
}
