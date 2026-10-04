package sessioncmd

// Vocabulary is how one front spells the actions these errors send a caller
// after. Both fronts drive this layer, but an agent holds only one of them:
// a session whose CLI carries no MCP client has the subcommands and nothing
// else, so naming a tool at it points at something it cannot call.
type Vocabulary struct {
	ListSessions   string
	ListTerminals  string
	ListTasks      string
	ListGroups     string
	CreateGroup    string
	CreateTerminal string
	SpawnTool      string
	SpawnModel     string
	SpawnEffort    string
	SpawnProfile   string
	Revive         string
	Restore        string
	Send           string
	Read           string
}

// MCPVocabulary spells the actions as the tools an MCP client sees.
func MCPVocabulary() Vocabulary {
	return Vocabulary{
		ListSessions:   "list_sessions",
		ListTerminals:  "list_terminals",
		ListTasks:      `task with action "list"`,
		ListGroups:     "list_groups",
		CreateGroup:    "create_group",
		CreateTerminal: "create_terminal",
		SpawnTool:      "create_session tool",
		SpawnModel:     "create_session model",
		SpawnEffort:    "create_session effort",
		SpawnProfile:   "create_session profile",
		Revive:         "revive_session",
		Restore:        "archive_session archived false",
		Send:           "send_session",
		Read:           "read_session",
	}
}

// CLIVocabulary spells them as the subcommands an agent runs from its shell.
func CLIVocabulary() Vocabulary {
	return Vocabulary{
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
}
