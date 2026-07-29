//go:build darwin

package sysstat

import (
	"os/exec"

	"github.com/shirou/gopsutil/v4/mem"
	"golang.org/x/sys/unix"
)

// sampleMemory on macOS matches Activity Monitor's "Memory Used":
//
//	App Memory + Wired + Compressed
//	  = (anonymous - purgeable) + wired + compressor pages
//
// gopsutil's VirtualMemory treats all inactive pages as available, which
// drifts from Activity Monitor under memory pressure (anonymous inactive
// is not free). vm_stat is the same source Activity Monitor-style tools
// use and keeps the number honest without CGO.
func sampleMemory(snap *Snapshot) {
	total, err := unix.SysctlUint64("hw.memsize")
	if err != nil || total == 0 {
		sampleMemoryFallback(snap)
		return
	}
	used, ok := memoryUsedFromVMStat()
	if !ok {
		sampleMemoryFallback(snap)
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

func sampleMemoryFallback(snap *Snapshot) {
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

func memoryUsedFromVMStat() (uint64, bool) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, false
	}
	return parseVMStatMemoryUsed(string(out), uint64(unix.Getpagesize()))
}
