//go:build windows

package launch

// tracksConversations: PowerShell has no exec to hand the agent a pid
// recorded before it starts, so a Windows launch records no agent and wires
// no conversation report. A session keeps the conversation it launched with.
const tracksConversations = false

func agentCommand(base string) string {
	return base
}

func asAgentProcess(_ string, _ int64, command string) string {
	return command
}
