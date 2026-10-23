package ui

import (
	"strings"
	"testing"
)

// Picking a model keeps it in the cache at once and writes the store only
// from the effect lane.
func TestChoicePickDefersTheStoreWrite(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	_, _ = m.form.Choice().Launch(m.spawnHost(), "claude", "opus")
	if got := m.settings.cache.value("choice.claude"); !strings.Contains(got, `"Model":"opus"`) {
		t.Fatalf("cached choice = %s, want opus", got)
	}
	if raw, err := m.services.store.Setting("choice.claude"); err != nil || raw != "" {
		t.Fatalf("the pick wrote the store on the update path: %q %v", raw, err)
	}
	m.drainEffects(t)
	raw, err := m.services.store.Setting("choice.claude")
	if err != nil || raw == "" {
		t.Fatalf("the effect lane did not persist the pick: %q %v", raw, err)
	}
}
