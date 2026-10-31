package composer

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSweepReportsSweepError(t *testing.T) {
	orig := SweepStale
	defer func() { SweepStale = orig }()
	SweepStale = func() error { return errors.New("permission denied") }

	msg, ok := Sweep().(SweepMsg)
	if !ok {
		t.Fatalf("want SweepMsg, got %T", Sweep())
	}
	if msg.Err == nil || msg.Err.Error() != "permission denied" {
		t.Fatalf("got %v", msg.Err)
	}
}

func TestSweepTickSweepsAgainAndRearms(t *testing.T) {
	if SweepTick() == nil {
		t.Fatal("tick must return work")
	}
	// A manager left open for weeks only keeps sweeping if the tick both
	// sweeps and re-arms. The route answers the tick without reading root,
	// and running the timer itself would wait out the real interval.
	cmd, handled := Update(nil, SweepTickMsg{})
	if !handled || cmd == nil {
		t.Fatal("the tick went unhandled")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("the tick did not batch a sweep and a re-arm")
	}
	if len(batch) != 2 {
		t.Fatalf("want sweep plus re-arm, got %d commands", len(batch))
	}
}
