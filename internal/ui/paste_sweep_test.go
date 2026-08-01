package ui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSweepPastesReportsSweepError(t *testing.T) {
	orig := sweepStalePastes
	defer func() { sweepStalePastes = orig }()
	sweepStalePastes = func() error { return errors.New("permission denied") }

	m := &Model{}
	msg, ok := m.sweepPastes().(pasteSweepMsg)
	if !ok {
		t.Fatalf("want pasteSweepMsg, got %T", m.sweepPastes())
	}
	if msg.err == nil || msg.err.Error() != "permission denied" {
		t.Fatalf("got %v", msg.err)
	}
}

func TestPasteSweepMsgSurfacesErrorOnce(t *testing.T) {
	m := buildModel(t)
	m.Update(pasteSweepMsg{err: errors.New("permission denied")})
	if m.errBar.text == "" {
		t.Fatal("a failed sweep must reach the user")
	}
	m.errBar.text = ""
	m.Update(pasteSweepMsg{})
	if m.errBar.text != "" {
		t.Fatalf("a clean sweep must stay silent, got %q", m.errBar.text)
	}
}

func TestPasteSweepTickSweepsAgainAndRearms(t *testing.T) {
	m := buildModel(t)
	_, cmd := m.Update(pasteSweepTickMsg{})
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
