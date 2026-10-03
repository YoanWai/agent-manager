//go:build !windows

package sysstat

import (
	"slices"
	"testing"
)

func TestParsePSTime(t *testing.T) {
	cases := map[string]float64{
		"0:00.50":    0.5,
		"1:30.00":    90,
		"37:06.59":   37*60 + 6.59,
		"975:30.99":  975*60 + 30.99,
		"01:02:03":   1*3600 + 2*60 + 3,
		"2-01:00:00": 2*86400 + 3600,
	}
	for in, want := range cases {
		got, err := parsePSTime(in)
		if err != nil {
			t.Fatalf("parsePSTime(%q): %v", in, err)
		}
		if got < want-0.01 || got > want+0.01 {
			t.Fatalf("parsePSTime(%q) = %v, want %v", in, got, want)
		}
	}
}

// A child that exits between the two ps passes frees its pid, and a pid the
// kernel hands to something unrelated must not be read as this pane's agent.
func TestChildNamesRequireTheSampledParent(t *testing.T) {
	stats := map[int]ProcStat{100: {OK: true}}
	children := map[int][]int{100: {101, 102}}
	applyChildNames(stats, children, "  101   100 /opt/homebrew/bin/codex --resume 7\n  102   999 /usr/bin/vim notes.txt\n")
	want := []string{"/opt/homebrew/bin/codex"}
	if got := stats[100].Children; !slices.Equal(got, want) {
		t.Fatalf("children = %v, want %v", got, want)
	}
}
