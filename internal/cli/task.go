package cli

import (
	"fmt"
	"io"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

const (
	usageTaskList    = "task list [--json]"
	usageTaskCreate  = "task create <title> [--body <text>] [--depends-on <id,id>] [--json]"
	usageTaskClaim   = "task claim [<task-id>] [--json]"
	usageTaskFinish  = "task finish <task-id> [--json]"
	usageTaskRelease = "task release <task-id> [--json]"
	usageTaskDelete  = "task delete <task-id>"
)

type taskCommands interface {
	Tasks(sessionID string) ([]sessioncmd.Task, error)
	CreateTask(sessionID, title, body string, dependsOn []string) (sessioncmd.Task, error)
	ClaimTask(sessionID, taskID string) (sessioncmd.Task, error)
	FinishTask(sessionID, taskID string) (sessioncmd.Task, error)
	ReleaseTask(sessionID, taskID string) (sessioncmd.Task, error)
	DeleteTask(sessionID, taskID string) error
}

func newTasks(configDir string) taskCommands {
	return sessioncmd.NewSessions(configDir, sessioncmd.CLIVocabulary())
}

func taskVerbs() []command {
	return []command{
		{name: "list", usage: usageTaskList, about: "read the work list every session shares: what is pending, who holds what, and what is blocked", run: bind(newTasks, runTaskList)},
		{name: "create", usage: usageTaskCreate, about: "put a piece of work on the shared list so any session can pick it up", run: bind(newTasks, runTaskCreate)},
		{name: "claim", usage: usageTaskClaim, about: "take a task before you start it; omit the id to take the oldest pending task nothing is blocking", run: bind(newTasks, runTaskClaim)},
		{name: "finish", usage: usageTaskFinish, about: "mark a task you claimed as done, which unblocks everything waiting on it", run: bind(newTasks, runTaskFinish)},
		{name: "release", usage: usageTaskRelease, about: "hand a task you claimed back to the list for another session", run: bind(newTasks, runTaskRelease)},
		{name: "delete", usage: usageTaskDelete, about: "remove a task nobody needs any more", run: bind(newTasks, runTaskDelete)},
	}
}

func taskSection() section {
	return groupSection("Shared task list", "task", "the work list every session in this manager claims from", taskVerbs())
}

func runTaskList(out io.Writer, tasks taskCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTaskList)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	listed, err := tasks.Tasks(sessionID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, listed, sessioncmd.FormatTaskList(listed))
}

func runTaskCreate(out io.Writer, tasks taskCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTaskCreate)
	body := set.String("body", "", "full instruction for whoever claims it; it cannot see your conversation")
	var dependsOn stringList
	set.Var(&dependsOn, "depends-on", "ids of tasks that must be done first, repeatable or comma separated")
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	created, err := tasks.CreateTask(sessionID, operands[0], *body, dependsOn)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, created, "created "+sessioncmd.FormatTask(created))
}

func runTaskClaim(out io.Writer, tasks taskCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTaskClaim)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 0, 1)
	if err != nil {
		return err
	}
	taskID := ""
	if len(operands) == 1 {
		taskID = operands[0]
	}
	claimed, err := tasks.ClaimTask(sessionID, taskID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, claimed, "claimed "+sessioncmd.FormatTask(claimed))
}

func runTaskFinish(out io.Writer, tasks taskCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTaskFinish)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	finished, err := tasks.FinishTask(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, finished, "finished "+sessioncmd.FormatTask(finished))
}

func runTaskRelease(out io.Writer, tasks taskCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTaskRelease)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	released, err := tasks.ReleaseTask(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, released, "released "+sessioncmd.FormatTask(released))
}

func runTaskDelete(out io.Writer, tasks taskCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTaskDelete)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	if err := tasks.DeleteTask(sessionID, operands[0]); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "deleted task "+operands[0])
	return err
}
