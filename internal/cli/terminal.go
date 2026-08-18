package cli

import (
	"flag"
	"fmt"
	"io"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

const (
	usageTerminalList   = "terminal list [--json]"
	usageTerminalCreate = "terminal create [--group <path>] [--directory <path>] [--nest=false] [--json]"
	usageTerminalSend   = `terminal send <terminal-id> --command "<text>" | --keys <key,key> [--json]`
	usageTerminalRead   = "terminal read <terminal-id> [--json]"
	usageTerminalClose  = "terminal close <terminal-id>"
)

type terminalCommands interface {
	List(sessionID string) ([]sessioncmd.Terminal, error)
	Create(sessionID string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error)
	Send(sessionID, terminalID, command string, keys []string) (sessioncmd.TerminalInput, error)
	Read(sessionID, terminalID string) (sessioncmd.TerminalScreen, error)
	Close(sessionID, terminalID string) error
}

func newTerminals(configDir string) terminalCommands {
	return sessioncmd.NewTerminals(configDir, sessioncmd.CLIVocabulary())
}

func terminalVerbs() []command {
	return []command{
		{name: "list", usage: usageTerminalList, about: "find a running terminal to reuse before opening another one", run: bind(newTerminals, runTerminalList)},
		{name: "create", usage: usageTerminalCreate, about: "open a terminal for work the user should see, such as SSH into a host; it hangs under this session unless --nest=false", run: bind(newTerminals, runTerminalCreate)},
		{name: "send", usage: usageTerminalSend, about: "run a command there, or send exact tmux keys such as C-c to control what is running", run: bind(newTerminals, runTerminalSend)},
		{name: "read", usage: usageTerminalRead, about: "read what that terminal's screen currently shows", run: bind(newTerminals, runTerminalRead)},
		{name: "close", usage: usageTerminalClose, about: "close a terminal opened under this session once its job is done, killing the pane and deleting the row", run: bind(newTerminals, runTerminalClose)},
	}
}

func terminalSection() section {
	return groupSection("Managed terminals", "terminal", "shells the user watches beside the agents, for work they should be able to watch, attach to or take over", terminalVerbs())
}

func runTerminalList(out io.Writer, terminals terminalCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTerminalList)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	listed, err := terminals.List(sessionID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, listed, sessioncmd.FormatTerminalList(listed))
}

func runTerminalCreate(out io.Writer, terminals terminalCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTerminalCreate)
	group := set.String("group", "", "existing group path to open it in; pass an empty string for the root group")
	directory := set.String("directory", "", "existing directory to open; defaults to yours, or to the group's inherited path")
	nest := set.Bool("nest", true, "hang the terminal under this session; pass --nest=false to leave it loose or to open it in another group")
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	opts := sessioncmd.CreateTerminalOptions{Directory: *directory, Nest: nest}
	// An omitted group inherits this session's, so only a flag the caller
	// actually typed is passed on.
	set.Visit(func(given *flag.Flag) {
		if given.Name == "group" {
			opts.Group = group
		}
	})
	created, err := terminals.Create(sessionID, opts)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, created, "created "+sessioncmd.FormatTerminal(created))
}

func runTerminalSend(out io.Writer, terminals terminalCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTerminalSend)
	text := set.String("command", "", "command text to paste and submit with Enter, which executes on the user's machine")
	var keys stringList
	set.Var(&keys, "keys", "exact tmux key names to send in order, repeatable or comma separated, such as C-c or Up,Enter")
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	sent, err := terminals.Send(sessionID, operands[0], *text, keys)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, sent, sessioncmd.FormatTerminalInput(sent))
}

func runTerminalRead(out io.Writer, terminals terminalCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTerminalRead)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	screen, err := terminals.Read(sessionID, operands[0])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, screen, sessioncmd.FormatTerminalScreen(screen))
}

func runTerminalClose(out io.Writer, terminals terminalCommands, args []string, sessionID string) error {
	set := newFlagSet(usageTerminalClose)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	if err := terminals.Close(sessionID, operands[0]); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "closed terminal "+operands[0])
	return err
}
