//go:build !darwin

package sysstat

import "github.com/shirou/gopsutil/v4/mem"

// sampleMemory uses the kernel's available figure so file cache is not
// counted as used (Linux MemAvailable, and the same field on other
// platforms gopsutil maps to Available). Percent is always used/total.
func sampleMemory(snap *Snapshot) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return
	}
	snap.MemTotal = vm.Total
	if vm.Total >= vm.Available {
		snap.MemUsed = vm.Total - vm.Available
	} else {
		snap.MemUsed = vm.Used
	}
	snap.MemPercent = usedPercent(snap.MemUsed, snap.MemTotal)
	snap.MemOK = true
}
