//go:build !darwin

package sysstat

func sampleDisk(snap *Snapshot, diskPath string) {
	sampleDiskFallback(snap, diskPath)
}
