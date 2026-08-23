//go:build !linux

package sysstat

func startHostSampler() {}

func overlayHost(*Snapshot) {}

func hostMemTotal() (uint64, bool) {
	return 0, false
}

func hostNCPU() (int, bool) {
	return 0, false
}
