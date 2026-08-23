package sysstat

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func resetHostState(t *testing.T) {
	t.Helper()
	resetHost()
	hostSamplerStart = sync.Once{}
	t.Cleanup(resetHost)
}

func resetHost() {
	hostState.mu.Lock()
	hostState.sample = HostSample{}
	hostState.at = time.Time{}
	hostState.mu.Unlock()
}

// disableHostSampling keeps Sample() from launching the real PowerShell
// probe loop while a test runs.
func disableHostSampling(t *testing.T) {
	t.Helper()
	orig := hostProbed
	hostProbed = func() bool { return false }
	resetHostState(t)
	t.Cleanup(func() { hostProbed = orig })
}

func seedHost(t *testing.T, s HostSample, age time.Duration) {
	t.Helper()
	resetHostState(t)
	hostState.mu.Lock()
	defer hostState.mu.Unlock()
	hostState.sample = s
	hostState.at = time.Now().Add(-age)
}

func TestParseHostSample(t *testing.T) {
	s, err := parseHostSample("5 12 34301874176 17150937088 500203874304 250101936128")
	if err != nil {
		t.Fatalf("valid line: %v", err)
	}
	if s.CPUPercent != 5 || s.NCPU != 12 {
		t.Fatalf("cpu/ncpu = %v %v", s.CPUPercent, s.NCPU)
	}
	if s.MemTotal != 34301874176 || s.MemAvailable != 17150937088 {
		t.Fatalf("mem = %v %v", s.MemTotal, s.MemAvailable)
	}
	if s.DiskTotal != 500203874304 || s.DiskFree != 250101936128 {
		t.Fatalf("disk = %v %v", s.DiskTotal, s.DiskFree)
	}

	s, err = parseHostSample("150 12 300 100 500 100")
	if err != nil {
		t.Fatalf("over-100 cpu should clamp, got %v", err)
	}
	if s.CPUPercent != 100 {
		t.Fatalf("cpu clamp: got %v, want 100", s.CPUPercent)
	}

	for name, line := range map[string]string{
		"too few fields":       "5 12 34301874176 17150937088",
		"too many fields":      "1 2 3 4 5 6 7",
		"non-numeric cpu":      "x 2 3 4 5 6",
		"negative cpu":         "-1 2 3 4 5 6",
		"zero ncpu":            "5 0 3 4 5 6",
		"non-numeric ncpu":     "5 x 3 4 5 6",
		"non-numeric memT":     "5 12 x 4 5 6",
		"non-numeric memA":     "5 12 3 x 5 6",
		"non-numeric diskT":    "5 12 3 4 x 6",
		"non-numeric diskF":    "5 12 3 4 5 x",
		"mem avail over total": "5 12 100 200 500 100",
		"disk free over total": "5 12 300 100 500 900",
	} {
		if _, err := parseHostSample(line); err == nil {
			t.Errorf("%s: want error, got nil (%q)", name, line)
		}
	}
}

func TestOverlayHostFresh(t *testing.T) {
	seedHost(t, HostSample{
		CPUPercent:   7,
		NCPU:         12,
		MemTotal:     32000,
		MemAvailable: 12000,
		DiskTotal:    500000,
		DiskFree:     100000,
	}, 0)

	var snap Snapshot
	overlayHost(&snap)
	if !snap.CPUOK || snap.CPUPercent != 7 {
		t.Fatalf("cpu = %v %v", snap.CPUPercent, snap.CPUOK)
	}
	if !snap.MemOK || snap.MemTotal != 32000 || snap.MemUsed != 20000 {
		t.Fatalf("mem = %v %v %v", snap.MemUsed, snap.MemTotal, snap.MemOK)
	}
	if snap.MemPercent != 62.5 {
		t.Fatalf("mem%% = %v", snap.MemPercent)
	}
	if !snap.DiskOK || snap.DiskTotal != 500000 || snap.DiskUsed != 400000 || snap.DiskFree != 100000 {
		t.Fatalf("disk = %v %v %v %v", snap.DiskUsed, snap.DiskTotal, snap.DiskFree, snap.DiskOK)
	}
	if snap.DiskPercent != 80 {
		t.Fatalf("disk%% = %v", snap.DiskPercent)
	}
}

func TestOverlayHostStaleFallsBackToGuest(t *testing.T) {
	seedHost(t, HostSample{CPUPercent: 99, NCPU: 12, MemTotal: 32000, MemAvailable: 0, DiskTotal: 500000, DiskFree: 0}, 31*time.Second)

	var snap Snapshot
	snap.CPUPercent, snap.CPUOK = 11, true
	snap.MemUsed, snap.MemTotal, snap.MemOK = 100, 16000, true
	overlayHost(&snap)
	if snap.CPUPercent != 11 || snap.MemTotal != 16000 {
		t.Fatalf("stale host sample overwrote guest values: %+v", snap)
	}
}

func TestHostAccessors(t *testing.T) {
	resetHostState(t)

	if _, ok := hostMemTotal(); ok {
		t.Fatal("empty cache should report not-ok")
	}
	if _, ok := hostNCPU(); ok {
		t.Fatal("empty cache should report not-ok")
	}

	seedHost(t, HostSample{CPUPercent: 1, NCPU: 12, MemTotal: 32000, MemAvailable: 100}, 0)
	total, ok := hostMemTotal()
	if !ok || total != 32000 {
		t.Fatalf("hostMemTotal = %v %v", total, ok)
	}
	n, ok := hostNCPU()
	if !ok || n != 12 {
		t.Fatalf("hostNCPU = %v %v", n, ok)
	}
}

func TestEnsureHostSamplerGating(t *testing.T) {
	origProbed, origLookup, origProbe := hostProbed, hostLookupPowerShell, hostProbe
	defer func() { hostProbed, hostLookupPowerShell, hostProbe = origProbed, origLookup, origProbe }()

	probeCalled := make(chan string, 4)
	hostProbe = func(path string, timeout time.Duration) (string, error) {
		probeCalled <- path
		return "", errors.New("unused")
	}

	t.Run("no wsl means no probe", func(t *testing.T) {
		resetHostState(t)
		hostProbed = func() bool { return false }
		startHostSampler()
		select {
		case p := <-probeCalled:
			t.Fatalf("probed without wsl via %q", p)
		case <-time.After(150 * time.Millisecond):
		}
	})

	t.Run("no powershell means no probe", func(t *testing.T) {
		resetHostState(t)
		hostProbed = func() bool { return true }
		hostLookupPowerShell = func() (string, bool) { return "", false }
		startHostSampler()
		select {
		case p := <-probeCalled:
			t.Fatalf("probed without powershell via %q", p)
		case <-time.After(150 * time.Millisecond):
		}
	})

	t.Run("found powershell probes immediately", func(t *testing.T) {
		resetHostState(t)
		hostLookupPowerShell = func() (string, bool) { return "/fake/powershell.exe", true }
		startHostSampler()
		select {
		case p := <-probeCalled:
			if p != "/fake/powershell.exe" {
				t.Fatalf("probe path = %q", p)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("probe never ran")
		}
	})
}

func TestStoreHostReplacesPreviousSample(t *testing.T) {
	resetHostState(t)

	storeHost(HostSample{CPUPercent: 1, NCPU: 4, MemTotal: 8, MemAvailable: 4})
	storeHost(HostSample{CPUPercent: 2, NCPU: 8, MemTotal: 32, MemAvailable: 16})

	hostState.mu.Lock()
	defer hostState.mu.Unlock()
	if hostState.sample.MemTotal != 32 || hostState.sample.NCPU != 8 {
		t.Fatalf("stored sample = %+v", hostState.sample)
	}
}
