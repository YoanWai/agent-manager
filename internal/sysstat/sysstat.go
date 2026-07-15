package sysstat

import (
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Snapshot struct {
	CPUPercent  float64
	CPUOK       bool
	MemUsed     uint64
	MemTotal    uint64
	MemPercent  float64
	MemOK       bool
	Load1       float64
	Load5       float64
	Load15      float64
	LoadOK      bool
	DiskUsed    uint64
	DiskTotal   uint64
	DiskPercent float64
	DiskOK      bool
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

	if avg, err := load.Avg(); err == nil {
		snap.Load1 = avg.Load1
		snap.Load5 = avg.Load5
		snap.Load15 = avg.Load15
		snap.LoadOK = true
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

	return snap
}

func Proc(pid int) ProcStat {
	if pid <= 0 {
		return ProcStat{}
	}
	proc, err := process.NewProcess(int32(pid))
	if err != nil {
		return ProcStat{}
	}
	stat := ProcStat{OK: true}
	if cpuPct, err := proc.Percent(0); err == nil {
		stat.CPUPercent = cpuPct
	}
	if memInfo, err := proc.MemoryInfo(); err == nil && memInfo != nil {
		stat.RSS = memInfo.RSS
	}
	return stat
}
