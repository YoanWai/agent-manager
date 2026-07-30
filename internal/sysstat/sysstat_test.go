package sysstat

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/shirou/gopsutil/v4/sensors"
)

func TestSample(t *testing.T) {
	snap := Sample("/")
	if snap.MemOK && snap.MemTotal == 0 {
		t.Fatal("mem reported OK but total is zero")
	}
	if snap.MemOK {
		if snap.MemUsed > snap.MemTotal {
			t.Fatalf("mem used %d > total %d", snap.MemUsed, snap.MemTotal)
		}
		want := usedPercent(snap.MemUsed, snap.MemTotal)
		if math.Abs(snap.MemPercent-want) > 0.01 {
			t.Fatalf("mem percent %v != used/total %v", snap.MemPercent, want)
		}
	}
	if snap.SwapOK && snap.SwapTotal > 0 {
		want := usedPercent(snap.SwapUsed, snap.SwapTotal)
		if math.Abs(snap.SwapPercent-want) > 0.01 {
			t.Fatalf("swap percent %v != used/total %v", snap.SwapPercent, want)
		}
		if snap.SwapUsed > snap.SwapTotal {
			t.Fatalf("swap used %d > total %d", snap.SwapUsed, snap.SwapTotal)
		}
	}
	if snap.DiskOK && snap.DiskTotal == 0 {
		t.Fatal("disk reported OK but total is zero")
	}
	if snap.DiskOK {
		if snap.DiskFree == 0 && snap.DiskUsed == 0 {
			t.Fatal("disk free and used both zero")
		}
		// Free is what the UI shows; it must be the kernel's available
		// figure (Bavail), not Total-Used (which includes reserved).
		if snap.DiskFree > snap.DiskTotal {
			t.Fatalf("disk free %d > total %d", snap.DiskFree, snap.DiskTotal)
		}
	}
	if snap.CPUTempOK && snap.CPUTemp <= 0 {
		t.Fatal("cpu temp reported OK but not positive")
	}
	if snap.GPUTempOK && snap.GPUTemp <= 0 {
		t.Fatal("gpu temp reported OK but not positive")
	}
	if snap.SoCTempOK && snap.SoCTemp <= 0 {
		t.Fatal("soc temp reported OK but not positive")
	}
	if snap.SoCTempOK && (snap.CPUTempOK || snap.GPUTempOK) {
		t.Fatal("soc temp should only be set when cpu/gpu split is unavailable")
	}
}

func TestSensorCategories(t *testing.T) {
	gpuKeys := []string{"tg0d", "gpu 0", "amdgpu", "nvidia gpu", "radeon"}
	for _, key := range gpuKeys {
		if !isGPUSensor(key) {
			t.Fatalf("expected %q to be a GPU sensor", key)
		}
		if isCPUSensor(key) {
			t.Fatalf("expected %q not to be a CPU sensor", key)
		}
	}
	cpuKeys := []string{"tc0d", "cpu 0", "coretemp_packageid0", "k10temp", "package id 0"}
	for _, key := range cpuKeys {
		if !isCPUSensor(key) {
			t.Fatalf("expected %q to be a CPU sensor", key)
		}
	}
	dieKeys := []string{"pmu tdie1", "pmu2 tdie8"}
	for _, key := range dieKeys {
		if isCPUSensor(key) || isGPUSensor(key) {
			t.Fatalf("apple silicon die key %q should be neither cpu nor gpu", key)
		}
	}
}

func TestClassifyTemps(t *testing.T) {
	cases := []struct {
		name string
		read []sensors.TemperatureStat
		want tempReading
	}{
		{
			name: "apple silicon dies fold into one soc reading",
			read: []sensors.TemperatureStat{
				{SensorKey: "PMU tdie1", Temperature: 60.4},
				{SensorKey: "PMU2 tdie8", Temperature: 50.7},
				{SensorKey: "gas gauge battery", Temperature: 27.8},
				{SensorKey: "NAND CH0 temp", Temperature: 42},
			},
			want: tempReading{soc: 60.4, socOK: true},
		},
		{
			name: "intel mac smc keys split cpu and gpu",
			read: []sensors.TemperatureStat{
				{SensorKey: "TC0D", Temperature: 61},
				{SensorKey: "TG0D", Temperature: 55},
			},
			want: tempReading{cpu: 61, cpuOK: true, gpu: 55, gpuOK: true},
		},
		{
			name: "intel linux keeps the package over its cores",
			read: []sensors.TemperatureStat{
				{SensorKey: "coretemp_package_id_0", Temperature: 58},
				{SensorKey: "coretemp_core_0", Temperature: 54},
				{SensorKey: "nvme_composite", Temperature: 47},
				{SensorKey: "iwlwifi_1", Temperature: 40},
			},
			want: tempReading{cpu: 58, cpuOK: true},
		},
		{
			name: "amd names the cpu tctl and the gpu amdgpu",
			read: []sensors.TemperatureStat{
				{SensorKey: "k10temp_tctl", Temperature: 64},
				{SensorKey: "k10temp_tccd1", Temperature: 59},
				{SensorKey: "amdgpu_edge", Temperature: 49},
			},
			want: tempReading{cpu: 64, cpuOK: true, gpu: 49, gpuOK: true},
		},
		{
			name: "zenpower tdie is a cpu, not a bare die",
			read: []sensors.TemperatureStat{
				{SensorKey: "zenpower_tdie", Temperature: 66},
			},
			want: tempReading{cpu: 66, cpuOK: true},
		},
		{
			name: "thermal zones name the chip by type",
			read: []sensors.TemperatureStat{
				{SensorKey: "cpu-thermal", Temperature: 52},
				{SensorKey: "x86_pkg_temp", Temperature: 57},
			},
			want: tempReading{cpu: 57, cpuOK: true},
		},
		{
			name: "an soc thermal zone alone reads as the soc",
			read: []sensors.TemperatureStat{
				{SensorKey: "soc-thermal", Temperature: 48},
			},
			want: tempReading{soc: 48, socOK: true},
		},
		{
			name: "zeroed and unknown sensors report nothing",
			read: []sensors.TemperatureStat{
				{SensorKey: "coretemp_core_0", Temperature: 0},
				{SensorKey: "acpitz", Temperature: 27.8},
			},
			want: tempReading{},
		},
		{
			name: "a sensor reporting its own absence is not a reading",
			read: []sensors.TemperatureStat{
				{SensorKey: "coretemp_core_0", Temperature: math.NaN()},
				{SensorKey: "amdgpu_edge", Temperature: math.Inf(1)},
				{SensorKey: "k10temp_tctl", Temperature: 3276.7},
			},
			want: tempReading{},
		},
		{
			name: "a live sensor outlives its broken neighbours",
			read: []sensors.TemperatureStat{
				{SensorKey: "coretemp_core_0", Temperature: math.NaN()},
				{SensorKey: "coretemp_package_id_0", Temperature: 58},
			},
			want: tempReading{cpu: 58, cpuOK: true},
		},
		{
			name: "no sensors at all",
			read: nil,
			want: tempReading{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTemps(tc.read); got != tc.want {
				t.Fatalf("classifyTemps() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTreesSelf(t *testing.T) {
	pid := os.Getpid()
	stat := Trees([]int{pid})[pid]
	if !stat.OK {
		t.Fatal("expected OK stat for current process")
	}
	if stat.RSS == 0 {
		t.Fatal("expected non-zero RSS for current process")
	}
}

func TestTreesInvalid(t *testing.T) {
	if Trees([]int{-1})[-1].OK {
		t.Fatal("negative pid should not be OK")
	}
	if len(Trees(nil)) != 0 {
		t.Fatal("no pids should yield no stats")
	}
}

func TestCountNetInterface(t *testing.T) {
	keep := []string{"en0", "en1", "eth0", "wlan0", "wlp2s0"}
	for _, name := range keep {
		if !countNetInterface(name) {
			t.Fatalf("expected to count %q", name)
		}
	}
	skip := []string{"lo", "lo0", "utun4", "awdl0", "llw0", "bridge0", "docker0", "br-abc", "veth0", "vmenet0", "ap1"}
	for _, name := range skip {
		if countNetInterface(name) {
			t.Fatalf("expected to skip %q", name)
		}
	}
}

func TestUsedPercent(t *testing.T) {
	if usedPercent(0, 0) != 0 {
		t.Fatal("zero total should yield 0")
	}
	if usedPercent(1, 4) != 25 {
		t.Fatalf("got %v", usedPercent(1, 4))
	}
	if usedPercent(3, 3) != 100 {
		t.Fatalf("got %v", usedPercent(3, 3))
	}
}

func TestParseVMStatMemoryUsed(t *testing.T) {
	// total pages = 10000 * 16384
	// reclaimable = free(100)+spec(50)+file(2000)+purg(100) = 2250
	// used pages = 10000 - 2250 = 7750
	const body = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               100.
Pages active:                            5000.
Pages inactive:                          4000.
Pages speculative:                        50.
Pages wired down:                       2000.
Pages purgeable:                         100.
Anonymous pages:                        1000.
File-backed pages:                      2000.
Pages occupied by compressor:           3000.
`
	total := uint64(10000) * 16384
	used, ok := parseVMStatMemoryUsed(body, total, 4096)
	if !ok {
		t.Fatal("expected parse ok")
	}
	want := uint64(7750) * 16384
	if used != want {
		t.Fatalf("used=%d want=%d", used, want)
	}
}

func TestParseVMStatMemoryUsedLegacyWithoutFileBacked(t *testing.T) {
	// No File-backed line: fall back to app+wired+comp.
	const body = `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                             1.
Pages wired down:                       10.
Pages purgeable:                         0.
Anonymous pages:                        20.
Pages used by compressor:               5.
`
	used, ok := parseVMStatMemoryUsed(body, 0, 4096)
	if !ok {
		t.Fatal("expected parse ok")
	}
	if used != 35*4096 {
		t.Fatalf("used=%d", used)
	}
}

func TestSampleMemoryMatchesVMStatOnDarwin(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	snap := Sample("/")
	if !snap.MemOK {
		t.Fatal("mem not ok")
	}
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		t.Fatal(err)
	}
	want, ok := parseVMStatMemoryUsed(string(out), snap.MemTotal, 0)
	if !ok {
		t.Fatal("vm_stat parse failed")
	}
	delta := float64(snap.MemUsed) - float64(want)
	if delta < 0 {
		delta = -delta
	}
	if delta > 256<<20 {
		t.Fatalf("mem used %d drifted %v from live vm_stat %d", snap.MemUsed, delta, want)
	}
	if snap.MemTotal == 0 || snap.MemUsed > snap.MemTotal {
		t.Fatalf("bad mem bounds used=%d total=%d", snap.MemUsed, snap.MemTotal)
	}
}

func TestHostCPUPercent(t *testing.T) {
	// 100 process-style % on 8 cores = 12.5% of the machine.
	if got := HostCPUPercent(100, 8); got != 12.5 {
		t.Fatalf("HostCPUPercent(100, 8) = %v, want 12.5", got)
	}
	// Full saturation of every core.
	if got := HostCPUPercent(800, 8); got != 100 {
		t.Fatalf("HostCPUPercent(800, 8) = %v, want 100", got)
	}
	// Overshoot clamps.
	if got := HostCPUPercent(900, 8); got != 100 {
		t.Fatalf("HostCPUPercent clamp = %v, want 100", got)
	}
	if got := HostCPUPercent(50, 0); got != 50 {
		t.Fatalf("HostCPUPercent ncpu=0 fallback = %v, want 50", got)
	}
	if got := HostCPUPercent(-1, 4); got != 0 {
		t.Fatalf("HostCPUPercent negative = %v, want 0", got)
	}
}

func TestHostRAMPercent(t *testing.T) {
	const total = 16 * 1024 * 1024 * 1024
	if got := HostRAMPercent(total/4, total); got != 25 {
		t.Fatalf("HostRAMPercent quarter = %v, want 25", got)
	}
	if got := HostRAMPercent(total*2, total); got != 100 {
		t.Fatalf("HostRAMPercent overshoot = %v, want 100", got)
	}
	if got := HostRAMPercent(100, 0); got != 0 {
		t.Fatalf("HostRAMPercent zero total = %v, want 0", got)
	}
}

func TestHostCPUFromDelta(t *testing.T) {
	// 1.0 CPU-second over 1s on 8 cores = 12.5% of the machine.
	if got := HostCPUFromDelta(1.0, 1.0, 8); got != 12.5 {
		t.Fatalf("HostCPUFromDelta = %v, want 12.5", got)
	}
	// Fully saturate all cores for the window.
	if got := HostCPUFromDelta(8.0, 1.0, 8); got != 100 {
		t.Fatalf("HostCPUFromDelta full = %v, want 100", got)
	}
	if got := HostCPUFromDelta(20.0, 1.0, 8); got != 100 {
		t.Fatalf("HostCPUFromDelta clamp = %v, want 100", got)
	}
	if got := HostCPUFromDelta(1.0, 0, 8); got != 0 {
		t.Fatalf("HostCPUFromDelta zero elapsed = %v, want 0", got)
	}
	if got := HostCPUFromDelta(-1.0, 1.0, 8); got != 0 {
		t.Fatalf("HostCPUFromDelta negative delta = %v, want 0", got)
	}
}

func TestParsePSTime(t *testing.T) {
	cases := map[string]float64{
		"0:00.50":    0.5,
		"1:30.00":    90,
		"37:06.59":   37*60 + 6.59,
		"975:30.99":  975*60 + 30.99,
		"01:02:03":   1*3600 + 2*60 + 3,
		"2-01:00:00": 2*86400 + 3600,
	}
	for in, want := range cases {
		got, err := parsePSTime(in)
		if err != nil {
			t.Fatalf("parsePSTime(%q): %v", in, err)
		}
		if got < want-0.01 || got > want+0.01 {
			t.Fatalf("parsePSTime(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestScaleToHost(t *testing.T) {
	raw := ProcStat{OK: true, PCPU: 200, RSS: 1024}
	scaled := raw.ScaleToHost(4, 4096)
	if scaled.CPUPercent != 50 {
		t.Fatalf("cpu = %v, want 50", scaled.CPUPercent)
	}
	if scaled.RamPercent != 25 {
		t.Fatalf("ram = %v, want 25", scaled.RamPercent)
	}
	if scaled.RSS != 1024 {
		t.Fatalf("rss must stay absolute, got %d", scaled.RSS)
	}
	dead := ProcStat{PCPU: 99, RSS: 1}.ScaleToHost(2, 100)
	if dead.OK || dead.PCPU != 99 {
		t.Fatal("ScaleToHost must leave non-OK stats alone")
	}
}

func TestLogicalCPUs(t *testing.T) {
	if n := LogicalCPUs(); n < 1 {
		t.Fatalf("LogicalCPUs = %d, want >= 1", n)
	}
}
