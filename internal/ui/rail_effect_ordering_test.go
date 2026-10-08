package ui

import (
	"encoding/json"
	"slices"
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

func TestRailStateDecisionPreservesLatestCollapseSnapshot(t *testing.T) {
	for _, scenario := range []string{"reveal", "rename"} {
		t.Run(scenario, func(t *testing.T) {
			m := buildModel(t)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"initial"}}}, 0, false)
			m.nextEffectCmd()
			m.rail.SetCollapsed("parent", true)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: m.rail.Collapsed()}}, 0, false)
			var expected []string
			if scenario == "rename" {
				m.applyRailStateDecision(m.rail.RenameGroup("parent", "renamed"))
				expected = []string{"renamed"}
			} else {
				m.applyRailStateDecision(m.rail.RevealGroup("parent/child"))
			}
			m.drainEffects(t)
			raw, err := m.services.store.Setting(collapsedSetting)
			if err != nil {
				t.Fatal(err)
			}
			var stored []string
			if err := json.Unmarshal([]byte(raw), &stored); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(stored, expected) {
				t.Fatalf("latest %s preference overwritten: got %v, want %v", scenario, stored, expected)
			}
		})
	}
}

func TestPrioritizedCollapseSaveKeepsActiveAndUnrelatedJobs(t *testing.T) {
	m := &Model{}
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.nextEffectCmd()
	active := m.effects.active
	m.enqueueEffect(geometryRequest{}, 0, false)
	geometry := m.effects.pending[0]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.enqueueEffect(lifecycleRequest{}, 0, false)
	lifecycle := m.effects.pending[2]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"latest"}}}, 0, true)
	if m.effects.active != active || len(m.effects.pending) != 3 || m.effects.pending[1] != geometry || m.effects.pending[2] != lifecycle {
		t.Fatal("superseding snapshots changed active work or unrelated job order")
	}
}
