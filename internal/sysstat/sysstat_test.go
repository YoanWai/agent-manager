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

func TestProcSelf(t *testing.T) {
	stat := Proc(os.Getpid())
	if !stat.OK {
		t.Fatal("expected OK stat for current process")
	}
	if stat.RSS == 0 {
		t.Fatal("expected non-zero RSS for current process")
	}
}

func TestProcInvalid(t *testing.T) {
	if Proc(-1).OK {
		t.Fatal("negative pid should not be OK")
	}
	if Proc(0).OK {
		t.Fatal("zero pid should not be OK")
	}
}
