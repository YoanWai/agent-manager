//go:build !windows

package launch

import (
	"github.com/YoanWai/agent-manager/internal/conversation"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// tracksConversations: the agent runs under a pid its launch records, so
// it can report the conversations it switches to.
const tracksConversations = true

// agentCommand starts the agent with exec, so it takes over the pid
// asAgentProcess exports and records.
func agentCommand(base string) string {
	return "exec " + base
}

// asAgentProcess runs a launch line, whose agent is started with exec, in a
// shell of its own that exports and records its pid first. The agent then
// runs under that pid, whichever shell the pane holds and however deeply it
// is nested. The shell that started it marks the record once it has quit.
func asAgentProcess(agentFile string, launch int64, command string) string {
	agent := "export " + hooks.EnvAgentPID + "=$$; " + conversation.RecordCommand(agentFile, launch) + "; " + command
	return "sh -c " + tmux.ShellQuote(agent) + "; " + conversation.EndedCommand(agentFile)
}
