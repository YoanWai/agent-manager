package sysstat

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"testing"
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
	// App = anon - purgeable = 1000 - 100 = 900
	// Used pages = 900 + 2000 + 3000 = 5900 → 5900 * 16384 bytes
	const body = `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               100.
Pages active:                            5000.
Pages inactive:                          4000.
Pages speculative:                        50.
Pages wired down:                       2000.
Pages purgeable:                         100.
Anonymous pages:                        1000.
Pages occupied by compressor:           3000.
`
	used, ok := parseVMStatMemoryUsed(body, 4096)
	if !ok {
		t.Fatal("expected parse ok")
	}
	want := uint64(5900) * 16384
	if used != want {
		t.Fatalf("used=%d want=%d", used, want)
	}
}

func TestParseVMStatMemoryUsedLegacyCompressorLabel(t *testing.T) {
	const body = `Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages wired down:                       10.
Pages purgeable:                         0.
Anonymous pages:                        20.
Pages used by compressor:               5.
`
	used, ok := parseVMStatMemoryUsed(body, 4096)
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
	// Re-parse live vm_stat; allow a little drift between samples.
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		t.Fatal(err)
	}
	want, ok := parseVMStatMemoryUsed(string(out), 0)
	if !ok {
		t.Fatal("vm_stat parse failed")
	}
	delta := float64(snap.MemUsed) - float64(want)
	if delta < 0 {
		delta = -delta
	}
	// 256 MiB of movement between two back-to-back samples is normal
	// under load; anything larger means we are not on the AM formula.
	if delta > 256<<20 {
		t.Fatalf("mem used %d drifted %v from live vm_stat %d", snap.MemUsed, delta, want)
	}
	if snap.MemTotal == 0 || snap.MemUsed > snap.MemTotal {
		t.Fatalf("bad mem bounds used=%d total=%d", snap.MemUsed, snap.MemTotal)
	}
}
