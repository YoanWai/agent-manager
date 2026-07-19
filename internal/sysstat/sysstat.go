package sysstat

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
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
	CPUTemp     float64
	CPUTempOK   bool
	GPUTemp     float64
	GPUTempOK   bool
	SoCTemp     float64
	SoCTempOK   bool
}

type ProcStat struct {
	CPUPercent float64
	RSS        uint64
	Procs      int
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

	sampleTemps(&snap)

	return snap
}

// sampleTemps categorizes hardware temperature sensors into CPU and GPU
// readings. Sensor names differ by platform: Intel Macs expose SMC keys
// (TC0D/TG0D), Linux exposes hwmon labels (coretemp/amdgpu), and Apple
// Silicon exposes only unlabeled per-chiplet die temperatures. When no
// CPU/GPU split is available (Apple Silicon), the hottest die temperature
// is reported as a single SoC reading. Each category keeps the hottest
// matching sensor.
func sampleTemps(snap *Snapshot) {
	temps, err := sensors.SensorsTemperatures()
	if err != nil || len(temps) == 0 {
		return
	}
	var cpu, gpu, die float64
	for _, sensor := range temps {
		if sensor.Temperature <= 0 {
			continue
		}
		key := strings.ToLower(sensor.SensorKey)
		switch {
		case isGPUSensor(key):
			if sensor.Temperature > gpu {
				gpu = sensor.Temperature
			}
			snap.GPUTempOK = true
		case isCPUSensor(key):
			if sensor.Temperature > cpu {
				cpu = sensor.Temperature
			}
			snap.CPUTempOK = true
		case strings.Contains(key, "tdie"):
			if sensor.Temperature > die {
				die = sensor.Temperature
			}
		}
	}
	snap.CPUTemp = cpu
	snap.GPUTemp = gpu
	if !snap.CPUTempOK && !snap.GPUTempOK && die > 0 {
		snap.SoCTemp = die
		snap.SoCTempOK = true
	}
}

func isGPUSensor(key string) bool {
	return strings.Contains(key, "gpu") ||
		strings.Contains(key, "tg0") ||
		strings.Contains(key, "nvidia") ||
		strings.Contains(key, "radeon")
}

func isCPUSensor(key string) bool {
	return strings.Contains(key, "cpu") ||
		strings.Contains(key, "tc0") ||
		strings.Contains(key, "coretemp") ||
		strings.Contains(key, "k10temp") ||
		strings.Contains(key, "package")
}

// Trees reports the combined CPU and resident memory of each requested
// process and all of its descendants, from a single ps invocation. tmux
// pane pids are shells whose real work happens in child processes, so a
// tree sum is the only honest number. ps %cpu is a recent decaying
// average, which suits a 2s poll.
func Trees(rootPIDs []int) map[int]ProcStat {
	stats := make(map[int]ProcStat, len(rootPIDs))
	if len(rootPIDs) == 0 {
		return stats
	}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,pcpu=,rss=").Output()
	if err != nil {
		return stats
	}

	type proc struct {
		cpu float64
		rss uint64
	}
	procs := map[int]proc{}
	children := map[int][]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 4 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		cpuPct, err3 := strconv.ParseFloat(fields[2], 64)
		rssKB, err4 := strconv.ParseUint(fields[3], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		procs[pid] = proc{cpu: cpuPct, rss: rssKB * 1024}
		children[ppid] = append(children[ppid], pid)
	}

	for _, root := range rootPIDs {
		if _, alive := procs[root]; !alive {
			continue
		}
		stat := ProcStat{OK: true}
		seen := map[int]bool{}
		var walk func(pid int)
		walk = func(pid int) {
			if seen[pid] {
				return
			}
			seen[pid] = true
			stat.Procs++
			stat.CPUPercent += procs[pid].cpu
			stat.RSS += procs[pid].rss
			for _, child := range children[pid] {
				walk(child)
			}
		}
		walk(root)
		stats[root] = stat
	}
	return stats
}
