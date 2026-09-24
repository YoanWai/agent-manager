package sysstat

import "github.com/distatus/battery"

// sampleBattery reads charge percent and charging state. A read error
// (no battery present, or a platform read failure) leaves BatteryOK false
// so the caller hides the line rather than showing a placeholder or zero.
func sampleBattery(snap *Snapshot) {
	batteries, err := batterySource()
	var perBattery battery.Errors
	if err != nil {
		readErrors, ok := err.(battery.Errors)
		if !ok {
			return
		}
		perBattery = readErrors
	}
	var readable []*battery.Battery
	for index, entry := range batteries {
		if index < len(perBattery) && !chargeReadable(perBattery[index]) {
			continue
		}
		readable = append(readable, entry)
	}
	applyBatteries(snap, readable)
}

// chargeReadable keeps a battery whose only gaps are fields the gauge does
// not use: a wireless mouse on Linux reports capacity but no energy files,
// and must not hide the laptop's own battery.
func chargeReadable(err error) bool {
	if err == nil {
		return true
	}
	partial, ok := err.(battery.ErrPartial)
	return ok && partial.Current == nil && partial.Full == nil
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
