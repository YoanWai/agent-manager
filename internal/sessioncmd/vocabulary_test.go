package sessioncmd

import "testing"

func TestCLIVocabulary(t *testing.T) {
	want := Vocabulary{
		ListSessions:   "agent-manager sessions",
		ListTerminals:  "agent-manager terminal list",
		ListBrowsers:   "agent-manager browser list",
		ListTasks:      "agent-manager task list",
		ListGroups:     "agent-manager groups",
		CreateGroup:    "agent-manager create-group",
		CreateTerminal: "agent-manager terminal create",
		CreateBrowser:  "agent-manager browser create",
		SpawnTool:      "agent-manager spawn --tool",
		SpawnModel:     "agent-manager spawn --model",
		SpawnEffort:    "agent-manager spawn --effort",
		SpawnProfile:   "agent-manager spawn --profile",
		Revive:         "agent-manager revive",
		Restore:        "agent-manager archive --restore",
		Send:           "agent-manager send",
		Read:           "agent-manager read",
		ArchiveSelf:    "agent-manager archive-self",
		KillSelf:       "agent-manager kill-self",
	}
	if got := CLIVocabulary(); got != want {
		t.Fatalf("CLIVocabulary() = %+v, want %+v", got, want)
	}
}

func TestMCPVocabulary(t *testing.T) {
	want := Vocabulary{
		ListSessions:   "list_sessions",
		ListTerminals:  "list_terminals",
		ListBrowsers:   "list_browsers",
		ListTasks:      `task with action "list"`,
		ListGroups:     "list_groups",
		CreateGroup:    "create_group",
		CreateTerminal: "create_terminal",
		CreateBrowser:  "create_browser",
		SpawnTool:      "create_session tool",
		SpawnModel:     "create_session model",
		SpawnEffort:    "create_session effort",
		SpawnProfile:   "create_session profile",
		Revive:         "revive_session",
		Restore:        "archive_session archived false",
		Send:           "send_session",
		Read:           "read_session",
		ArchiveSelf:    "archive_self",
		KillSelf:       "kill_self",
	}
	if got := MCPVocabulary(); got != want {
		t.Fatalf("MCPVocabulary() = %+v, want %+v", got, want)
	}
}
