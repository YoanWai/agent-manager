//go:build darwin

package sysstat

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/disk"
)

func TestDarwinDiskCapacity(t *testing.T) {
	for _, path := range []string{"/", t.TempDir()} {
		total, available, err := readDarwinDisk(path)
		if err != nil {
			t.Fatalf("readDarwinDisk(%q): %v", path, err)
		}
		usage, err := disk.Usage(path)
		if err != nil {
			t.Fatal(err)
		}
		if total != usage.Total {
			t.Fatalf("total = %d, kernel total = %d", total, usage.Total)
		}
		if available > total {
			t.Fatalf("available = %d of %d", available, total)
		}
		var snap Snapshot
		sampleDisk(&snap, path)
		if !snap.DiskOK || snap.DiskTotal != total || snap.DiskUsed+snap.DiskAvailable != total {
			t.Fatalf("inconsistent disk sample: %+v", snap)
		}
		if want := usedPercent(snap.DiskUsed, total); math.Abs(snap.DiskPercent-want) > 0.01 {
			t.Fatalf("percent = %v, want %v", snap.DiskPercent, want)
		}
		t.Logf("%s: %.2f GB available, %.2f GB physically free", path, float64(available)/1e9, float64(usage.Free)/1e9)
	}
}

func TestDarwinDiskMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")
	if _, _, err := readDarwinDisk(path); err == nil {
		t.Fatal("missing path returned disk capacity")
	}
	var snap Snapshot
	sampleDisk(&snap, path)
	if snap.DiskOK {
		t.Fatal("missing path reported disk OK")
	}
}
