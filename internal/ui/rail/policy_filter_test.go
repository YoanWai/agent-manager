package rail

import "testing"

func TestAttentionFilterMatchesOnlyActionableStatuses(t *testing.T) {
	want := map[string]bool{
		"waiting": true, "finished": true, "errored": true,
		"working": false, "idle": false, "dead": false, "starting": false,
	}
	for state, keep := range want {
		if got := attentionStatus(state); got != keep {
			t.Fatalf("attentionStatus(%q) = %v want %v", state, got, keep)
		}
	}
}

func TestAttentionFilterTogglesBackToAll(t *testing.T) {
	model := New(nil)
	model.SetFilteringAttention(true)
	if !model.FilteringAttention() {
		t.Fatal("attention filter did not become active")
	}
	model.SetFilteringAttention(false)
	if model.FilteringAttention() {
		t.Fatal("attention filter did not return to all")
	}
}
