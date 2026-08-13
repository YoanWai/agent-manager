// Package launch assembles the shell command and environment a managed
// session starts with. The TUI and the MCP session tools both spawn
// sessions, from different processes, and a session spawned either way
// must carry the same prompt handling, session-id flags, hook wiring and
// MCP registration.
package launch

import (
	"os"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

// RenameDirective asks the agent, as the first line of its first prompt,
// to name its own session via the rename tool. Injected only for
// auto-named sessions that launch with a prompt, so it fires exactly once.
const RenameDirective = `First, run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name for the broad feature or theme of this whole session (not one subtask of a larger feature): agent-manager rename "<name>". Run rename only this once. Do not rename again later in the conversation unless the user explicitly asks you to rename; if they do, pick a broad name from context, not a narrow step. Then do the task:`

// DeferredRenameDirective is the standalone message sent into sessions
// whose first prompt could not carry the directive: slash-command
// prompts (the command must open the message) and promptless launches.
const DeferredRenameDirective = `Run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name for the broad feature or theme of this whole session (not one subtask of a larger feature): agent-manager rename "<name>". Run rename only this once. Do not rename again later in the conversation unless the user explicitly asks you to rename; if they do, pick a broad name from context, not a narrow step. Then continue.`

// RenameAvailableNote tells a custom-named session that rename exists for
// later use without asking it to rename now.
const RenameAvailableNote = `This session is already named. You can rename it later with agent-manager rename "<name>" only if the user asks. Do not rename it now. Then do the task:`

// DirectiveEmbeddable reports whether a launch note can ride the
// session's first prompt; otherwise auto-named sessions get the rename
// directive later as its own message.
func DirectiveEmbeddable(prompt string) bool {
	return prompt != "" && !strings.HasPrefix(prompt, "/")
}

// Prompt prepends a short agent note when the first prompt can carry one:
// auto-named sessions must rename once; custom-named sessions only learn
// that rename is available later.
func Prompt(prompt string, autoNamed bool) string {
	if !DirectiveEmbeddable(prompt) {
		return prompt
	}
	if autoNamed {
		return RenameDirective + "\n\n" + prompt
	}
	return RenameAvailableNote + "\n\n" + prompt
}

// WithPrompt appends the first prompt to a tool's command, using the
// tool's prompt flag when it has one. Tools that take their prompt as
// typed input instead keep the bare command.
func WithPrompt(tool config.Tool, command, prompt string) string {
	if prompt == "" || tool.PromptMode == "send" {
		return command
	}
	if tool.PromptFlag != "" {
		return command + " " + tool.PromptFlag + " " + tmux.ShellQuote(prompt)
	}
	return command + " " + tmux.ShellQuote(prompt)
}

// Plan is everything a spawn needs beyond the store row: the command to
// run, the inputs the poller types in once the agent is up, and the
// conversation id a later revive resumes.
type Plan struct {
	Command        string
	PendingInputs  []string
	AgentSessionID string
}

// Assemble resolves a session's first prompt into a launch plan. A prompt
// rides the command line when the tool takes one, and is typed into the
// pane when the tool's prompt mode is "send". Tools that accept a chosen
// session id launch with one, so a later revive resumes this exact
// conversation rather than the directory's most recent one; tools without
// the flag mint their own id, captured after launch by the poller.
func Assemble(tool config.Tool, rawPrompt string, autoNamed bool) Plan {
	deferDirective := autoNamed && !DirectiveEmbeddable(rawPrompt)
	prompt := Prompt(rawPrompt, autoNamed)
	plan := Plan{Command: WithPrompt(tool, tool.Command, prompt)}
	if tool.PromptMode == "send" && prompt != "" {
		plan.PendingInputs = append(plan.PendingInputs, prompt)
	}
	if deferDirective {
		plan.PendingInputs = append(plan.PendingInputs, DeferredRenameDirective)
	}
	if tool.SessionIDFlag != "" {
		plan.AgentSessionID = uuid.NewString()
		plan.Command += " " + tool.SessionIDFlag + " " + plan.AgentSessionID
	}
	return plan
}

// ReviveCommand is the base command a dead session comes back on. When
// its own conversation id was captured, it resumes that exact
// conversation instead of the working directory's most recent one, which
// would be the wrong conversation whenever sessions share a cwd.
func ReviveCommand(tool config.Tool, agentSessionID string) string {
	if agentSessionID != "" && tool.ResumeByIDCommand != "" {
		return strings.ReplaceAll(tool.ResumeByIDCommand, "{id}", agentSessionID)
	}
	if tool.ReviveCommand != "" {
		return tool.ReviveCommand
	}
	return tool.Command
}

// Environment resolves the shell command and environment a session
// launches with. Every session carries its id so the rename subcommand
// can find it; tools backed by hooks additionally get the generated
// settings file and their status-file path, plus a clean slate from any
// earlier files under the same id.
func Environment(manager *hooks.Manager, toolName string, tool config.Tool, baseCommand, id string) (string, map[string]string, error) {
	if err := manager.RemoveName(id); err != nil {
		return "", nil, err
	}
	env := map[string]string{hooks.EnvSessionID: id}
	command, err := mcpreg.Apply(mcpreg.Style(toolName, tool.MCP), Executable(), manager.Dir(), baseCommand, env)
	if err != nil {
		return "", nil, err
	}
	if tool.StatusSource != hooks.StatusSourceClaude {
		return command, env, nil
	}
	settingsPath, err := manager.EnsureSettings()
	if err != nil {
		return "", nil, err
	}
	if err := manager.Remove(id); err != nil {
		return "", nil, err
	}
	env[hooks.EnvStatusFile] = manager.StatusFile(id)
	return command + " --settings " + tmux.ShellQuote(settingsPath), env, nil
}

// Executable names the binary generated MCP configs point at: the
// running manager itself, falling back to the PATH-resolved name.
func Executable() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "agent-manager"
}
