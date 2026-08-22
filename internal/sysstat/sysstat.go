package sysstat

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
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
	DiskFree    uint64
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

// parsePSTime turns a ps time/cputime field into cumulative CPU seconds.
// Handles DD-HH:MM:SS, HH:MM:SS, and the common Darwin MM:SS.ss form
// (minutes may exceed 59).
func parsePSTime(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, nil
	}
	var days float64
	if i := strings.IndexByte(s, '-'); i >= 0 {
		d, err := strconv.ParseFloat(s[:i], 64)
		if err != nil {
			return 0, err
		}
		days = d
		s = s[i+1:]
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		sec, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0, err
		}
		return days*86400 + sec, nil
	case 2:
		min, err1 := strconv.ParseFloat(parts[0], 64)
		sec, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 != nil || err2 != nil {
			return 0, fmt.Errorf("ps time %q", s)
		}
		return days*86400 + min*60 + sec, nil
	case 3:
		hour, err1 := strconv.ParseFloat(parts[0], 64)
		min, err2 := strconv.ParseFloat(parts[1], 64)
		sec, err3 := strconv.ParseFloat(parts[2], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, fmt.Errorf("ps time %q", s)
		}
		return days*86400 + hour*3600 + min*60 + sec, nil
	default:
		return 0, fmt.Errorf("ps time %q", s)
	}
}

func nextField(line string) (string, string) {
	line = strings.TrimLeft(line, " ")
	if i := strings.IndexByte(line, ' '); i >= 0 {
		return line[:i], line[i+1:]
	}
	return line, ""
}

// Trees reports the combined CPU and resident memory of each requested
// process and all of its descendants, from one ps pass over the machine
// and a second limited to the roots' own children. tmux pane pids are
// shells whose real work happens in child processes, so a tree sum is the
// only honest number.
//
// CPUSeconds is cumulative CPU time for interval host-share math. PCPU is
// the raw ps %cpu sum (fallback). Callers convert to host % via
// HostCPUFromDelta between polls, or ScaleToHost for a one-shot sample.
func Trees(rootPIDs []int) map[int]ProcStat {
	stats := make(map[int]ProcStat, len(rootPIDs))
	if len(rootPIDs) == 0 {
		return stats
	}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,pcpu=,rss=,time=").Output()
	if err != nil {
		return stats
	}

	type proc struct {
		pcpu    float64
		rss     uint64
		cpuSecs float64
	}
	procs := map[int]proc{}
	children := map[int][]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 5 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		cpuPct, err3 := strconv.ParseFloat(fields[2], 64)
		rssKB, err4 := strconv.ParseUint(fields[3], 10, 64)
		cpuSecs, err5 := parsePSTime(fields[4])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
			continue
		}
		procs[pid] = proc{pcpu: cpuPct, rss: rssKB * 1024, cpuSecs: cpuSecs}
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
			stat.PCPU += procs[pid].pcpu
			stat.CPUSeconds += procs[pid].cpuSecs
			stat.RSS += procs[pid].rss
			for _, child := range children[pid] {
				walk(child)
			}
		}
		walk(root)
		// Leave CPUPercent 0 until the caller applies interval or fallback.
		stats[root] = stat
	}
	nameChildren(stats, children)
	return stats
}

// nameChildren fills in what each root runs directly. The programs come
// from a second ps limited to those pids: arguments cost the kernel a
// lookup per process, which is worth paying for a pane's own children and
// not for every process on the machine.
func nameChildren(stats map[int]ProcStat, children map[int][]int) {
	var wanted []string
	for root := range stats {
		for _, child := range children[root] {
			wanted = append(wanted, strconv.Itoa(child))
		}
	}
	if len(wanted) == 0 {
		return
	}
	// ps exits non-zero when every pid it was given has gone, which is a
	// child that ended between the two calls rather than a failure: there is
	// nothing left to name and the next sample sees whatever replaced it.
	out, err := exec.Command("ps", "-o", "pid=,ppid=,args=", "-p", strings.Join(wanted, ",")).Output()
	if err != nil {
		return
	}
	applyChildNames(stats, children, string(out))
}

// applyChildNames matches the second ps pass back to the tree the first one
// built. The parent has to still be the root it was sampled under: a child
// that exited between the two calls leaves its pid free for a process that
// is nothing to do with this pane.
func applyChildNames(stats map[int]ProcStat, children map[int][]int, psOutput string) {
	type child struct {
		ppid    int
		command string
	}
	named := map[int]child{}
	for _, line := range strings.Split(strings.TrimSpace(psOutput), "\n") {
		pidText, rest := nextField(line)
		ppidText, rest := nextField(rest)
		command, _ := nextField(rest)
		pid, err1 := strconv.Atoi(pidText)
		ppid, err2 := strconv.Atoi(ppidText)
		if err1 != nil || err2 != nil || command == "" {
			continue
		}
		named[pid] = child{ppid: ppid, command: command}
	}
	for root, stat := range stats {
		for _, pid := range children[root] {
			if named[pid].ppid == root {
				stat.Children = append(stat.Children, named[pid].command)
			}
		}
		stats[root] = stat
	}
}
