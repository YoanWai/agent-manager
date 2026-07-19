package sysstat

import (
	"os"
	"testing"
)

func TestSample(t *testing.T) {
	snap := Sample("/")
	if snap.MemOK && snap.MemTotal == 0 {
		t.Fatal("mem reported OK but total is zero")
	}
	if snap.DiskOK && snap.DiskTotal == 0 {
		t.Fatal("disk reported OK but total is zero")
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
