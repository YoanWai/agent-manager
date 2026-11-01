package mcpserver

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listGroupsArgs struct{}

type createGroupArgs struct {
	Path      string `json:"path" jsonschema:"full group path, slash separated for nesting (e.g. work/payments); every parent except the last segment must already exist"`
	Directory string `json:"directory,omitempty" jsonschema:"default working directory sessions created in this group inherit"`
	Host      string `json:"host,omitempty" jsonschema:"SSH connection to create the group on, the host list_groups reports; omit for this machine"`
}

type deleteGroupArgs struct {
	Path string `json:"path" jsonschema:"full group path to delete, slash separated; groups nested under it go too"`
	Host string `json:"host,omitempty" jsonschema:"SSH connection the group is on, the host list_groups reports; omit for this machine"`
}

type listGroupsOutput struct {
	Groups           []groupRow        `json:"groups"`
	ConnectionErrors []connectionError `json:"connection_errors,omitempty" jsonschema:"SSH connections that could not be refreshed for this list; it is never refused for them"`
}

type moveSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session or terminal id from list_sessions or list_terminals; a host::id moves it on that SSH connection"`
	Group     string `json:"group" jsonschema:"existing group path to file it under, slash separated; an empty string is the root group; call list_groups for the existing ones"`
}

func addGroupTools(server *mcp.Server, noCaller error, sessionID string, sessions sessionCommands, remotes *remoteHosts) {
	addTool(server, noCaller, &mcp.Tool{
		Name: "list_groups",
		Description: "List the groups sessions and terminals are filed under, with each group's default directory, worktree default and session count. " +
			"Call before passing a group to create_session or create_terminal, since a group must already exist. " +
			"Groups on the user's SSH connections follow this machine's with host set; pass that host beside the path.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listGroupsArgs) (*mcp.CallToolResult, listGroupsOutput, error) {
		listed, err := sessions.Groups(sessionID)
		if err != nil {
			return nil, listGroupsOutput{}, err
		}
		text, output, err := remotes.listGroups(ctx, listed)
		if err != nil {
			return nil, listGroupsOutput{}, err
		}
		return textContent(text), output, nil
	})

	addTool(server, noCaller, &mcp.Tool{
		Name: "create_group",
		Description: "Create a group to file related sessions under, so a fleet you spawn stays together in the user's list. " +
			"Call before create_session when the work deserves its own heading and list_groups shows no fitting group. " +
			"Nest with a slash path such as work/payments, whose parent must already exist.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createGroupArgs) (*mcp.CallToolResult, sessioncmd.Group, error) {
		if args.Host != "" {
			created, err := remotes.createGroup(ctx, args.Host, args.Path, args.Directory)
			if err != nil {
				return nil, sessioncmd.Group{}, err
			}
			return textContent("created group " + created.Path + " on " + args.Host), created, nil
		}
		created, err := sessions.CreateGroup(sessionID, args.Path, args.Directory)
		if err != nil {
			return nil, sessioncmd.Group{}, err
		}
		return textContent("created group " + created.Path), created, nil
	})

	addTool(server, noCaller, &mcp.Tool{
		Name: "delete_group",
		Description: "Delete a group once the work filed under it is done, so a fleet does not leave a heading behind in the user's list. " +
			"Groups nested under it go too. Any session still filed there moves to the root group rather than being stopped, " +
			"so this never ends an agent: kill_session or archive_session those first if that is what you mean.",
		Annotations: toolAnnotations(false, true, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteGroupArgs) (*mcp.CallToolResult, sessioncmd.GroupRemoval, error) {
		if args.Host != "" {
			removal, err := remotes.deleteGroup(ctx, args.Host, args.Path)
			if err != nil {
				return nil, sessioncmd.GroupRemoval{}, err
			}
			return textContent(sessioncmd.FormatGroupRemoval(removal) + " on " + args.Host), removal, nil
		}
		removal, err := sessions.DeleteGroup(sessionID, args.Path)
		if err != nil {
			return nil, sessioncmd.GroupRemoval{}, err
		}
		return textContent(sessioncmd.FormatGroupRemoval(removal)), removal, nil
	})

	addTool(server, noCaller, &mcp.Tool{
		Name: "move_session",
		Description: "Move a session or terminal into another group, the way the user's move key does, when the user asks to file or tidy their sessions. " +
			"Terminals nested under a session go with it; a nested terminal moved on its own leaves its session. " +
			"The group must already exist: call list_groups, and create_group to add one. " +
			"Only the filing changes, so a running agent keeps running.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args moveSessionArgs) (*mcp.CallToolResult, sessioncmd.Placement, error) {
		move := func() (sessioncmd.Placement, error) { return sessions.Move(sessionID, args.SessionID, args.Group) }
		if ref, ok := remote.ParseRef(args.SessionID); ok {
			move = func() (sessioncmd.Placement, error) { return remotes.move(ctx, ref, args.Group) }
		}
		placement, err := move()
		if err != nil {
			return nil, sessioncmd.Placement{}, err
		}
		return textContent(sessioncmd.FormatPlacement(placement)), placement, nil
	})
}
