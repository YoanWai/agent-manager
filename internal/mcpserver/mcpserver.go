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

type listTerminalsOutput struct {
	Terminals []sessioncmd.Terminal `json:"terminals"`
}

type sendTerminalOutput struct {
	TerminalID string `json:"terminal_id"`
	Sent       string `json:"sent" jsonschema:"input kind sent: command or keys"`
}

type terminalCommands interface {
	List(sessionID string) ([]sessioncmd.Terminal, error)
	Create(sessionID string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error)
	Send(sessionID, terminalID, command string, keys []string) error
	Read(sessionID, terminalID string) (sessioncmd.TerminalScreen, error)
}

const serverInstructions = `Use Agent Manager's terminal tools proactively for task-related shell work that should remain visible, persistent, or run alongside the conversation. Do not wait for the user to ask when these conditions apply.

Before starting a long-running, output-heavy, or continuously monitored command such as a test suite, build, development server, or log tail, call list_terminals. Reuse a relevant running terminal when possible; otherwise call create_terminal in the current group and directory. Use send_terminal to submit the command, then call read_terminal to inspect its screen. Read again as needed while the command is running, and use send_terminal keys for interactive input or interruption.

Do not create a new terminal for every short one-shot command when persistence or separate visibility adds no value. Sending a terminal command executes on the user's machine and follows the same safety and approval expectations as normal shell execution.`

// NewServer builds the MCP server with every session tool registered.
// Split from Run so tests can connect an in-process client.
func NewServer(configDir, sessionID, version string) *mcp.Server {
	return newServer(configDir, sessionID, version, sessioncmd.NewTerminals(configDir))
}

func newServer(configDir, sessionID, version string, terminals terminalCommands) *mcp.Server {
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
		Name: "list_terminals",
		Description: "Call proactively before long-running, output-heavy, persistent, or parallel shell work to find a terminal you can reuse. " +
			"Lists active managed terminals with ids, names, groups, current directories, statuses, and whether their tmux panes are running. " +
			"Reuse a relevant running terminal when possible; otherwise call create_terminal. Use the returned id with send_terminal and read_terminal.",
		Annotations: terminalToolAnnotations(true, false, false),
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
			"Then call send_terminal with the returned id.",
		Annotations: terminalToolAnnotations(false, false, false),
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
		Annotations: terminalToolAnnotations(false, true, true),
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
		Annotations: terminalToolAnnotations(true, false, false),
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

func terminalToolAnnotations(readOnly, destructive, openWorld bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		DestructiveHint: &destructive,
		IdempotentHint:  readOnly,
		OpenWorldHint:   &openWorld,
	}
}

func formatTerminal(terminal sessioncmd.Terminal) string {
	group := terminal.Group
	if group == "" {
		group = "root"
	}
	return fmt.Sprintf("%s (%s) in %s at %s", terminal.Name, terminal.ID, group, terminal.Directory)
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
