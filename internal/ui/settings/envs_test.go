package settings

import (
	"strings"
	"testing"
)

func TestEnvSummaryNamesKeysAndNeverValues(t *testing.T) {
	if got := envSummary(nil); !strings.Contains(got, "agent-manager env set") {
		t.Fatalf("empty summary = %q, want the command that defines one", got)
	}
	got := envSummary([]Env{{Name: "team"}, {Name: "work", Keys: []string{"CLAUDE_CONFIG_DIR", "KUBECONFIG"}}})
	if want := "team · work (CLAUDE_CONFIG_DIR KUBECONFIG)"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}
