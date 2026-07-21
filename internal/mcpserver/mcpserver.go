// Package mcpserver exposes agent-manager's session commands as MCP tools
// over stdio, so any MCP-capable agent discovers and calls them natively.
// The manager registers this server into every session it spawns; the
// session id travels via the AGENT_MANAGER_SESSION_ID environment variable.
package mcpserver

import (
	"context"
	"errors"
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

// NewServer builds the MCP server with every session tool registered.
// Split from Run so tests can connect an in-process client.
func NewServer(configDir, sessionID, version string) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "agent-manager", Version: version}, nil)

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

	return server
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
