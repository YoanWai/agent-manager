package sysstat

import "github.com/distatus/battery"

// batterySource is swappable in tests for a fake battery source.
var batterySource = battery.GetAll

// sampleBattery reads charge percent and charging state. A read error
// (no battery present, or a platform read failure) leaves BatteryOK false
// so the caller hides the line rather than showing a placeholder or zero.
func sampleBattery(snap *Snapshot) {
	batteries, err := batterySource()
	if err != nil {
		return
	}
	applyBatteries(snap, batteries)
}

// applyBatteries sums current and full capacity across every battery
// before dividing, rather than averaging each battery's own percent: a
// depleted second battery then pulls the reported charge down instead of
// being diluted by a healthy one.
func applyBatteries(snap *Snapshot, batteries []*battery.Battery) {
	var full, current float64
	var charging bool
	for _, b := range batteries {
		if b == nil || b.Full <= 0 {
			continue
		}
		full += b.Full
		current += b.Current
		if b.State.Raw == battery.Charging {
			charging = true
		}
	}
	if full <= 0 {
		return
	}
	snap.BatteryPercent = clampPct(current / full * 100)
	snap.BatteryCharging = charging
	snap.BatteryOK = true
}
