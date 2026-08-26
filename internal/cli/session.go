package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

const (
	usageSessions      = "sessions [--json]"
	usageSpawn         = "spawn [--name <name>] [--prompt <text>] [--tool <cli>] [--group <path>] [--directory <path>] [--worktree] [--json]"
	usageSend          = `send <session-id> "<message>" [--json]`
	usageRead          = "read <session-id> [--json]"
	usageWait          = "wait <session-id> [--until <state>] [--timeout <duration>] [--json]"
	usageMessageStatus = "message-status <message-id> [--json]"
	usageKill          = "kill <session-id> [--json]"
	usageRevive        = "revive <session-id> [--json]"
	usageArchive       = "archive <session-id> [--restore] [--json]"
	usageGroups        = "groups [--json]"
	usageCreateGroup   = "create-group <path> [--directory <path>] [--json]"
	usageDeleteGroup   = "delete-group <path> [--json]"
)

type sessionCommands interface {
	List(sessionID string) ([]sessioncmd.Session, error)
	Create(sessionID string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error)
	Send(sessionID, targetID, message string) (sessioncmd.SendResult, error)
	Read(sessionID, targetID string) (sessioncmd.SessionScreen, error)
	Wait(ctx context.Context, sessionID, targetID string, until []string, timeout time.Duration) (sessioncmd.WaitResult, error)
	MessageStatus(sessionID string, messageID int64) (sessioncmd.MessageState, error)
	Kill(sessionID, targetID string) (sessioncmd.Session, error)
	Revive(sessionID, targetID string) (sessioncmd.Session, error)
	Archive(sessionID, targetID string, archived bool) (sessioncmd.Session, error)
	Groups(sessionID string) ([]sessioncmd.Group, error)
	CreateGroup(sessionID, path, directory string) (sessioncmd.Group, error)
	DeleteGroup(sessionID, path string) (sessioncmd.GroupRemoval, error)
}

func newSessions(configDir string) sessionCommands {
	return sessioncmd.NewSessions(configDir, sessioncmd.CLIVocabulary())
}

func sessionSection() section {
	return section{
		title: "Agent sessions",
		commands: []command{
			{name: "sessions", usage: usageSessions, about: "list every agent session with its id, CLI, group, directory and status; call it before delegating anything", run: bind(newSessions, runSessions)},
			{name: "spawn", usage: usageSpawn, about: "start another agent CLI on a task of its own, so independent work runs beside you instead of queued behind you", run: bind(newSessions, runSpawn)},
			{name: "send", usage: usageSend, about: "queue a message for another agent; it is typed in once that agent is at rest, so it never lands on an approval prompt", run: bind(newSessions, runSend)},
			{name: "read", usage: usageRead, about: "read what another agent's screen currently shows", run: bind(newSessions, runRead)},
			{name: "wait", usage: usageWait, about: "park until another session stops working, instead of reading its screen in a loop; exits non-zero when it timed out", run: bind(newSessions, runWait)},
			{name: "message-status", usage: usageMessageStatus, about: "check whether a message you sent is queued, held, delivered, dropped or answered", run: bind(newSessions, runMessageStatus)},
			{name: "kill", usage: usageKill, about: "stop another agent's process, ending whatever it is doing; its row keeps the last screen", run: bind(newSessions, runKill)},
			{name: "revive", usage: usageRevive, about: "bring a dead session back on its old row, resuming the conversation it held; an agent that quit inside a live pane comes back there", run: bind(newSessions, runRevive)},
			{name: "archive", usage: usageArchive, about: "file a finished session out of the active list, or restore it with --restore", run: bind(newSessions, runArchive)},
			{name: "groups", usage: usageGroups, about: "list the groups sessions and terminals are filed under", run: bind(newSessions, runGroups)},
			{name: "create-group", usage: usageCreateGroup, about: "create a group so a fleet you spawn stays together in the user's list", run: bind(newSessions, runCreateGroup)},
			{name: "delete-group", usage: usageDeleteGroup, about: "remove a group whose work is done; sessions still in it move to the root rather than stopping", run: bind(newSessions, runDeleteGroup)},
		},
	}
}

func runSessions(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageSessions)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	listed, err := sessions.List(sessionID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, listed, sessioncmd.FormatSessionList(listed))
}

func runSpawn(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageSpawn)
	name := set.String("name", "", "kebab-case name naming the work it will do; the new agent names itself when this is empty")
	prompt := set.String("prompt", "", "first task to hand it, written as a full instruction, since it cannot see your conversation")
	tool := set.String("tool", "", "agent CLI to run; defaults to the CLI this session runs")
	group := set.String("group", "", "existing group path to file it under; pass an empty string for the root group")
	directory := set.String("directory", "", "existing directory it works in; defaults to yours, or to the group's inherited path")
	worktree := set.Bool("worktree", false, "give it its own git worktree and branch, which is what keeps parallel agents off each other's files")
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	opts := sessioncmd.CreateSessionOptions{
		Tool:      *tool,
		Name:      *name,
		Directory: *directory,
		Prompt:    *prompt,
	}
	// An omitted group inherits this session's, and an omitted worktree the
	// group's default, so only a flag the caller actually typed is passed on.
	set.Visit(func(given *flag.Flag) {
		switch given.Name {
		case "group":
			opts.Group = group
		case "worktree":
			opts.Worktree = worktree
		}
	})
	created, err := sessions.Create(sessionID, opts)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, created, "created "+sessioncmd.FormatSession(created))
}

func runSend(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageSend)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 2, 2)
	if err != nil {
		return err
	}
	result, err := sessions.Send(sessionID, operands[0], operands[1])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, result, sessioncmd.FormatSendResult(result, operands[0]))
}

func runRead(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageRead)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	screen, err := sessions.Read(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, screen, sessioncmd.FormatSessionScreen(screen))
}

// runWait exits non-zero when the session never reached an awaited state, so
// `agent-manager wait <id> && next-step` reads the outcome the way a shell
// caller expects. The result still goes out first, JSON included.
func runWait(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageWait)
	var until stringList
	set.Var(&until, "until", "state that ends the wait, repeatable or comma separated; defaults to every state meaning the session stopped working")
	timeout := set.Duration("timeout", 0, "how long to wait before giving up, default "+sessioncmd.DefaultWaitTimeout.String()+", maximum "+sessioncmd.MaxWaitTimeout.String())
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	result, err := sessions.Wait(context.Background(), sessionID, operands[0], until, *timeout)
	if err != nil {
		return err
	}
	human := sessioncmd.FormatWaitResult(result)
	if !result.Reached {
		if *asJSON {
			if err := writeJSON(out, result); err != nil {
				return err
			}
		}
		return errors.New(human)
	}
	return emit(out, *asJSON, result, human)
}

func runMessageStatus(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageMessageStatus)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	messageID, err := strconv.ParseInt(operands[0], 10, 64)
	if err != nil {
		return fmt.Errorf("message id %q is not a number; agent-manager send prints the id it queued", operands[0])
	}
	state, err := sessions.MessageStatus(sessionID, messageID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, state, sessioncmd.FormatMessageState(state))
}

func runKill(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageKill)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	killed, err := sessions.Kill(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, killed, "killed "+sessioncmd.FormatSession(killed))
}

func runRevive(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageRevive)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	revived, err := sessions.Revive(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, revived, "revived "+sessioncmd.FormatSession(revived))
}

func runArchive(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageArchive)
	restore := set.Bool("restore", false, "put an archived session back on the active list")
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	updated, err := sessions.Archive(sessionID, operands[0], !*restore)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, updated, sessioncmd.FormatArchiveState(updated))
}

func runGroups(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageGroups)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	listed, err := sessions.Groups(sessionID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, listed, sessioncmd.FormatGroupList(listed))
}

func runCreateGroup(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageCreateGroup)
	directory := set.String("directory", "", "default working directory sessions created in this group inherit")
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	created, err := sessions.CreateGroup(sessionID, operands[0], *directory)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, created, "created group "+created.Path)
}

func runDeleteGroup(out io.Writer, sessions sessionCommands, args []string, sessionID string) error {
	set := newFlagSet(usageDeleteGroup)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	removal, err := sessions.DeleteGroup(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, removal, sessioncmd.FormatGroupRemoval(removal))
}
