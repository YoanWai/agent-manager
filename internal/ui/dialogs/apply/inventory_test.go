package apply

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

// A group rename moves the subtree and mirrors only the stages that
// committed, so a failed default-dir write does not leak the asked value.
func TestRenamedGroupMirrorsOnlyTheStagesThatCommitted(t *testing.T) {
	inv := Inventory{
		Groups:         []string{"web", "web/api", "ops"},
		Sessions:       []store.Session{{ID: "s1", Group: "web/api"}, {ID: "s2", Group: "ops"}},
		GroupPaths:     map[string]string{"web": "/w", "web/api": "/w/api", "ops": "/o"},
		GroupWorktrees: map[string]string{"web": "on"},
		GroupBases:     map[string]string{"web/api": "main"},
	}
	renamed := inv.RenamedGroup("web/api", "web/svc", "/w/svc", "off", "dev",
		Stages{GroupRenamed: true, PathSet: true})
	if renamed.Groups[1] != "web/svc" || renamed.Sessions[0].Group != "web/svc" || renamed.Sessions[1].Group != "ops" {
		t.Fatalf("subtree = %q / %+v", renamed.Groups, renamed.Sessions)
	}
	if got := renamed.GroupPaths["web/svc"]; got != "/w/svc" {
		t.Fatalf("default path = %q, want the resolved one", got)
	}
	// The worktree and base choices follow their group; the asked values
	// are not written because those stages did not commit.
	if _, ok := renamed.GroupWorktrees["web/svc"]; ok {
		t.Fatal("a worktree choice leaked past its failed stage")
	}
	if got := renamed.GroupBases["web/svc"]; got != "main" {
		t.Fatalf("base = %q, want the one the group already had", got)
	}
	if inv.Groups[1] != "web/api" {
		t.Fatal("the caller's inventory was mutated")
	}
}

// A session rename mirrors the name, the branch and a tool change, which
// starts the agent over.
func TestRenamedSessionMirrorsTheCommittedStages(t *testing.T) {
	inv := Inventory{Sessions: []store.Session{
		{ID: "s1", Name: "alpha", Tool: "claude", AgentSessionID: "abc", WorktreeBranch: "old"},
		{ID: "s2", Name: "beta", Tool: "codex"},
	}}
	sessions := inv.RenamedSession("s1", Stages{NameRenamed: true, ToolChanged: true}, "gamma", "new", "shell")
	if sessions[0].Name != "gamma" || sessions[0].Tool != "shell" || sessions[0].AgentSessionID != "" || sessions[0].WorktreeBranch != "new" {
		t.Fatalf("s1 = %+v", sessions[0])
	}
	if sessions[1].Name != "beta" || sessions[1].Tool != "codex" {
		t.Fatalf("another row changed: %+v", sessions[1])
	}
	partial := inv.RenamedSession("s1", Stages{NameRenamed: true}, "delta", "", "shell")
	if partial[0].Tool != "claude" || partial[0].AgentSessionID != "abc" {
		t.Fatalf("a tool change leaked past its failed stage: %+v", partial[0])
	}
}

// SetChoice records a group's own value and drops it back to inherit.
func TestSetChoiceDropsToInherit(t *testing.T) {
	choices := SetChoice(map[string]string{"web": "on"}, "ops", "off")
	if choices["ops"] != "off" {
		t.Fatalf("choices = %+v", choices)
	}
	choices = SetChoice(choices, "ops", "")
	if _, ok := choices["ops"]; ok {
		t.Fatalf("an empty value must drop the entry: %+v", choices)
	}
	if SetChoice(nil, "web", "on")["web"] != "on" {
		t.Fatal("a nil map must take the first choice")
	}
}
