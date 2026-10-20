package settings

import "testing"

func TestEnvSummaryNamesKeysAndNeverValues(t *testing.T) {
	if got := envSummary(nil); got != "none · defined from a shell" {
		t.Fatalf("empty summary = %q, want it to say profiles come from a shell", got)
	}
	got := envSummary([]Env{{Name: "team"}, {Name: "work", Keys: []string{"CLAUDE_CONFIG_DIR", "KUBECONFIG"}}})
	if want := "team · work (CLAUDE_CONFIG_DIR KUBECONFIG)"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
