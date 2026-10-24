package connections

import (
	"testing"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
)

func TestRowFindsAgentsAndTerminals(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	c.Refreshed("box", c.Gen(), Host{
		OK:        true,
		Sessions:  []Session{{ID: "s1", Name: "api", Group: "web", Status: "idle"}},
		Terminals: []Session{{ID: "t1", Name: "sh", Group: "web", Archived: true}},
	})
	agent, ok := c.Row(Target{Kind: SessionRow, Host: "box", ID: "s1"})
	if !ok || agent.Terminal || agent.Ref != (remote.Ref{Host: "box", ID: "s1"}) || agent.Name != "api" || agent.Group != "web" {
		t.Fatalf("agent row = %+v, %v", agent, ok)
	}
	term, ok := c.Row(Target{Kind: SessionRow, Host: "box", ID: "t1"})
	if !ok || !term.Terminal || !term.Archived {
		t.Fatalf("terminal row = %+v, %v", term, ok)
	}
	if _, ok := c.Row(Target{Kind: SessionRow, Host: "box", ID: "gone"}); ok {
		t.Fatal("a row the snapshot does not list was found")
	}
	if group, ok := c.Row(Target{Kind: GroupRow, Host: "box", Group: "web"}); !ok || group.Kind != GroupRow || group.Group != "web" {
		t.Fatalf("group row = %+v, %v", group, ok)
	}
}

func TestTroubleLabels(t *testing.T) {
	for trouble, want := range map[remote.Trouble]string{
		remote.TroubleUnreachable: "unreachable",
		remote.TroubleMissing:     "agent-manager missing",
		remote.TroubleOutdated:    "agent-manager too old",
		remote.TroubleNewer:       "agent-manager newer",
		remote.TroubleFailed:      "error",
	} {
		if got := TroubleLabel(trouble); got != want {
			t.Errorf("TroubleLabel(%d) = %q, want %q", trouble, got, want)
		}
	}
}
