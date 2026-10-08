package ui

import "testing"

// Picking a model keeps it in the cache at once and writes the store only
// from the effect lane.
func TestChoicePickDefersTheStoreWrite(t *testing.T) {
	m := buildModel(t)
	answered(m, claudeLike, claudeAnswer)
	openFormOnClaude(t, m)
	m.form.choice.pickModel(m, "claude", "opus")
	if got := savedChoice(m, "claude"); got == nil || got.Model != "opus" {
		t.Fatalf("cached choice = %+v, want opus", got)
	}
	if raw, err := m.services.store.Setting(savedChoiceKey("claude")); err != nil || raw != "" {
		t.Fatalf("the pick wrote the store on the update path: %q %v", raw, err)
	}
	m.drainEffects(t)
	raw, err := m.services.store.Setting(savedChoiceKey("claude"))
	if err != nil || raw == "" {
		t.Fatalf("the effect lane did not persist the pick: %q %v", raw, err)
	}
}
