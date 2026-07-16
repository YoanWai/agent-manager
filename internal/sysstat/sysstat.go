package sysstat

import (
	"sync"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type Snapshot struct {
	CPUPercent  float64
	CPUOK       bool
	MemUsed     uint64
	MemTotal    uint64
	MemPercent  float64
	MemOK       bool
	SwapUsed    uint64
	SwapTotal   uint64
	SwapPercent float64
	SwapOK      bool
	DiskUsed    uint64
	DiskTotal   uint64
	DiskPercent float64
	DiskOK      bool
	NetSent     uint64
	NetRecv     uint64
	NetOK       bool
}

type ProcStat struct {
	CPUPercent float64
	RSS        uint64
	OK         bool
}

func Sample(diskPath string) Snapshot {
	var snap Snapshot

	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		snap.CPUPercent = percents[0]
		snap.CPUOK = true
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		snap.MemUsed = vm.Used
		snap.MemTotal = vm.Total
		snap.MemPercent = vm.UsedPercent
		snap.MemOK = true
	}

	if sm, err := mem.SwapMemory(); err == nil {
		snap.SwapUsed = sm.Used
		snap.SwapTotal = sm.Total
		snap.SwapPercent = sm.UsedPercent
		snap.SwapOK = true
	}

	if diskPath == "" {
		diskPath = "/"
	}
	if usage, err := disk.Usage(diskPath); err == nil {
		snap.DiskUsed = usage.Used
		snap.DiskTotal = usage.Total
		snap.DiskPercent = usage.UsedPercent
		snap.DiskOK = true
	}

	// Cumulative bytes across all interfaces since boot; the caller
	// diffs consecutive snapshots to get transfer rates.
	if counters, err := net.IOCounters(false); err == nil && len(counters) > 0 {
		snap.NetSent = counters[0].BytesSent
		snap.NetRecv = counters[0].BytesRecv
		snap.NetOK = true
	}

	return snap
}

// procCache keeps one handle per pid across polls so Percent(0) returns
// usage since the previous poll instead of an average since process start.
var (
	procMu    sync.Mutex
	procCache = map[int32]*process.Process{}
)

func cachedProc(pid int32) (*process.Process, error) {
	procMu.Lock()
	defer procMu.Unlock()
	if proc, ok := procCache[pid]; ok {
		return proc, nil
	}
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil, err
	}
	procCache[pid] = proc
	return proc, nil
}

func dropProc(pid int32) {
	procMu.Lock()
	delete(procCache, pid)
	procMu.Unlock()
}

// Proc reports the combined CPU and resident memory of a process and all
// of its descendants. tmux pane pids are shells whose real work happens
// in child processes, so a tree sum is the only honest number.
func Proc(pid int) ProcStat {
	if pid <= 0 {
		return ProcStat{}
	}
	var stat ProcStat
	seen := map[int32]bool{}
	var walk func(pid int32)
	walk = func(pid int32) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		proc, err := cachedProc(pid)
		if err != nil {
			return
		}
		if running, err := proc.IsRunning(); err != nil || !running {
			dropProc(pid)
			return
		}
		stat.OK = true
		if cpuPct, err := proc.Percent(0); err == nil {
			stat.CPUPercent += cpuPct
		}
		if memInfo, err := proc.MemoryInfo(); err == nil && memInfo != nil {
			stat.RSS += memInfo.RSS
		}
		if children, err := proc.Children(); err == nil {
			for _, child := range children {
				walk(child.Pid)
			}
		}
	}
	walk(int32(pid))
	return stat
}
