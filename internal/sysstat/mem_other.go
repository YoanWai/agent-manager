//go:build !darwin

package sysstat

func sampleMemory(snap *Snapshot) {
	sampleAvailableMemory(snap)
}
