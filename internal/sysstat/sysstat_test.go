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
