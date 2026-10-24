package runtime

import (
	"os/exec"
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

// A probe of a window tmux does not hold refuses the entry, which is the only
// way the person is told to revive or restart the row.
func TestProbeRefusesAWindowTmuxDropped(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip(tmux.Binary + " not installed")
	}
	st, current := storeWith(t)
	driver, err := tmux.NewWithSocket("amfocusprobe")
	if err != nil {
		t.Fatalf("driver: %v", err)
	}
	if err := (Prober{Store: st, Driver: driver}).Probe(current.ID); err == nil {
		t.Fatal("a window tmux does not hold passed the probe")
	}
}
