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
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type renameArgs struct {
	Name string `json:"name" jsonschema:"short 2-4 word kebab-case name for the broad feature of this whole session, not one subtask"`
}

type reviewArgs struct {
	Repo string `json:"repo,omitempty" jsonschema:"path inside the git repo or worktree being worked on, so review opens there"`
	Base string `json:"base,omitempty" jsonschema:"git ref the branch scope diffs against (e.g. origin/develop); \"auto\" returns to auto-detection"`
	Mode string `json:"mode,omitempty" jsonschema:"diff scope review opens with: uncommitted, branch, last_commit or staged"`
}

type reviewCommentArgs struct {
	CommentID string `json:"comment_id" jsonschema:"stable comment id included in the review prompt"`
	Handled   *bool  `json:"handled,omitempty" jsonschema:"true or omitted after addressing the point; false reopens it"`
}

type listTerminalsArgs struct{}

type createTerminalArgs struct {
	Group     *string `json:"group,omitempty" jsonschema:"existing group path for the new terminal; pass an empty string for the root group; defaults to this agent's group"`
	Directory string  `json:"directory,omitempty" jsonschema:"existing directory to open; defaults to the selected group's inherited path, then this agent's current directory"`
	Nest      *bool   `json:"nest,omitempty" jsonschema:"when true or omitted, nest under this session, or beside it under the same parent when this session is itself a terminal; false places an un-nested terminal in group"`
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

type taskArgs struct {
	Action    string   `json:"action" jsonschema:"list, create, claim, finish, release or delete"`
	TaskID    string   `json:"task_id,omitempty" jsonschema:"task to act on; omit on claim to take the oldest pending task nothing is blocking"`
	Title     string   `json:"title,omitempty" jsonschema:"create: one-line summary of the work, specific enough that another agent knows what done looks like"`
	Body      string   `json:"body,omitempty" jsonschema:"create: full instruction for whoever claims it; it cannot see this conversation"`
	DependsOn []string `json:"depends_on,omitempty" jsonschema:"create: ids of tasks that must be done first; a task with unfinished dependencies cannot be claimed"`
}

type reserveFilesArgs struct {
	Paths []string `json:"paths" jsonschema:"files or globs you are about to edit, such as internal/store/*.go"`
	Mode  string   `json:"mode,omitempty" jsonschema:"exclusive (default) means nobody else should edit these paths; shared means others may edit them too"`
	Note  string   `json:"note,omitempty" jsonschema:"what you are doing there, so a conflicting agent knows what it is up against"`
	TTLM  int      `json:"ttl_minutes,omitempty" jsonschema:"minutes before the lease lapses on its own, default 30, maximum 240"`
}

type releaseFilesArgs struct {
	Paths []string `json:"paths,omitempty" jsonschema:"patterns to release; omit to release every lease this session holds"`
}

type listReservationsArgs struct{}

type listGroupsArgs struct{}

type createGroupArgs struct {
	Path      string `json:"path" jsonschema:"full group path, slash separated for nesting (e.g. work/payments); every parent except the last segment must already exist"`
	Directory string `json:"directory,omitempty" jsonschema:"default working directory sessions created in this group inherit"`
}

type deleteGroupArgs struct {
	Path string `json:"path" jsonschema:"full group path to delete, slash separated; groups nested under it go too"`
}

type closeTerminalArgs struct {
	TerminalID string `json:"terminal_id" jsonschema:"terminal id returned by list_terminals or create_terminal"`
}

type listTerminalsOutput struct {
	Terminals []sessioncmd.Terminal `json:"terminals"`
}

type listSessionsOutput struct {
	Sessions []sessioncmd.Session `json:"sessions"`
}

type taskOutput struct {
	Tasks []sessioncmd.Task `json:"tasks,omitempty" jsonschema:"the whole shared list, for action list"`
	Task  *sessioncmd.Task  `json:"task,omitempty" jsonschema:"the task acted on"`
}

type listReservationsOutput struct {
	Reservations []sessioncmd.Reservation `json:"reservations"`
}

type listGroupsOutput struct {
	Groups []sessioncmd.Group `json:"groups"`
}

type releaseFilesOutput struct {
	Released int `json:"released"`
}

type waitSessionArgs struct {
	SessionID string   `json:"session_id" jsonschema:"session id returned by list_sessions or create_session"`
	Until     []string `json:"until,omitempty" jsonschema:"states that end the wait; defaults to every state that means the session stopped working (finished, waiting, idle, errored, dead). Valid values: starting, working, waiting, finished, idle, errored, dead"`
	TimeoutS  int      `json:"timeout_s,omitempty" jsonschema:"seconds to wait before giving up, default 50, maximum 300; your own client may cut the call short before this"`
}

type messageStatusArgs struct {
	MessageID int64 `json:"message_id" jsonschema:"message id returned by send_session"`
}

type terminalCommands interface {
	List(sessionID string) ([]sessioncmd.Terminal, error)
	Create(sessionID string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error)
	Send(sessionID, terminalID, command string, keys []string) (sessioncmd.TerminalInput, error)
	Read(sessionID, terminalID string) (sessioncmd.TerminalScreen, error)
	Close(sessionID, terminalID string) error
}

type sessionCommands interface {
	List(sessionID string) ([]sessioncmd.Session, error)
	Create(sessionID string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error)
	Send(sessionID, targetID, message string) (sessioncmd.SendResult, error)
	Wait(ctx context.Context, sessionID, targetID string, until []string, timeout time.Duration) (sessioncmd.WaitResult, error)
	MessageStatus(sessionID string, messageID int64) (sessioncmd.MessageState, error)
	Read(sessionID, targetID string) (sessioncmd.SessionScreen, error)
	Revive(sessionID, targetID string) (sessioncmd.Session, error)
	Kill(sessionID, targetID string) (sessioncmd.Session, error)
	Archive(sessionID, targetID string, archived bool) (sessioncmd.Session, error)
	Tasks(sessionID string) ([]sessioncmd.Task, error)
	CreateTask(sessionID, title, body string, dependsOn []string) (sessioncmd.Task, error)
	ClaimTask(sessionID, taskID string) (sessioncmd.Task, error)
	FinishTask(sessionID, taskID string) (sessioncmd.Task, error)
	ReleaseTask(sessionID, taskID string) (sessioncmd.Task, error)
	DeleteTask(sessionID, taskID string) error
	Reserve(sessionID string, patterns []string, mode, note string, ttl time.Duration) (sessioncmd.ReserveResult, error)
	ReleaseFiles(sessionID string, patterns []string) (int, error)
	Reservations(sessionID string) ([]sessioncmd.Reservation, error)
	Groups(sessionID string) ([]sessioncmd.Group, error)
	CreateGroup(sessionID, path, directory string) (sessioncmd.Group, error)
	DeleteGroup(sessionID, path string) (sessioncmd.GroupRemoval, error)
}

// serverInstructions is the block a client shows its model before any tool
// is called, and it is what makes an agent reach for these tools at all:
// with it emptied, a model offered the same tools delegates to its own
// subagents instead. Claude Code truncates the block at 2048 characters, so
// it stays under that; what individual tool descriptions already carry (the
// review targets, the queueing rules) is left to them.
const serverInstructions = `Agent Manager runs this conversation in one of the user's managed tmux sessions. The others are separate CLI processes with contexts of their own, running any CLI the user chose (Claude Code, Codex, Gemini), never subagents of this conversation. These tools operate that workspace. Use them whenever the conditions below apply, without waiting to be asked.

Delegating to other agents. When the work holds two or more deliverables that could be built at once, or the user asks for parallel work, a second opinion or another agent: call list_sessions, reuse a relevant idle session, otherwise create_session per part, each with a descriptive name and a prompt stating the whole task, since a new agent cannot see this conversation. Repo work takes worktree: true so parallel agents never share a checkout; where they do, reserve_files before editing so an overlap surfaces early. Follow with read_session, send_session to answer or redirect an agent, and wait_for_session when your next step needs one finished. Put the plan on the shared task list with the task tool: spawned agents claim their next piece and finish it, unblocking what waited on it. Group related spawns with create_group, archive_session once done. Sessions spend the user's tokens, so create one per workstream, not per trivial step.

Shell work the user should see. Open a terminal when the session itself is the point: the user should be able to watch, attach or take over, as with SSH into a host. Keep one-shot local commands in your normal tools. Call list_terminals first and reuse a running terminal when possible. create_terminal nests under this session unless nest is false, which another group needs. Use send_terminal and read_terminal, and close_terminal when that job is done unless it is left for the user.

Everything here acts on the user's machine: create_session and create_terminal start real processes, send_terminal runs commands, and kill_session ends a running agent. Treat them with the care and approval normal shell execution needs.`

// NewServer builds the MCP server with every session tool registered.
// Split from Run so tests can connect an in-process client.
func NewServer(configDir, sessionID, version string) *mcp.Server {
	words := sessioncmd.MCPVocabulary()
	return newServer(configDir, sessionID, version, sessioncmd.NewTerminals(configDir, words), sessioncmd.NewSessions(configDir, words))
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
			"Prefer a broad feature name over a single subtask. " +
			"The result reports the name Agent Manager applied, or why the session keeps its current one.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args renameArgs) (*mcp.CallToolResult, any, error) {
		return textResult(sessioncmd.Rename(configDir, sessionID, args.Name))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "review",
		Description: "Declare what the user's review screen shows for this session; set any of repo, base and mode together. " +
			"repo is the git repo or worktree you are working in, so review opens there: declare it when you start in a repo or switch to another. " +
			"base is the git ref your work will merge into (e.g. origin/develop), which the branch scope diffs against; \"auto\" returns to auto-detection. " +
			"mode is the diff scope review opens with: uncommitted (working dir), branch (vs base), last_commit or staged, e.g. staged before committing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reviewArgs) (*mcp.CallToolResult, any, error) {
		var done []string
		if args.Repo != "" {
			message, err := sessioncmd.ReviewRepo(configDir, sessionID, args.Repo)
			if err != nil {
				return nil, nil, err
			}
			done = append(done, message)
		}
		if args.Base != "" {
			cwd := args.Repo
			if cwd == "" {
				cwd = "."
			}
			ref := args.Base
			if ref == "auto" {
				ref = ""
			}
			message, err := sessioncmd.ReviewBase(configDir, sessionID, cwd, ref)
			if err != nil {
				return nil, nil, applyFailure(err, done)
			}
			done = append(done, message)
		}
		if args.Mode != "" {
			message, err := sessioncmd.ReviewScope(configDir, sessionID, args.Mode)
			if err != nil {
				return nil, nil, applyFailure(err, done)
			}
			done = append(done, message)
		}
		if len(done) == 0 {
			return nil, nil, errors.New("set repo, base or mode")
		}
		return textContent(strings.Join(done, "; ")), nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "review_comment",
		Description: "Mark one sent review comment handled after addressing it, using the stable comment_id from the review prompt. " +
			"The comment stays visible in its original review round. Pass handled false only to reopen it.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reviewCommentArgs) (*mcp.CallToolResult, any, error) {
		handled := true
		if args.Handled != nil {
			handled = *args.Handled
		}
		return textResult(sessioncmd.ReviewComment(configDir, sessionID, args.CommentID, handled))
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_sessions",
		Description: "Call first whenever the work involves another agent: before delegating, before reporting what the fleet is doing, and to find the id of a session to read, prompt, revive, kill or archive. " +
			"Lists every agent session Agent Manager knows with ids, names, CLIs, groups, directories, worktree branches, statuses (starting, working, waiting, finished, idle, errored, dead) and which row is this session. " +
			"These are separate CLI processes running on the user's machine, each with its own context and its own conversation, and any CLI the user configured: Claude Code, Codex, Gemini and others, not only Claude. " +
			"They are not this conversation's subagents, they outlive this conversation, and the user watches them all in one list; nothing else this session can call reports them. " +
			"Reuse a relevant idle session instead of creating another; otherwise call create_session.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listSessionsArgs) (*mcp.CallToolResult, listSessionsOutput, error) {
		listed, err := sessions.List(sessionID)
		if err != nil {
			return nil, listSessionsOutput{}, err
		}
		return textContent(sessioncmd.FormatSessionList(listed)), listSessionsOutput{Sessions: listed}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_session",
		Description: "Start another agent CLI in its own Agent Manager session and hand it a task, so independent work runs beside this conversation instead of queued behind it. " +
			"The new session is a full CLI process of its own on the user's machine, which the user can watch and type into, and it can run a different CLI than this one. " +
			"Call it without waiting for the user when a task splits into parallel parts, or the user asks for a second agent or an independent opinion. " +
			"Pass a descriptive name and a prompt stating the whole task, since the new agent cannot see this conversation, and worktree true for repo work so it edits its own checkout and branch. " +
			"Follow it with read_session and send_session; use create_terminal instead for a plain shell.",
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
		return textContent("created " + sessioncmd.FormatSession(created)), created, nil
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
		return textContent(sessioncmd.FormatSessionScreen(screen)), screen, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "send_session",
		Description: "Queue a message for another agent, to give a session you spawned its next task, answer a question read_session surfaced, or redirect work going the wrong way. " +
			"The other agent has no view of this conversation, so send a self-contained instruction. " +
			"The message is not typed in the instant you send it: Agent Manager holds it until that agent is at rest, so it can never land on an approval prompt and answer it. " +
			"It arrives labelled as coming from another agent rather than from the user, and cannot approve permissions or change that agent's configuration. " +
			"Returns a message id for message_status; call read_session to see what the agent did with it. " +
			"Refused rather than delivered: identical text to the same session inside ten minutes, more than five messages a minute per recipient, more than twenty undelivered held by a recipient, or a message over 8000 bytes (point the agent at a file or a task instead). " +
			"After a refusal, call message_status on the earlier message rather than sending again.",
		Annotations: toolAnnotations(false, false, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendSessionArgs) (*mcp.CallToolResult, sessioncmd.SendResult, error) {
		result, err := sessions.Send(sessionID, args.SessionID, args.Message)
		if err != nil {
			return nil, sessioncmd.SendResult{}, err
		}
		return textContent(sessioncmd.FormatSendResult(result, args.SessionID)), result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "message_status",
		Description: "Check what happened to a message send_session queued: still queued, held, delivered into the agent's prompt, dropped, or answered. " +
			"Call it when you need to know a handoff landed before you build on it, instead of reading the other agent's screen. " +
			"Held means nothing will type it in as things stand: the recipient's screen is showing a dialog that has to be answered first, or its session is archived or no longer running, and reason says which. " +
			"Dropped means it never reached the prompt and will not be retried, so send it again.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args messageStatusArgs) (*mcp.CallToolResult, sessioncmd.MessageState, error) {
		state, err := sessions.MessageStatus(sessionID, args.MessageID)
		if err != nil {
			return nil, sessioncmd.MessageState{}, err
		}
		return textContent(sessioncmd.FormatMessageState(state)), state, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "wait_for_session",
		Description: "Park until another session stops working, instead of calling read_session in a loop. " +
			"Call it after handing an agent a task when your next step depends on its result. " +
			"By default it returns once the session reaches any state that means it stopped (finished, waiting, idle, errored or dead); pass until to wait for particular states. " +
			"A timeout is a normal answer, not a failure: the result carries reached false and the actual state, and outcome says whether it reached, timed_out or died. " +
			"Follow it with read_session to see what the agent produced.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args waitSessionArgs) (*mcp.CallToolResult, sessioncmd.WaitResult, error) {
		result, err := sessions.Wait(ctx, sessionID, args.SessionID, args.Until, time.Duration(args.TimeoutS)*time.Second)
		if err != nil {
			return nil, sessioncmd.WaitResult{}, err
		}
		return textContent(sessioncmd.FormatWaitResult(result)), result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "revive_session",
		Description: "Bring a dead session back on its old row, resuming the conversation it held where its CLI supports that. " +
			"An agent that quit while its window stayed open comes back inside that same pane. " +
			"Call when list_sessions or send_session reports a session is not running and its work should continue.",
		Annotations: toolAnnotations(false, false, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sessionTargetArgs) (*mcp.CallToolResult, sessioncmd.Session, error) {
		revived, err := sessions.Revive(sessionID, args.SessionID)
		if err != nil {
			return nil, sessioncmd.Session{}, err
		}
		return textContent("revived " + sessioncmd.FormatSession(revived)), revived, nil
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
		return textContent("killed " + sessioncmd.FormatSession(killed)), killed, nil
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
		return textContent(sessioncmd.FormatArchiveState(updated)), updated, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "task",
		Description: "The shared work list every session in this manager claims from; action picks the operation. " +
			"list reads it: call it before starting work so two agents do not build the same thing, and before reporting progress on a fleet. " +
			"create puts a piece of work up for any session to pick up, instead of holding the plan where nobody else sees it: split the plan into tasks when you spawn a fleet, and sequence with depends_on, which makes a dependent claimable the moment what it waits on finishes. " +
			"claim takes a task before you start it, so no other session picks the same piece; omitting task_id takes the oldest unblocked pending task, which is how a worker finds its next job, and a task another session holds is refused with the holder named. " +
			"finish marks a claimed task done, unblocking its dependents; call it the moment the work completes, since a task left in progress keeps other agents idle. " +
			"release hands a claimed task back for another session. delete removes work that turned out not to be needed.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args taskArgs) (*mcp.CallToolResult, taskOutput, error) {
		switch args.Action {
		case "list":
			listed, err := sessions.Tasks(sessionID)
			if err != nil {
				return nil, taskOutput{}, err
			}
			return textContent(sessioncmd.FormatTaskList(listed)), taskOutput{Tasks: listed}, nil
		case "create":
			created, err := sessions.CreateTask(sessionID, args.Title, args.Body, args.DependsOn)
			if err != nil {
				return nil, taskOutput{}, err
			}
			return textContent("created " + sessioncmd.FormatTask(created)), taskOutput{Task: &created}, nil
		case "claim":
			claimed, err := sessions.ClaimTask(sessionID, args.TaskID)
			if err != nil {
				return nil, taskOutput{}, err
			}
			return textContent("claimed " + sessioncmd.FormatTask(claimed)), taskOutput{Task: &claimed}, nil
		case "finish":
			finished, err := sessions.FinishTask(sessionID, args.TaskID)
			if err != nil {
				return nil, taskOutput{}, err
			}
			return textContent("finished " + sessioncmd.FormatTask(finished)), taskOutput{Task: &finished}, nil
		case "release":
			released, err := sessions.ReleaseTask(sessionID, args.TaskID)
			if err != nil {
				return nil, taskOutput{}, err
			}
			return textContent("released " + sessioncmd.FormatTask(released)), taskOutput{Task: &released}, nil
		case "delete":
			if err := sessions.DeleteTask(sessionID, args.TaskID); err != nil {
				return nil, taskOutput{}, err
			}
			return textContent("deleted task " + args.TaskID), taskOutput{}, nil
		default:
			return nil, taskOutput{}, fmt.Errorf("unknown action %q (list, create, claim, finish, release, delete)", args.Action)
		}
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "reserve_files",
		Description: "Declare the files you are about to edit, so another agent working the same repo finds out before both of you change them. " +
			"Call it when several sessions share one checkout and you are starting on a set of files; a session in its own worktree does not need it. " +
			"The lease is advisory: nothing is blocked, and any overlap with a lease another session holds comes back in conflicts, with the holder to message. " +
			"It lapses on its own, so an agent that dies never holds the repo. Call release_files when you are done.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args reserveFilesArgs) (*mcp.CallToolResult, sessioncmd.ReserveResult, error) {
		result, err := sessions.Reserve(sessionID, args.Paths, args.Mode, args.Note, time.Duration(args.TTLM)*time.Minute)
		if err != nil {
			return nil, sessioncmd.ReserveResult{}, err
		}
		return textContent(sessioncmd.FormatReserveResult(result)), result, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "release_files",
		Description: "Give back the leases you took with reserve_files once the edits are made, so another agent can take those paths. " +
			"Omit paths to release everything this session holds.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args releaseFilesArgs) (*mcp.CallToolResult, releaseFilesOutput, error) {
		released, err := sessions.ReleaseFiles(sessionID, args.Paths)
		if err != nil {
			return nil, releaseFilesOutput{}, err
		}
		return textContent(sessioncmd.FormatReleased(released)), releaseFilesOutput{Released: released}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_reservations",
		Description: "See which files the other sessions are working on right now. " +
			"Call it before editing shared code, or when planning who takes which part of a change.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listReservationsArgs) (*mcp.CallToolResult, listReservationsOutput, error) {
		listed, err := sessions.Reservations(sessionID)
		if err != nil {
			return nil, listReservationsOutput{}, err
		}
		return textContent(sessioncmd.FormatReservations(listed)), listReservationsOutput{Reservations: listed}, nil
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
		return textContent(sessioncmd.FormatGroupList(listed)), listGroupsOutput{Groups: listed}, nil
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
		Name: "delete_group",
		Description: "Delete a group once the work filed under it is done, so a fleet does not leave a heading behind in the user's list. " +
			"Groups nested under it go too. Any session still filed there moves to the root group rather than being stopped, " +
			"so this never ends an agent: kill_session or archive_session those first if that is what you mean.",
		Annotations: toolAnnotations(false, true, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args deleteGroupArgs) (*mcp.CallToolResult, sessioncmd.GroupRemoval, error) {
		removal, err := sessions.DeleteGroup(sessionID, args.Path)
		if err != nil {
			return nil, sessioncmd.GroupRemoval{}, err
		}
		return textContent(sessioncmd.FormatGroupRemoval(removal)), removal, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "list_terminals",
		Description: "Call before opening a terminal for human-visible work, to find one you can reuse. " +
			"Lists active managed terminals with ids, names, groups, current directories, statuses, whether their tmux panes are running, and the session each one is nested under. " +
			"Reuse a relevant running terminal when possible; otherwise call create_terminal. Use the returned id with send_terminal and read_terminal.",
		Annotations: toolAnnotations(true, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listTerminalsArgs) (*mcp.CallToolResult, listTerminalsOutput, error) {
		listed, err := terminals.List(sessionID)
		if err != nil {
			return nil, listTerminalsOutput{}, err
		}
		output := listTerminalsOutput{Terminals: listed}
		return textContent(sessioncmd.FormatTerminalList(listed)), output, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "create_terminal",
		Description: "Create a managed terminal for human-visible work such as SSH, not for one-shot local commands or other internal work. " +
			"It nests under this session unless nest is false, which a group other than this session's needs; a terminal created from a terminal joins it as a sibling under the same agent. The group supplies the inherited directory, and directory set explicitly wins. " +
			"Then call send_terminal with the returned id; use create_session instead for another agent CLI. Call close_terminal when the job is finished and the terminal is not being left for the user.",
		Annotations: toolAnnotations(false, false, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args createTerminalArgs) (*mcp.CallToolResult, sessioncmd.Terminal, error) {
		created, err := terminals.Create(sessionID, sessioncmd.CreateTerminalOptions{
			Group:     args.Group,
			Directory: args.Directory,
			Nest:      args.Nest,
		})
		if err != nil {
			return nil, sessioncmd.Terminal{}, err
		}
		return textContent("created " + sessioncmd.FormatTerminal(created)), created, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "send_terminal",
		Description: "Call after list_terminals or create_terminal to run or control work in a managed terminal, keeping it visible and separate from the conversation. Provide exactly one of command or keys. " +
			"A command is pasted and submitted with Enter, so it executes on the user's machine. " +
			"Keys sends exact tmux key names for interactive control, such as [\"C-c\"] or [\"Up\", \"Enter\"]. Call read_terminal after sending to inspect the result.",
		Annotations: toolAnnotations(false, true, true),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args sendTerminalArgs) (*mcp.CallToolResult, sessioncmd.TerminalInput, error) {
		sent, err := terminals.Send(sessionID, args.TerminalID, args.Command, args.Keys)
		if err != nil {
			return nil, sessioncmd.TerminalInput{}, err
		}
		return textContent(sessioncmd.FormatTerminalInput(sent)), sent, nil
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
		return textContent(sessioncmd.FormatTerminalScreen(screen)), screen, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: "close_terminal",
		Description: "Delete a terminal nested under this session once its job is finished: kills the pane and removes the row. " +
			"Leave it running when you opened it for the user (for example an SSH session they may attach to). " +
			"Refuses agent sessions, un-nested terminals, and terminals under another session.",
		Annotations: toolAnnotations(false, true, false),
	}, func(ctx context.Context, req *mcp.CallToolRequest, args closeTerminalArgs) (*mcp.CallToolResult, any, error) {
		if err := terminals.Close(sessionID, args.TerminalID); err != nil {
			return nil, nil, err
		}
		return textContent("closed terminal " + args.TerminalID), nil, nil
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

func textContent(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: message}}}
}

// applyFailure carries the declarations a review call already applied
// into its error, so a caller that set several at once knows which took
// effect before the failing one.
func applyFailure(err error, done []string) error {
	if len(done) == 0 {
		return err
	}
	return fmt.Errorf("%w (already applied: %s)", err, strings.Join(done, "; "))
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
