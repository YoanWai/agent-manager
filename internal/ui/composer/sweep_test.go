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
	cmd := SweepTick()
	if cmd == nil {
		t.Fatal("tick must return work")
	}
	// A manager left open for weeks only keeps sweeping if the tick both
	// sweeps and re-arms, so the batch must carry two commands. Running the
	// timer itself here would wait out the real interval.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("want a batch, got %T", cmd())
	}
	if len(batch) != 2 {
		t.Fatalf("want sweep plus re-arm, got %d commands", len(batch))
	}
}
