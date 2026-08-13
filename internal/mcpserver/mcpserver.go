// Package mcpserver exposes agent-manager's session commands as MCP tools
// over stdio, so any MCP-capable agent discovers and calls them natively.
// The manager registers this server into every session it spawns; the
// session id travels via the AGENT_MANAGER_SESSION_ID environment variable.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type renameArgs struct {
	Name string `json:"name" jsonschema:"short 2-4 word kebab-case name for the broad feature of this whole session, not one subtask"`
}

type reviewRepoArgs struct {
	Path string `json:"path" jsonschema:"absolute path to the git repo or worktree being worked on"`
}

type reviewBaseArgs struct {
	Ref      string `json:"ref" jsonschema:"git ref the review diffs against (e.g. origin/develop); pass \"auto\" to return to auto-detection"`
	RepoPath string `json:"repo_path,omitempty" jsonschema:"path inside the repo the ref belongs to; defaults to the current working directory"`
}

type reviewModeArgs struct {
	Scope string `json:"scope" jsonschema:"diff scope: uncommitted, branch (vs target), last_commit, or staged"`
}

type listTerminalsArgs struct{}

type createTerminalArgs struct {
	Group     *string `json:"group,omitempty" jsonschema:"existing group path for the new terminal; pass an empty string for the root group; defaults to this agent's group"`
	Directory string  `json:"directory,omitempty" jsonschema:"existing directory to open; defaults to the selected group's inherited path, then this agent's current directory"`
}

type sendTerminalArgs struct {
	TerminalID string   `json:"terminal_id" jsonschema:"terminal id returned by list_terminals or create_terminal"`
	Command    string   `json:"command,omitempty" jsonschema:"command text to paste and submit with Enter; provide exactly one of command or keys"`
	Keys       []string `json:"keys,omitempty" jsonschema:"exact tmux key names to send in order, such as C-c, Up, or Enter; provide exactly one of keys or command"`
}

type readTerminalArgs struct {
	TerminalID string `json:"terminal_id" jsonschema:"terminal id returned by list_terminals or create_terminal"`
}

type listSessionsArgs struct{}

type createSessionArgs struct {
	Name      string  `json:"name,omitempty" jsonschema:"kebab-case name for the new session, 2-4 words naming the work it will do (e.g. payments-retry-fix); leave empty only when the task is unknown, and the new agent will name itself"`
	Prompt    string  `json:"prompt,omitempty" jsonschema:"first task to hand the new agent, written as a full instruction; it starts idle when empty"`
	Tool      string  `json:"tool,omitempty" jsonschema:"agent CLI to run, such as claude, codex, opencode, gemini or grok; defaults to the CLI this session runs; call list_sessions to see which are in use"`
	Group     *string `json:"group,omitempty" jsonschema:"existing group path to file the session under; pass an empty string for the root group; defaults to this agent's group; call list_groups for the existing ones"`
	Directory string  `json:"directory,omitempty" jsonschema:"existing directory the session works in; defaults to this agent's own directory, or to the selected group's inherited path when group is set"`
	Worktree  *bool   `json:"worktree,omitempty" jsonschema:"true gives the session its own git worktree and branch off the directory's repo, which is what keeps parallel agents from overwriting each other; omit to inherit the group's default"`
}

type sessionTargetArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions or create_session"`
}

type sendSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions or create_session"`
	Message   string `json:"message" jsonschema:"message typed into that agent's prompt as its next turn; write it as a full instruction, since the other agent does not see this conversation"`
}

type archiveSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions"`
	Archived  *bool  `json:"archived,omitempty" jsonschema:"true archives the session out of the active list, false restores it; defaults to true"`
}

type listGroupsArgs struct{}

type createGroupArgs struct {
	Path      string `json:"path" jsonschema:"full group path, slash separated for nesting (e.g. work/payments); every parent except the last segment must already exist"`
	Directory string `json:"directory,omitempty" jsonschema:"default working directory sessions created in this group inherit"`
}

type listTerminalsOutput struct {
	Terminals []sessioncmd.Terminal `json:"terminals"`
}

type sendTerminalOutput struct {
	TerminalID string `json:"terminal_id"`
	Sent       string `json:"sent" jsonschema:"input kind sent: command or keys"`
}

type listSessionsOutput struct {
	Sessions []sessioncmd.Session `json:"sessions"`
}

type listGroupsOutput struct {
	Groups []sessioncmd.Group `json:"groups"`
}

type messageStatusArgs struct {
	MessageID int64 `json:"message_id" jsonschema:"message id returned by send_session"`
}

type terminalCommands interface {
	List(sessionID string) ([]sessioncmd.Terminal, error)
	Create(sessionID string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error)
	Send(sessionID, terminalID, command string, keys []string) error
	Read(sessionID, terminalID string) (sessioncmd.TerminalScreen, error)
}

type sessionCommands interface {
	List(sessionID string) ([]sessioncmd.Session, error)
	Create(sessionID string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error)
	Send(sessionID, targetID, message string) (sessioncmd.SendResult, error)
	MessageStatus(sessionID string, messageID int64) (sessioncmd.MessageState, error)
	Read(sessionID, targetID string) (sessioncmd.SessionScreen, error)
	Revive(sessionID, targetID string) (sessioncmd.Session, error)
	Kill(sessionID, targetID string) (sessioncmd.Session, error)
	Archive(sessionID, targetID string, archived bool) (sessioncmd.Session, error)
	Groups(sessionID string) ([]sessioncmd.Group, error)
	CreateGroup(sessionID, path, directory string) (sessioncmd.Group, error)
}

const serverInstructions = `Agent Manager runs this conversation in a managed tmux session, beside the user's other agent sessions and terminals. These tools operate that workspace. Use them proactively whenever the conditions below apply. Do not wait for the user to ask.

Delegating to other agents. When a task splits into parts that can run at the same time, or the user asks for parallel work, a second opinion, or another agent, call list_sessions first to see what already runs. Reuse a relevant idle session; otherwise call create_session per part, each with a descriptive name and a prompt stating the whole task, since a new agent cannot see this conversation. A session working in a git repo takes worktree: true so parallel agents never edit the same checkout. Follow the work with read_session, and send_session to answer a question or redirect an agent. Group related spawns with create_group, and archive_session once their work is done. Sessions are long-lived and spend the user's tokens, so create one per real workstream, never one per trivial step.

Shell work that stays visible. Before a long-running, output-heavy or continuously monitored command such as a test suite, build, development server or log tail, call list_terminals. Reuse a relevant running terminal when possible; otherwise call create_terminal in the current group and directory. Send the command with send_terminal, then read_terminal to inspect its screen, again as needed while it runs, and send keys for interactive input or interruption. Short one-shot commands stay in your normal execution path.

Your own session. Call rename once while the session still carries a placeholder name. Call review_repo when you start in a repo or worktree, review_base when you know the branch the work merges into, and review_mode to point the user's review screen at the diff scope you want them to see.

Everything here acts on the user's machine: create_session and create_terminal start real processes, send_terminal executes commands, and kill_session ends a running agent. Treat them with the same care and approval expectations as normal shell execution.`

// NewServer builds the MCP server with every session tool registered.
// Split from Run so tests can connect an in-process client.
func NewServer(configDir, sessionID, version string) *mcp.Server {
	return newServer(configDir, sessionID, version, sessioncmd.NewTerminals(configDir), sessioncmd.NewSessions(configDir))
}

func newServer(configDir, sessionID, version string, terminals terminalCommands, sessions sessionCommands) *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{Name: "agent-manager", Version: version},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)

	mcp.AddTool(server, &mcp.Tool{
		Name: "rename",
		Description: "Rename this session to a short 2-4 word kebab-case name for the broad feature it is about. " +
			"Call once at the start only when the session still has a placeholder name (e.g. claude-a1b2). " +
			"If the session already has a real name, leave it unless the user asks to rename. " +
			"Prefer a broad feature name over a single subtask.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args renameArgs) (*mcp.CallToolResult, any, error) {
		return textResult(sessioncmd.Rename(configDir, sessionID, args.Name))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "review_repo",
		Description: "Declare the git repo or worktree you are actively working in, so the manager's " +
			"review screen opens on it. Call when you start working in a repo or switch to another " +
			"repo or worktree.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reviewRepoArgs) (*mcp.CallToolResult, any, error) {
		return textResult(sessioncmd.ReviewRepo(configDir, sessionID, args.Path))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "review_base",
		Description: "Declare the git ref the manager's review screen diffs your work against " +
			"(the merge target, e.g. origin/develop). Call when you know the branch your work " +
			"will merge into; pass \"auto\" to return to auto-detection.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reviewBaseArgs) (*mcp.CallToolResult, any, error) {
		cwd := args.RepoPath
		if cwd == "" {
			cwd = "."
		}
		ref := args.Ref
		if ref == "auto" {
			ref = ""
		}
		return textResult(sessioncmd.ReviewBase(configDir, sessionID, cwd, ref))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "review_mode",
		Description: "Set the diff scope the manager's review screen shows for this session. " +
			"Valid values: uncommitted (working dir changes), branch (vs target/merge base), " +
			"last_commit (HEAD diff), staged (cached changes). " +
			"Call when you want the review to show a different scope, e.g. switch from " +
			"uncommitted to staged before committing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reviewModeArgs) (*mcp.CallToolResult, any, error) {
		return textResult(sessioncmd.ReviewScope(configDir, sessionID, args.Scope))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_sessions",
		Description: "Call first whenever the work involves another agent: before delegating, before reporting what the fleet is doing, and to find the id of a session to read, prompt, revive, kill or archive. " +
			"Lists every agent session Agent Manager knows with ids, names, CLIs, groups, directories, worktree branches, statuses (starting, working, waiting, finished, idle, errored, dead) and which row is this session. " +
			"Reuse a relevant idle session instead of creating another; otherwise call create_session.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listSessionsArgs) (*mcp.CallToolResult, listSessionsOutput, error) {
		listed, err := sessions.List(sessionID)
		if err != nil {
			return nil, listSessionsOutput{}, err
		}
		return textContent(formatSessionList(listed)), listSessionsOutput{Sessions: listed}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_session",
		Description: "Start another agent CLI in its own Agent Manager session and hand it a task, so independent work runs in parallel with this conversation instead of queued behind it. " +
			"Call it without waiting for the user whenever a task splits into parts that can run at the same time, or the user asks for a second agent, a parallel run or an independent opinion. " +
			"Always pass a descriptive name and a prompt that states the whole task, since the new agent cannot see this conversation. " +
			"Pass worktree true for work in a git repo so the new agent edits its own checkout and branch. " +
			"Returns the new session id; follow it with read_session and send_session. Use create_terminal instead for a plain shell.",
		Annotations: toolAnnotations(false, false, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createSessionArgs) (*mcp.CallToolResult, sessioncmd.Session, error) {
		created, err := sessions.Create(sessionID, sessioncmd.CreateSessionOptions{
			Tool:      args.Tool,
			Name:      args.Name,
			Group:     args.Group,
			Directory: args.Directory,
			Prompt:    args.Prompt,
			Worktree:  args.Worktree,
		})
		if err != nil {
			return nil, sessioncmd.Session{}, err
		}
		return textContent("created " + formatSession(created)), created, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "read_session",
		Description: "Read what another agent's screen currently shows: call it after create_session to confirm the agent started on the task, and again to check progress, read an answer, or see why a session is waiting. " +
			"Returns the plain text visible in that session's pane, which is the current screen rather than its full history. " +
			"A stopped session returns the last screen Agent Manager captured.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sessionTargetArgs) (*mcp.CallToolResult, sessioncmd.SessionScreen, error) {
		screen, err := sessions.Read(sessionID, args.SessionID)
		if err != nil {
			return nil, sessioncmd.SessionScreen{}, err
		}
		text := screen.Output
		if text == "" {
			text = "session screen is empty"
		}
		return textContent(text), screen, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "send_session",
		Description: "Queue a message for another agent, to give a session you spawned its next task, answer a question read_session surfaced, or redirect work going the wrong way. " +
			"The other agent has no view of this conversation, so send a self-contained instruction. " +
			"The message is not typed in the instant you send it: Agent Manager holds it until that agent is at rest, so it can never land on an approval prompt and answer it. " +
			"It arrives labelled as coming from another agent rather than from the user, and cannot approve permissions or change that agent's configuration. " +
			"Returns a message id for message_status; call read_session to see what the agent did with it.",
		Annotations: toolAnnotations(false, false, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendSessionArgs) (*mcp.CallToolResult, sessioncmd.SendResult, error) {
		result, err := sessions.Send(sessionID, args.SessionID, args.Message)
		if err != nil {
			return nil, sessioncmd.SendResult{}, err
		}
		text := fmt.Sprintf("queued message %d for session %s, %d ahead of it", result.MessageID, args.SessionID, result.QueuePosition-1)
		if !result.ManagerAwake {
			text += "; Agent Manager is not running, so it waits until the user opens it"
		}
		return textContent(text), result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "message_status",
		Description: "Check what happened to a message send_session queued: still queued, delivered into the agent's prompt, or answered. " +
			"Call it when you need to know a handoff landed before you build on it, instead of reading the other agent's screen.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args messageStatusArgs) (*mcp.CallToolResult, sessioncmd.MessageState, error) {
		state, err := sessions.MessageStatus(sessionID, args.MessageID)
		if err != nil {
			return nil, sessioncmd.MessageState{}, err
		}
		text := fmt.Sprintf("message %d to session %s is %s", state.MessageID, state.SessionID, state.State)
		if state.DeliveredAt != "" {
			text += " (delivered " + state.DeliveredAt + ")"
		}
		return textContent(text), state, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "revive_session",
		Description: "Bring a dead session back on its old row, resuming the conversation it held where its CLI supports that. " +
			"Call when list_sessions or send_session reports a session is not running and its work should continue.",
		Annotations: toolAnnotations(false, false, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sessionTargetArgs) (*mcp.CallToolResult, sessioncmd.Session, error) {
		revived, err := sessions.Revive(sessionID, args.SessionID)
		if err != nil {
			return nil, sessioncmd.Session{}, err
		}
		return textContent("revived " + formatSession(revived)), revived, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "kill_session",
		Description: "Stop another agent's process, ending whatever it is doing. The row stays with its last screen and can be brought back with revive_session. " +
			"Reserve it for a session whose work is finished or has gone wrong, and prefer send_session to redirect an agent that is still useful. " +
			"Killing interrupts work in progress on the user's machine, so ask first unless the user asked for it.",
		Annotations: toolAnnotations(false, true, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sessionTargetArgs) (*mcp.CallToolResult, sessioncmd.Session, error) {
		killed, err := sessions.Kill(sessionID, args.SessionID)
		if err != nil {
			return nil, sessioncmd.Session{}, err
		}
		return textContent("killed " + formatSession(killed)), killed, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "archive_session",
		Description: "File a finished session out of the active list, or restore an archived one with archived false. " +
			"Use it to keep the user's list readable once a session's work is done; the row and its last screen are kept, and a running pane keeps running.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args archiveSessionArgs) (*mcp.CallToolResult, sessioncmd.Session, error) {
		archived := true
		if args.Archived != nil {
			archived = *args.Archived
		}
		updated, err := sessions.Archive(sessionID, args.SessionID, archived)
		if err != nil {
			return nil, sessioncmd.Session{}, err
		}
		verb := "archived "
		if !archived {
			verb = "restored "
		}
		return textContent(verb + formatSession(updated)), updated, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_groups",
		Description: "List the groups sessions and terminals are filed under, with each group's default directory, worktree default and session count. " +
			"Call before passing a group to create_session or create_terminal, since a group must already exist.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listGroupsArgs) (*mcp.CallToolResult, listGroupsOutput, error) {
		listed, err := sessions.Groups(sessionID)
		if err != nil {
			return nil, listGroupsOutput{}, err
		}
		return textContent(formatGroupList(listed)), listGroupsOutput{Groups: listed}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_group",
		Description: "Create a group to file related sessions under, so a fleet you spawn stays together in the user's list. " +
			"Call before create_session when the work deserves its own heading and list_groups shows no fitting group. " +
			"Nest with a slash path such as work/payments, whose parent must already exist.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createGroupArgs) (*mcp.CallToolResult, sessioncmd.Group, error) {
		created, err := sessions.CreateGroup(sessionID, args.Path, args.Directory)
		if err != nil {
			return nil, sessioncmd.Group{}, err
		}
		return textContent("created group " + created.Path), created, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_terminals",
		Description: "Call proactively before long-running, output-heavy, persistent, or parallel shell work to find a terminal you can reuse. " +
			"Lists active managed terminals with ids, names, groups, current directories, statuses, and whether their tmux panes are running. " +
			"Reuse a relevant running terminal when possible; otherwise call create_terminal. Use the returned id with send_terminal and read_terminal.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listTerminalsArgs) (*mcp.CallToolResult, listTerminalsOutput, error) {
		listed, err := terminals.List(sessionID)
		if err != nil {
			return nil, listTerminalsOutput{}, err
		}
		output := listTerminalsOutput{Terminals: listed}
		return textContent(formatTerminalList(listed)), output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_terminal",
		Description: "After list_terminals finds no relevant running terminal, call this without waiting for the user to create one for long-running, output-heavy, persistent, or parallel shell work. " +
			"Returns its id and opens by default in this agent's group and current directory. Set group to use another existing group and its inherited directory, or set directory explicitly. " +
			"Then call send_terminal with the returned id. Use create_session instead to start another agent CLI rather than a shell.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createTerminalArgs) (*mcp.CallToolResult, sessioncmd.Terminal, error) {
		created, err := terminals.Create(sessionID, sessioncmd.CreateTerminalOptions{
			Group:     args.Group,
			Directory: args.Directory,
		})
		if err != nil {
			return nil, sessioncmd.Terminal{}, err
		}
		return textContent("created " + formatTerminal(created)), created, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "send_terminal",
		Description: "Call after list_terminals or create_terminal to run or control work in a managed terminal, keeping it visible and separate from the conversation. Provide exactly one of command or keys. " +
			"A command is pasted and submitted with Enter, so it executes on the user's machine. " +
			"Keys sends exact tmux key names for interactive control, such as [\"C-c\"] or [\"Up\", \"Enter\"]. Call read_terminal after sending to inspect the result.",
		Annotations: toolAnnotations(false, true, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendTerminalArgs) (*mcp.CallToolResult, sendTerminalOutput, error) {
		if err := terminals.Send(sessionID, args.TerminalID, args.Command, args.Keys); err != nil {
			return nil, sendTerminalOutput{}, err
		}
		sent := "keys"
		if strings.TrimSpace(args.Command) != "" {
			sent = "command"
		}
		output := sendTerminalOutput{TerminalID: args.TerminalID, Sent: sent}
		return textContent(fmt.Sprintf("sent %s to terminal %s", sent, args.TerminalID)), output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "read_terminal",
		Description: "Call immediately after send_terminal to inspect the result, and call again as needed to monitor ongoing work. " +
			"Returns the plain-text content currently visible in the managed terminal pane. This is the current screen, not the pane's full scrollback history.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args readTerminalArgs) (*mcp.CallToolResult, sessioncmd.TerminalScreen, error) {
		screen, err := terminals.Read(sessionID, args.TerminalID)
		if err != nil {
			return nil, sessioncmd.TerminalScreen{}, err
		}
		text := screen.Output
		if text == "" {
			text = "terminal screen is empty"
		}
		return textContent(text), screen, nil
	})

	return server
}

func toolAnnotations(readOnly, destructive, openWorld bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  readOnly,
		OpenWorldHint:   &openWorld,
	}
}

func groupLabel(group string) string {
	if group == "" {
		return "root"
	}
	return group
}

func formatTerminal(terminal sessioncmd.Terminal) string {
	return fmt.Sprintf("%s (%s) in %s at %s", terminal.Name, terminal.ID, groupLabel(terminal.Group), terminal.Directory)
}

func formatTerminalList(terminals []sessioncmd.Terminal) string {
	if len(terminals) == 0 {
		return "no managed terminals"
	}
	lines := make([]string, 0, len(terminals))
	for _, terminal := range terminals {
		lines = append(lines, fmt.Sprintf("- %s; status=%s; running=%t", formatTerminal(terminal), terminal.Status, terminal.Running))
	}
	return strings.Join(lines, "\n")
}

func formatSession(session sessioncmd.Session) string {
	line := fmt.Sprintf("%s (%s) running %s in %s at %s", session.Name, session.ID, session.Tool, groupLabel(session.Group), session.Directory)
	if session.Branch != "" {
		line += " on branch " + session.Branch
	}
	return line
}

func formatSessionList(sessions []sessioncmd.Session) string {
	if len(sessions) == 0 {
		return "no agent sessions"
	}
	lines := make([]string, 0, len(sessions))
	for _, session := range sessions {
		line := fmt.Sprintf("- %s; status=%s; running=%t", formatSession(session), session.Status, session.Running)
		if session.Archived {
			line += "; archived"
		}
		if session.Self {
			line += "; this session"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func formatGroupList(groups []sessioncmd.Group) string {
	if len(groups) == 0 {
		return "no groups; sessions live in the root"
	}
	lines := make([]string, 0, len(groups))
	for _, group := range groups {
		line := fmt.Sprintf("- %s; sessions=%d", group.Path, group.Sessions)
		if group.Directory != "" {
			line += "; directory=" + group.Directory
		}
		if group.Worktree != "" {
			line += "; worktree=" + group.Worktree
		}
		if group.Archived {
			line += "; archived"
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func textContent(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

func textResult(message string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}, nil, nil
}

// Run serves MCP over stdio until the client closes the connection. A
// client that drops the pipe without the shutdown handshake surfaces as
// EOF, which is a normal exit, not a failure.
func Run(configDir, sessionID, version string) error {
	err := NewServer(configDir, sessionID, version).Run(context.Background(), &mcp.StdioTransport{})
	// The SDK reports an abrupt pipe close as an internal "server is
	// closing" wire error that wraps EOF without errors.Is support.
	if err != nil && (errors.Is(err, io.EOF) || strings.Contains(err.Error(), "server is closing")) {
		return nil
	}
	return err
}
