//go:build darwin

package sysstat

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

// sampleMemory on macOS matches Activity Monitor's "Memory Used":
// resident RAM minus free, speculative, and reclaimable file cache.
// gopsutil's VirtualMemory treats all inactive pages as available and
// under-counts kernel/other pages; vm_stat keeps the number honest
// without CGO.
func sampleMemory(snap *Snapshot) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		sampleAvailableMemory(snap)
		return
	}
	used, ok := memoryUsedFromVMStat(total)
	if !ok {
		sampleAvailableMemory(snap)
		return
	}
	if used > total {
		used = total
	}
	snap.MemTotal = total
	snap.MemUsed = used
	snap.MemPercent = usedPercent(used, total)
	snap.MemOK = true
}

func memoryUsedFromVMStat(total uint64) (uint64, bool) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, false
	}
	return parseVMStatMemoryUsed(string(out), total, uint64(unix.Getpagesize()))
}
