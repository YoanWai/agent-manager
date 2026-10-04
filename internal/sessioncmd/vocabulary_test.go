package sessioncmd

import "testing"

func TestCLIVocabulary(t *testing.T) {
	want := Vocabulary{
		ListSessions:   "agent-manager sessions",
		ListTerminals:  "agent-manager terminal list",
		ListTasks:      "agent-manager task list",
		ListGroups:     "agent-manager groups",
		CreateGroup:    "agent-manager create-group",
		CreateTerminal: "agent-manager terminal create",
		SpawnTool:      "agent-manager spawn --tool",
		SpawnModel:     "agent-manager spawn --model",
		SpawnEffort:    "agent-manager spawn --effort",
		SpawnProfile:   "agent-manager spawn --profile",
		Revive:         "agent-manager revive",
		Restore:        "agent-manager archive --restore",
		Send:           "agent-manager send",
		Read:           "agent-manager read",
	}
	if got := CLIVocabulary(); got != want {
		t.Fatalf("CLIVocabulary() = %+v, want %+v", got, want)
	}
}
