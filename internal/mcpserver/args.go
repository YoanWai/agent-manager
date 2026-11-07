package mcpserver

import "github.com/YoanWai/agent-manager/internal/sessioncmd"

type renameArgs struct {
	Name string `json:"name" jsonschema:"short 2-4 word kebab-case name for the broad feature of this whole session, not one subtask"`
}

type reviewArgs struct {
	Repo string `json:"repo,omitempty" jsonschema:"path inside the git repo or worktree being worked on, so review opens there"`
	Base string `json:"base,omitempty" jsonschema:"git ref the branch scope diffs against (e.g. origin/develop); \"auto\" returns to auto-detection"`
	Mode string `json:"mode,omitempty" jsonschema:"diff scope review opens with: uncommitted, branch, commit or staged"`
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
	Host      string  `json:"host,omitempty" jsonschema:"SSH connection to open the terminal on, the host list_terminals reports; omit for this machine. There it is un-nested and only this session can send to or close it; group defaults to the root and directory to the remote user's home"`
	Name      string  `json:"name,omitempty" jsonschema:"descriptive name for the terminal, such as cu-1234-api, applied at creation; omit to name it after the session it hangs under"`
}

type sendTerminalArgs struct {
	TerminalID string   `json:"terminal_id" jsonschema:"terminal id returned by list_terminals or create_terminal"`
	Command    string   `json:"command,omitempty" jsonschema:"command text to paste and submit with Enter; provide exactly one of command or keys"`
	Keys       []string `json:"keys,omitempty" jsonschema:"exact tmux key names to send in order, such as C-c, Up, or Enter; provide exactly one of keys or command"`
}

type readTerminalArgs struct {
	TerminalID string `json:"terminal_id" jsonschema:"terminal id returned by list_terminals or create_terminal"`
	Tail       int    `json:"tail,omitempty" jsonschema:"return only the last N non-blank rows; omit for the whole screen"`
}

type readSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions or create_session"`
	Tail      int    `json:"tail,omitempty" jsonschema:"return only the last N content rows, without the CLI's composer, footer and spinners; omit for the whole screen"`
}

type listSessionsArgs struct{}

type createSessionArgs struct {
	Name        string  `json:"name,omitempty" jsonschema:"kebab-case name for the new session, 2-4 words naming the work it will do (e.g. payments-retry-fix); leave empty only when the task is unknown, and the new agent will name itself"`
	Prompt      string  `json:"prompt,omitempty" jsonschema:"first task to hand the new agent, written as a full instruction; an empty task sends only the saved prompt when one is selected, otherwise it starts idle"`
	Saved       string  `json:"saved,omitempty" jsonschema:"name of a saved prompt from Settings; resolved freshly at creation and prepended literally to the first task; omit for none; a missing name refuses creation"`
	Tool        string  `json:"tool,omitempty" jsonschema:"agent CLI to run, such as claude, codex, opencode, gemini or grok; defaults to the caller's CLI, or to the one picked in settings when the caller is a terminal; call list_sessions to see which are in use"`
	Group       *string `json:"group,omitempty" jsonschema:"existing group path to file the session under; pass an empty string for the root group; defaults to this agent's group; call list_groups for the existing ones"`
	Directory   string  `json:"directory,omitempty" jsonschema:"existing directory the session works in; defaults to this agent's own directory, or to the selected group's inherited path when group is set"`
	Worktree    *bool   `json:"worktree,omitempty" jsonschema:"true gives the session its own git worktree and branch off the directory's repo, which is what keeps parallel agents from overwriting each other; omit to inherit the group's default"`
	Model       string  `json:"model,omitempty" jsonschema:"model to run the CLI on, one it lists; omit to keep the CLI's own default, which is what the user set up; a wrong name is refused with the models the CLI lists"`
	Effort      string  `json:"effort,omitempty" jsonschema:"reasoning effort, one the chosen model takes; omit to keep the CLI's own"`
	Profile     string  `json:"profile,omitempty" jsonschema:"profile to launch the CLI under, for a CLI that has them (hermes); omit for the CLI's active one"`
	Environment *string `json:"environment,omitempty" jsonschema:"environment profile from Agent Manager to launch under, a named set of environment variables the user defined; omit to inherit the group's, else this agent's own; pass an empty string for none; not taken with host"`
	Resume      string  `json:"resume,omitempty" jsonschema:"conversation id the chosen CLI saved earlier. The session opens that conversation instead of a new one, keeps it on every revive, and sends it nothing unless prompt is set. Pass the directory it was saved in, a name when there is no prompt, and no worktree"`
	Host        string  `json:"host,omitempty" jsonschema:"SSH connection to start the session on, the host list_sessions reports; omit for this machine. There nothing is inherited from this agent: group defaults to the root, directory to the remote user's home and tool to the one picked in that host's settings"`
}

type sessionTargetArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions or create_session"`
}

type sendSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions or create_session"`
	Message   string `json:"message,omitempty" jsonschema:"message typed into that agent's prompt as its next turn; write it as a full instruction, since the other agent does not see this conversation; omit it to send only the saved prompt"`
	Saved     string `json:"saved,omitempty" jsonschema:"name of a saved prompt from Settings; resolved when sending and placed in front of message; omit for none; a missing name refuses the send"`
}

type archiveSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions"`
	Archived  *bool  `json:"archived,omitempty" jsonschema:"true archives the session out of the active list, false restores it; defaults to true"`
}

type moveSessionArgs struct {
	SessionID string `json:"session_id" jsonschema:"session id returned by list_sessions"`
	Group     string `json:"group" jsonschema:"existing group path to file it under, from list_groups; empty for the root group"`
}

type endSelfArgs struct {
	Cancel bool `json:"cancel,omitempty" jsonschema:"true withdraws the archive or kill this session has pending instead of asking for one"`
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
	Host      string `json:"host,omitempty" jsonschema:"SSH connection to create the group on, the host list_groups reports; omit for this machine"`
}

type deleteGroupArgs struct {
	Path string `json:"path" jsonschema:"full group path to delete, slash separated; groups nested under it go too"`
	Host string `json:"host,omitempty" jsonschema:"SSH connection the group is on, the host list_groups reports; omit for this machine"`
}

type closeTerminalArgs struct {
	TerminalID string `json:"terminal_id" jsonschema:"terminal id returned by list_terminals or create_terminal"`
}

type listTerminalsOutput struct {
	Terminals        []terminalRow     `json:"terminals"`
	ConnectionErrors []connectionError `json:"connection_errors,omitempty" jsonschema:"SSH connections that could not be refreshed for this list; it is never refused for them"`
}

type listSessionsOutput struct {
	Sessions         []sessionRow      `json:"sessions"`
	ConnectionErrors []connectionError `json:"connection_errors,omitempty" jsonschema:"SSH connections that could not be refreshed for this list; it is never refused for them"`
}

type taskOutput struct {
	Tasks []sessioncmd.Task `json:"tasks,omitempty" jsonschema:"the whole shared list, for action list"`
	Task  *sessioncmd.Task  `json:"task,omitempty" jsonschema:"the task acted on"`
}

type listReservationsOutput struct {
	Reservations []sessioncmd.Reservation `json:"reservations"`
}

type listGroupsOutput struct {
	Groups           []groupRow        `json:"groups"`
	ConnectionErrors []connectionError `json:"connection_errors,omitempty" jsonschema:"SSH connections that could not be refreshed for this list; it is never refused for them"`
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

type reportIssueArgs struct {
	Kind      string `json:"kind" jsonschema:"bug for something the manager does wrong, feature for something it should do"`
	Title     string `json:"title" jsonschema:"one-line issue title naming the behaviour, as the user would search for it"`
	Body      string `json:"body" jsonschema:"bug: what the user did, what they expected and what happened instead, step by step; feature: what they are trying to do and what they have in mind. Markdown; it is posted publicly, so no secrets or private paths"`
	PreviewID string `json:"preview_id,omitempty" jsonschema:"omitted returns a preview and posts nothing; after the user approves that preview, pass the id it returned to file exactly what they saw"`
}
