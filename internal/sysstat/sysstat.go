package sysstat

import (
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
)

type Snapshot struct {
	CPUPercent      float64
	CPUOK           bool
	MemUsed         uint64
	MemTotal        uint64
	MemPercent      float64
	MemOK           bool
	SwapUsed        uint64
	SwapTotal       uint64
	SwapPercent     float64
	SwapOK          bool
	DiskUsed        uint64
	DiskFree        uint64
	DiskTotal       uint64
	DiskPercent     float64
	DiskOK          bool
	NetSent         uint64
	NetRecv         uint64
	NetOK           bool
	CPUTemp         float64
	CPUTempOK       bool
	GPUTemp         float64
	GPUTempOK       bool
	SoCTemp         float64
	SoCTempOK       bool
	BatteryPercent  float64
	BatteryCharging bool
	BatteryOK       bool
}

type ProcStat struct {
	// CPUPercent is host capacity share 0–100 after interval scaling
	// (or after ScaleToHost's pcpu fallback).
	CPUPercent float64
	// RamPercent is 0 until host scaling; then 0–100 of installed RAM.
	RamPercent float64
	RSS        uint64
	// CPUSeconds is cumulative user+system CPU time for the whole tree.
	// Used with a previous sample to compute interval host share.
	CPUSeconds float64
	// PCPU is the raw ps pcpu sum (100 ≈ one core). Fallback only.
	PCPU  float64
	Procs int
	OK    bool
	// Children names what the root pid runs directly, one entry per child
	// process, as the program was invoked. A tmux pane's root is its shell,
	// so this is the agent that shell is running right now.
	Children []string
}

func Sample(diskPath string) Snapshot {
	var snap Snapshot

	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		snap.CPUPercent = percents[0]
		snap.CPUOK = true
	}

	sampleMemory(&snap)
	sampleSwap(&snap)
	sampleDisk(&snap, diskPath)
	sampleNet(&snap)
	sampleTemps(&snap)
	sampleBattery(&snap)
	startHostSampler()
	overlayHost(&snap)

	return snap
}

// sampleSwap reads swap usage and sets SwapPercent as used/total.
// That is the only meaningful fill fraction: on macOS the swap file
// grows under pressure, so the denominator is the current allocation
// from vm.swapusage, not a fixed partition size.
func sampleSwap(snap *Snapshot) {
	sm, err := mem.SwapMemory()
	if err != nil {
		return
	}
	snap.SwapUsed = sm.Used
	snap.SwapTotal = sm.Total
	snap.SwapPercent = usedPercent(sm.Used, sm.Total)
	snap.SwapOK = true
}

func sampleDisk(snap *Snapshot, diskPath string) {
	if diskPath == "" {
		diskPath = "/"
	}
	usage, err := disk.Usage(diskPath)
	if err != nil {
		return
	}
	snap.DiskUsed = usage.Used
	snap.DiskFree = usage.Free
	snap.DiskTotal = usage.Total
	// used/(used+free) matches df Capacity and ignores reserved blocks
	// that sit in Total but are not available to ordinary processes.
	if usable := usage.Used + usage.Free; usable > 0 {
		snap.DiskPercent = usedPercent(usage.Used, usable)
	} else {
		snap.DiskPercent = usage.UsedPercent
	}
	snap.DiskOK = true
}

// sampleNet sums counters for real NICs only. Loopback and common
// virtual interfaces would otherwise dominate the rate on a busy local
// machine (IPC, VPN tunnels, AWDL).
func sampleNet(snap *Snapshot) {
	counters, err := net.IOCounters(true)
	if err != nil || len(counters) == 0 {
		return
	}
	var sent, recv uint64
	var any bool
	for _, counter := range counters {
		if !countNetInterface(counter.Name) {
			continue
		}
		sent += counter.BytesSent
		recv += counter.BytesRecv
		any = true
	}
	if !any {
		return
	}
	snap.NetSent = sent
	snap.NetRecv = recv
	snap.NetOK = true
}

func countNetInterface(name string) bool {
	n := strings.ToLower(name)
	if n == "lo" || n == "lo0" {
		return false
	}
	for _, prefix := range netSkipPrefixes {
		if strings.HasPrefix(n, prefix) {
			return false
		}
	}
	return true
}

// Virtual / point-to-point / container bridges that are not "the network".
var netSkipPrefixes = []string{
	"utun", "awdl", "llw", "bridge", "gif", "stf", "anpi", "ap",
	"vmenet", "vboxnet", "docker", "br-", "veth", "cni", "flannel",
	"virbr", "tun", "tap", "wg", "zt", "tailscale", "ipsec", "vmnet",
}

func usedPercent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(used) / float64(total)
}

// A read builds an IOKit HID client on Apple Silicon and costs ~57ms.
const tempRefresh = 5 * time.Second

type tempReading struct {
	cpu   float64
	cpuOK bool
	gpu   float64
	gpuOK bool
	soc   float64
	socOK bool
}

var tempCache struct {
	mu     sync.Mutex
	filled bool
	at     time.Time
	last   tempReading
}

func sampleTemps(snap *Snapshot) {
	reading := cachedTemps()
	snap.CPUTemp, snap.CPUTempOK = reading.cpu, reading.cpuOK
	snap.GPUTemp, snap.GPUTempOK = reading.gpu, reading.gpuOK
	snap.SoCTemp, snap.SoCTempOK = reading.soc, reading.socOK
}

func cachedTemps() tempReading {
	tempCache.mu.Lock()
	defer tempCache.mu.Unlock()
	if tempCache.filled && time.Since(tempCache.at) < tempRefresh {
		return tempCache.last
	}
	// Linux reports a warning for the sensors it could not read and still
	// returns the ones it could.
	read, err := sensors.SensorsTemperatures()
	if len(read) == 0 && err != nil {
		return tempReading{}
	}
	tempCache.last = classifyTemps(read)
	tempCache.at = time.Now()
	tempCache.filled = true
	return tempCache.last
}

// Apple Silicon draws no CPU/GPU line, so its dies collapse into one SoC
// reading. Each category keeps its hottest sensor.
func classifyTemps(read []sensors.TemperatureStat) tempReading {
	var reading tempReading
	for _, sensor := range read {
		if !plausibleTemp(sensor.Temperature) {
			continue
		}
		key := strings.ToLower(sensor.SensorKey)
		switch {
		case isGPUSensor(key):
			if sensor.Temperature > reading.gpu {
				reading.gpu = sensor.Temperature
			}
			reading.gpuOK = true
		case isCPUSensor(key):
			if sensor.Temperature > reading.cpu {
				reading.cpu = sensor.Temperature
			}
			reading.cpuOK = true
		case isDieSensor(key):
			if sensor.Temperature > reading.soc {
				reading.soc = sensor.Temperature
			}
			reading.socOK = true
		}
	}
	if reading.cpuOK || reading.gpuOK {
		reading.soc, reading.socOK = 0, false
	}
	return reading
}

// An absent sensor reads 0, NaN or an infinity, and a confused driver reads
// thousands of degrees. Requiring a positive value is what rejects NaN.
func plausibleTemp(celsius float64) bool {
	return celsius > 0 && celsius <= 150
}

func isGPUSensor(key string) bool {
	return strings.Contains(key, "gpu") ||
		strings.Contains(key, "tg0") ||
		strings.Contains(key, "nvidia") ||
		strings.Contains(key, "radeon") ||
		strings.Contains(key, "nouveau")
}

func isCPUSensor(key string) bool {
	return strings.Contains(key, "cpu") ||
		strings.Contains(key, "tc0") ||
		strings.Contains(key, "coretemp") ||
		strings.Contains(key, "k10temp") ||
		strings.Contains(key, "zenpower") ||
		strings.Contains(key, "package") ||
		strings.Contains(key, "pkg") ||
		strings.Contains(key, "tctl") ||
		strings.Contains(key, "tccd")
}

func isDieSensor(key string) bool {
	return strings.Contains(key, "tdie") || strings.Contains(key, "soc")
}

// LogicalCPUs is the number of logical processors used as the denominator
// when converting process-style pcpu into a share of the machine.
func LogicalCPUs() int {
	if n, ok := hostNCPU(); ok {
		return n
	}
	if n, err := cpu.Counts(true); err == nil && n > 0 {
		return n
	}
	if n := runtime.NumCPU(); n > 0 {
		return n
	}
	return 1
}

// MemTotalBytes is installed RAM, used as the denominator for agent RAM %.
func MemTotalBytes() (uint64, bool) {
	if t, ok := hostMemTotal(); ok {
		return t, true
	}
	vm, err := mem.VirtualMemory()
	if err != nil || vm.Total == 0 {
		return 0, false
	}
	return vm.Total, true
}

// HostCPUPercent turns a process-style pcpu sum (100 ≈ one full core) into
// a percentage of total machine capacity, clamped to [0, 100]. Prefer
// HostCPUFromDelta when an interval sample is available; pcpu is a coarse
// fallback and can disagree with the host gauge.
func HostCPUPercent(pcpu float64, ncpu int) float64 {
	if ncpu < 1 {
		ncpu = 1
	}
	return clampPct(pcpu / float64(ncpu))
}

// HostCPUFromDelta is agent CPU time used over an interval as a share of
// total machine capacity: cpuSec / (elapsed * ncpu) * 100. Same unit as
// the host gauge (0–100% of the box), so a busy agent fleet cannot
// honestly read higher than full machine use.
func HostCPUFromDelta(cpuSecDelta, elapsedSec float64, ncpu int) float64 {
	if elapsedSec <= 0 || ncpu < 1 {
		return 0
	}
	if cpuSecDelta < 0 {
		cpuSecDelta = 0
	}
	return clampPct(cpuSecDelta / (elapsedSec * float64(ncpu)) * 100)
}

// HostRAMPercent is rss as a percentage of installed RAM, clamped to [0, 100].
func HostRAMPercent(rss, memTotal uint64) float64 {
	if memTotal == 0 {
		return 0
	}
	return clampPct(float64(rss) / float64(memTotal) * 100)
}

func clampPct(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// ScaleToHost sets CPU/RAM to machine shares using pcpu fallback for CPU.
// Prefer interval scaling in the poller; this path is for one-shot samples
// (preview) that have no previous CPU-seconds reading.
func (s ProcStat) ScaleToHost(ncpu int, memTotal uint64) ProcStat {
	if !s.OK {
		return s
	}
	pcpu := s.PCPU
	if pcpu == 0 {
		pcpu = s.CPUPercent
	}
	s.CPUPercent = HostCPUPercent(pcpu, ncpu)
	s.RamPercent = HostRAMPercent(s.RSS, memTotal)
	return s
}
