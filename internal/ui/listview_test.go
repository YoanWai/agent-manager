package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/x/ansi"
)

// The computer block reports whichever temperatures the machine exposes:
// a cpu/gpu pair where the hardware draws that line, a single soc figure
// on a chip that does not, and no temp row at all where nothing is read.
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
			m := &Model{width: 120, height: 34, snap: tc.snap}
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
