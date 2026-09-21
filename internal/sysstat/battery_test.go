package sysstat

import (
	"errors"
	"testing"

	"github.com/distatus/battery"
)

func withBatterySource(t *testing.T, batteries []*battery.Battery, err error) {
	t.Helper()
	prev := batterySource
	batterySource = func() ([]*battery.Battery, error) { return batteries, err }
	t.Cleanup(func() { batterySource = prev })
}

func TestSampleBatteryDischarging(t *testing.T) {
	withBatterySource(t, []*battery.Battery{
		{Current: 42, Full: 50, State: battery.State{Raw: battery.Discharging}},
	}, nil)

	var snap Snapshot
	sampleBattery(&snap)

	if !snap.BatteryOK {
		t.Fatal("expected BatteryOK")
	}
	if snap.BatteryCharging {
		t.Fatal("expected not charging")
	}
	if want := 84.0; snap.BatteryPercent != want {
		t.Fatalf("percent = %v, want %v", snap.BatteryPercent, want)
	}
}

func TestSampleBatteryCharging(t *testing.T) {
	withBatterySource(t, []*battery.Battery{
		{Current: 25, Full: 50, State: battery.State{Raw: battery.Charging}},
	}, nil)

	var snap Snapshot
	sampleBattery(&snap)

	if !snap.BatteryOK || !snap.BatteryCharging {
		t.Fatal("expected BatteryOK and charging")
	}
	if want := 50.0; snap.BatteryPercent != want {
		t.Fatalf("percent = %v, want %v", snap.BatteryPercent, want)
	}
}

// TestSampleBatteryMultiple sums current and full across batteries rather
// than averaging each one's own percent, so a depleted second battery pulls
// the reported charge down instead of being diluted by a healthy one.
func TestSampleBatteryMultiple(t *testing.T) {
	withBatterySource(t, []*battery.Battery{
		{Current: 50, Full: 50, State: battery.State{Raw: battery.Full}},
		{Current: 0, Full: 50, State: battery.State{Raw: battery.Empty}},
	}, nil)

	var snap Snapshot
	sampleBattery(&snap)

	if !snap.BatteryOK {
		t.Fatal("expected BatteryOK")
	}
	if want := 50.0; snap.BatteryPercent != want {
		t.Fatalf("percent = %v, want %v", snap.BatteryPercent, want)
	}
}

func TestSampleBatteryNotFound(t *testing.T) {
	withBatterySource(t, nil, battery.ErrFatal{Err: battery.ErrNotFound})

	var snap Snapshot
	sampleBattery(&snap)

	if snap.BatteryOK {
		t.Fatal("expected BatteryOK false when no battery is present")
	}
}

func TestSampleBatteryEmptySlice(t *testing.T) {
	withBatterySource(t, []*battery.Battery{}, nil)

	var snap Snapshot
	sampleBattery(&snap)

	if snap.BatteryOK {
		t.Fatal("expected BatteryOK false for an empty battery list")
	}
}

func TestSampleBatteryReadError(t *testing.T) {
	withBatterySource(t, nil, errors.New("boom"))

	var snap Snapshot
	sampleBattery(&snap)

	if snap.BatteryOK {
		t.Fatal("expected BatteryOK false on a read error")
	}
}

func TestSampleBatteryPartialResultWithError(t *testing.T) {
	withBatterySource(t, []*battery.Battery{
		{Current: 30, Full: 40, State: battery.State{Raw: battery.Discharging}},
	}, errors.New("boom"))

	var snap Snapshot
	sampleBattery(&snap)

	if snap.BatteryOK {
		t.Fatal("expected BatteryOK false when the source returns an error alongside entries")
	}
}

func TestSampleBatterySkipsUnreadableEntries(t *testing.T) {
	withBatterySource(t, []*battery.Battery{
		nil,
		{Current: 0, Full: 0, State: battery.State{Raw: battery.Unknown}},
		{Current: 30, Full: 40, State: battery.State{Raw: battery.Discharging}},
	}, nil)

	var snap Snapshot
	sampleBattery(&snap)

	if !snap.BatteryOK {
		t.Fatal("expected BatteryOK from the one readable entry")
	}
	if want := 75.0; snap.BatteryPercent != want {
		t.Fatalf("percent = %v, want %v", snap.BatteryPercent, want)
	}
}
