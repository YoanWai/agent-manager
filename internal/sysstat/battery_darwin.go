//go:build darwin

package sysstat

import (
	"os/exec"
	"sync"
	"time"

	"github.com/distatus/battery"
	"howett.net/plist"
)

const batteryRefresh = 10 * time.Second

// batterySource is swappable in tests for a fake battery source.
var batterySource = cachedDarwinBattery

var batteryCache struct {
	mu   sync.Mutex
	at   time.Time
	last []*battery.Battery
	err  error
}

func cachedDarwinBattery() ([]*battery.Battery, error) {
	batteryCache.mu.Lock()
	defer batteryCache.mu.Unlock()
	if !batteryCache.at.IsZero() && time.Since(batteryCache.at) < batteryRefresh {
		return batteryCache.last, batteryCache.err
	}
	batteryCache.last, batteryCache.err = readDarwinBattery()
	batteryCache.at = time.Now()
	return batteryCache.last, batteryCache.err
}

// readDarwinBattery reads CurrentCapacity over MaxCapacity, the pair the
// menu bar and pmset show. The library divides the raw capacities, which
// reads a point or two under.
func readDarwinBattery() ([]*battery.Battery, error) {
	out, err := exec.Command("ioreg", "-n", "AppleSmartBattery", "-r", "-a").Output()
	if err != nil {
		return nil, err
	}
	return parseDarwinBattery(out)
}

func parseDarwinBattery(data []byte) ([]*battery.Battery, error) {
	var entries []struct {
		CurrentCapacity int
		MaxCapacity     int
		IsCharging      bool
	}
	if _, err := plist.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	batteries := make([]*battery.Battery, 0, len(entries))
	for _, entry := range entries {
		state := battery.Discharging
		if entry.IsCharging {
			state = battery.Charging
		}
		batteries = append(batteries, &battery.Battery{
			Current: float64(entry.CurrentCapacity),
			Full:    float64(entry.MaxCapacity),
			State:   battery.State{Raw: state},
		})
	}
	return batteries, nil
}
