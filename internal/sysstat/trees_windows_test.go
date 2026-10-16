//go:build windows

package sysstat

import (
	"os"
	"testing"
)

// The idle process is in every snapshot and its creation time cannot be
// read, so it stands for a root that is alive but not sampleable.
func TestTreesLeavesOutARootItCannotSample(t *testing.T) {
	stats := Trees([]int{0, os.Getpid()})
	if stat, ok := stats[0]; ok {
		t.Fatalf("unsampleable root got an entry: %+v", stat)
	}
	self, ok := stats[os.Getpid()]
	if !ok || !self.OK || self.Procs < 1 {
		t.Fatalf("own process = %+v, ok %v", self, ok)
	}
}
