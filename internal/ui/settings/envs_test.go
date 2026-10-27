package settings

import (
	"strings"
	"testing"
)

func TestEnvSummaryNamesKeysAndNeverValues(t *testing.T) {
	got := envSummary([]Env{{Name: "team"}, {Name: "work", Keys: []string{"CLAUDE_CONFIG_DIR", "KUBECONFIG"}}})
	if want := "team · work (CLAUDE_CONFIG_DIR KUBECONFIG)"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
}

// The environments row waits for a profile to list: without one it is not
// painted, and the cursor steps over it both ways.
func TestEnvironmentsRowWaitsForAProfile(t *testing.T) {
	ctx := testContext()
	s := openState(ctx)
	view := viewContext()
	if body := s.View(view).Body; strings.Contains(body, "environments") {
		t.Fatalf("an environments row with no profile:\n%s", body)
	}
	s.Field = FieldPrompts
	if s.Key(key("down"), ctx); s.Field != FieldSetup {
		t.Fatalf("down from saved prompts landed on %d, want the setup", s.Field)
	}
	if s.Key(key("up"), ctx); s.Field != FieldPrompts {
		t.Fatalf("up from the setup landed on %d, want saved prompts", s.Field)
	}
	view.Envs = []Env{{Name: "work"}}
	if body := s.View(view).Body; !strings.Contains(body, "environments") {
		t.Fatalf("no environments row with a profile:\n%s", body)
	}
	if s.Key(key("down"), ctx); s.Field != FieldEnvs {
		t.Fatalf("down from saved prompts landed on %d, want environments", s.Field)
	}
}
