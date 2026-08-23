//go:build linux

package sysstat

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/wsl"
)

// On WSL2 /proc describes the guest VM (half the Windows RAM by default,
// fewer cores, a virtual disk), while the Computer block is labeled as the
// machine. These probes read the real figures through PowerShell interop;
// each call costs about a second, so a background loop samples slowly and
// Sample() overlays the freshest reading onto the guest numbers.

const (
	probeTimeout    = 15 * time.Second
	hostInterval    = 10 * time.Second
	hostFreshWindow = 30 * time.Second
)

type HostSample struct {
	CPUPercent float64
	NCPU       int
	MemTotal   uint64
	MemFree    uint64
	DiskTotal  uint64
	DiskFree   uint64
}

var hostProbed = wsl.Detect

var hostLookupPowerShell = func() (string, bool) {
	if path, err := exec.LookPath("powershell.exe"); err == nil {
		return path, true
	}
	const systemPath = "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	if _, err := os.Stat(systemPath); err == nil {
		return systemPath, true
	}
	return "", false
}

var hostProbe = func(path string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-NoProfile", "-Command", hostProbeCommand)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

const hostProbeCommand = `[System.Threading.Thread]::CurrentThread.CurrentCulture=[cultureinfo]::InvariantCulture;` +
	`$cs=Get-CimInstance Win32_ComputerSystem;` +
	`$os=Get-CimInstance Win32_OperatingSystem;` +
	`$p=Get-CimInstance Win32_Processor|Measure-Object -Property LoadPercentage -Average;` +
	`$d=Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='C:'";` +
	`"{0} {1} {2} {3} {4} {5}" -f $(if($null -ne $p.Average){$p.Average}else{0}),` +
	`$cs.NumberOfLogicalProcessors,$cs.TotalPhysicalMemory,($os.FreePhysicalMemory*1KB),$d.Size,$d.FreeSpace`

func parseHostSample(line string) (HostSample, error) {
	fields := strings.Fields(line)
	if len(fields) != 6 {
		return HostSample{}, errors.New("host sample wants 6 fields")
	}
	cpu, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || cpu < 0 {
		return HostSample{}, errors.New("bad cpu field")
	}
	ncpu, err := strconv.Atoi(fields[1])
	if err != nil || ncpu < 1 {
		return HostSample{}, errors.New("bad ncpu field")
	}
	memTotal, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return HostSample{}, errors.New("bad mem total field")
	}
	memFree, err := strconv.ParseUint(fields[3], 10, 64)
	if err != nil {
		return HostSample{}, errors.New("bad mem free field")
	}
	diskTotal, err := strconv.ParseUint(fields[4], 10, 64)
	if err != nil {
		return HostSample{}, errors.New("bad disk total field")
	}
	diskFree, err := strconv.ParseUint(fields[5], 10, 64)
	if err != nil {
		return HostSample{}, errors.New("bad disk free field")
	}
	// A free figure above its total would underflow the used math below.
	if memFree > memTotal {
		return HostSample{}, errors.New("mem free exceeds total")
	}
	if diskFree > diskTotal {
		return HostSample{}, errors.New("disk free exceeds total")
	}
	return HostSample{
		CPUPercent: cpu,
		NCPU:       ncpu,
		MemTotal:   memTotal,
		MemFree:    memFree,
		DiskTotal:  diskTotal,
		DiskFree:   diskFree,
	}, nil
}

type hostCache struct {
	mu     sync.Mutex
	sample HostSample
	at     time.Time
}

var hostState hostCache

var hostSamplerStart sync.Once

func storeHost(s HostSample) {
	hostState.mu.Lock()
	hostState.sample = s
	hostState.at = time.Now()
	hostState.mu.Unlock()
}

func freshHostSample() (HostSample, bool) {
	hostState.mu.Lock()
	defer hostState.mu.Unlock()
	if hostState.at.IsZero() || time.Since(hostState.at) > hostFreshWindow {
		return HostSample{}, false
	}
	return hostState.sample, true
}

func startHostSampler() {
	hostSamplerStart.Do(func() {
		if !hostProbed() {
			return
		}
		path, ok := hostLookupPowerShell()
		if !ok {
			return
		}
		go runHostSampler(path, hostProbe)
	})
}

func runHostSampler(path string, probe func(string, time.Duration) (string, error)) {
	tick := func() {
		out, err := probe(path, probeTimeout)
		if err != nil {
			return
		}
		if s, err := parseHostSample(out); err == nil {
			storeHost(s)
		}
	}
	tick()
	ticker := time.NewTicker(hostInterval)
	defer ticker.Stop()
	for range ticker.C {
		tick()
	}
}

func overlayHost(snap *Snapshot) {
	s, ok := freshHostSample()
	if !ok {
		return
	}
	snap.CPUPercent = s.CPUPercent
	snap.CPUOK = true
	snap.MemTotal = s.MemTotal
	snap.MemUsed = s.MemTotal - s.MemFree
	snap.MemPercent = usedPercent(snap.MemUsed, s.MemTotal)
	snap.MemOK = true
	snap.DiskTotal = s.DiskTotal
	snap.DiskUsed = s.DiskTotal - s.DiskFree
	snap.DiskFree = s.DiskFree
	snap.DiskPercent = usedPercent(snap.DiskUsed, snap.DiskUsed+snap.DiskFree)
	snap.DiskOK = true
}

func hostMemTotal() (uint64, bool) {
	s, ok := freshHostSample()
	if !ok || s.MemTotal == 0 {
		return 0, false
	}
	return s.MemTotal, true
}

func hostNCPU() (int, bool) {
	s, ok := freshHostSample()
	if !ok || s.NCPU < 1 {
		return 0, false
	}
	return s.NCPU, true
}
