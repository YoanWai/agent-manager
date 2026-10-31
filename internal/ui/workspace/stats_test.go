package workspace

import (
	"testing"
	"time"
)

// A poll diff is a rate against the gap since the last one, so a pass that
// arrives late reports a slower rate than one that arrives promptly.
func TestUpdateNetRatesDiffsCounters(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var net Net
	net.Update(1_000, 2_000, true, start)
	net.Update(3_000, 6_000, true, start.Add(2*time.Second))

	if !net.Rates || net.Up != 1_000 || net.Down != 2_000 {
		t.Fatalf("rates = %v up = %d down = %d, want 1000 up and 2000 down", net.Rates, net.Up, net.Down)
	}
}

// Counters can reset under a sleep or a new interface, and a backwards jump
// reseeds the baseline instead of reporting a nonsense rate.
func TestUpdateNetRatesReseedsOnABackwardsJump(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var net Net
	net.Update(5_000, 5_000, true, start)
	net.Update(1_000, 1_000, true, start.Add(time.Second))

	if net.Rates {
		t.Fatal("a backwards jump reported rates")
	}
	net.Update(3_000, 3_000, true, start.Add(2*time.Second))
	if net.Up != 2_000 || net.Down != 2_000 {
		t.Fatalf("reseeded baseline gave up = %d down = %d, want 2000 each", net.Up, net.Down)
	}
}

// An interface probe that fails leaves the last rate on screen rather than
// reporting a drop to zero.
func TestUpdateNetRatesKeepsTheLastReadingWhenTheProbeFails(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	var net Net
	net.Update(1_000, 1_000, true, start)
	net.Update(2_000, 2_000, true, start.Add(time.Second))
	net.Update(3_000, 3_000, false, start.Add(2*time.Second))

	if net.Up != 1_000 || net.Down != 1_000 {
		t.Fatalf("up = %d down = %d, want the last good 1000", net.Up, net.Down)
	}
}

func TestTemperaturesReadsSensorsInDisplayOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readings Readings
		want     string
	}{
		{
			name:     "cpu and gpu",
			readings: Readings{CPUTempOK: true, CPUTemp: 61, GPUTempOK: true, GPUTemp: 55},
			want:     "cpu 61°C gpu 55°C",
		},
		{
			name:     "soc alone",
			readings: Readings{SoCTempOK: true, SoCTemp: 60.4},
			want:     "soc 60°C",
		},
		{name: "no sensors"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Temperatures(tc.readings)
			joined := ""
			for i, part := range got {
				if i > 0 {
					joined += " "
				}
				joined += part
			}
			if joined != tc.want {
				t.Fatalf("temperatures = %q, want %q", joined, tc.want)
			}
		})
	}
}

// The meter column stays readable on a narrow rail and stops growing once
// the label and the reading need the room.
func TestMeterWidthBounds(t *testing.T) {
	if got := MeterWidth(20); got != 4 {
		t.Fatalf("narrow meter width = %d, want the floor of 4", got)
	}
	if got := MeterWidth(120); got != 10 {
		t.Fatalf("wide meter width = %d, want the ceiling of 10", got)
	}
}
