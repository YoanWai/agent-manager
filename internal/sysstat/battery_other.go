//go:build !darwin

package sysstat

import "github.com/distatus/battery"

// batterySource is swappable in tests for a fake battery source.
var batterySource = battery.GetAll
