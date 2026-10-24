package ui

import (
	"encoding/json"
	"slices"
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uirailpersist "github.com/YoanWai/agent-manager/internal/ui/rail/persist"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
)

func TestRailStateDecisionPreservesLatestCollapseSnapshot(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"reveal", "rename"} {
		t.Run(scenario, func(t *testing.T) {
			m := buildModel(t)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"initial"}}}, 0, false)
			m.nextEffectCmd()
			m.rail.SetCollapsed("parent", true)
			m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: m.rail.Collapsed()}}, 0, false)
			var expected []string
			if scenario == "rename" {
				m.queueRail(m.rail.RenameGroup("parent", "renamed"), 0, true)
				expected = []string{"renamed"}
			} else {
				m.queueRail(m.rail.RevealGroup("parent/child"), 0, true)
			}
			m.drainEffects(t)
			raw, err := m.services.store.Setting(uirailpersist.CollapsedSetting)
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
	active := m.effects.Main.Active
	m.enqueueEffect(geometryRequest{}, 0, false)
	geometry := m.effects.Main.Pending[0]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed}}, 0, false)
	m.enqueueEffect(uilifecycle.Request{}, 0, false)
	lifecycle := m.effects.Main.Pending[2]
	m.queueRail([]uirail.Mutation{{Kind: uirail.SaveCollapsed, Collapsed: []string{"latest"}}}, 0, true)
	if m.effects.Main.Active != active || len(m.effects.Main.Pending) != 3 || m.effects.Main.Pending[1] != geometry || m.effects.Main.Pending[2] != lifecycle {
		t.Fatal("superseding snapshots changed active work or unrelated job order")
	}
}
