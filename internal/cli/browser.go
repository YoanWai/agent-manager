package cli

import (
	"fmt"
	"io"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

const (
	usageBrowserList    = "browser list [--json]"
	usageBrowserCreate  = "browser create [--url <url>] [--group <path>] [--directory <path>] [--nest=false] [--json]"
	usageBrowserCommand = `browser command <browser-id> "<agent-browser command>" [--json]`
	usageBrowserClose   = "browser close <browser-id>"
)

type browserCommands interface {
	List(sessionID string) ([]sessioncmd.Browser, error)
	Create(sessionID string, opts sessioncmd.CreateBrowserOptions) (sessioncmd.Browser, error)
	Command(sessionID, browserID, command string) (sessioncmd.BrowserOutput, error)
	Close(sessionID, browserID string) error
}

func newBrowsers(configDir string) browserCommands {
	return sessioncmd.NewBrowsers(configDir, sessioncmd.CLIVocabulary())
}

func browserVerbs() []command {
	return browserVerbsWith(newBrowsers)
}

func browserVerbsWith(open func(string) browserCommands) []command {
	return []command{
		{name: "list", usage: usageBrowserList, about: "find a running browser to reuse before opening another one", run: bind(open, runBrowserList)},
		{name: "create", usage: usageBrowserCreate, about: "open a browser the user can watch, at --url when given; it hangs under this session unless --nest=false", run: bind(open, runBrowserCreate)},
		{name: "command", usage: usageBrowserCommand, about: `run one agent-browser command in a browser opened under this session, such as "snapshot -i" or "click @e14", and print its output`, run: bind(open, runBrowserCommand)},
		{name: "close", usage: usageBrowserClose, about: "close a browser opened under this session once its job is done, killing the pane and deleting the row", run: bind(open, runBrowserClose)},
	}
}

func browserSection() section {
	return browserSectionWith(newBrowsers)
}

func browserSectionWith(open func(string) browserCommands) section {
	return groupSection("Managed browsers", "browser", "a real browser the user watches beside the agents, driven with agent-browser commands", browserVerbsWith(open))
}

func runBrowserList(out io.Writer, browsers browserCommands, args []string, sessionID string) error {
	set := newFlagSet(usageBrowserList)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	listed, err := browsers.List(sessionID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, listed, sessioncmd.FormatBrowserList(listed))
}

func runBrowserCreate(out io.Writer, browsers browserCommands, args []string, sessionID string) error {
	set := newFlagSet(usageBrowserCreate)
	url := set.String("url", "", "page to open right away")
	group := set.String("group", "", "existing group path to open it in; pass an empty string for the root group")
	directory := set.String("directory", "", "existing directory to open; defaults to yours, or to the group's inherited path")
	nest := set.Bool("nest", true, "hang the browser under this session, the default when run from one; pass --nest=false to leave it loose or to open it in another group")
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	opts := sessioncmd.CreateBrowserOptions{URL: *url, Directory: *directory}
	opts.Group, opts.Nest = typedPlacement(set, group, nest)
	created, err := browsers.Create(sessionID, opts)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, created, "created "+sessioncmd.FormatBrowser(created))
}

func runBrowserCommand(out io.Writer, browsers browserCommands, args []string, sessionID string) error {
	set := newFlagSet(usageBrowserCommand)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 2, 2)
	if err != nil {
		return err
	}
	result, err := browsers.Command(sessionID, operands[0], operands[1])
	if err != nil {
		return err
	}
	return emit(out, *asJSON, result, sessioncmd.FormatBrowserOutput(result))
}

func runBrowserClose(out io.Writer, browsers browserCommands, args []string, sessionID string) error {
	set := newFlagSet(usageBrowserClose)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	if err := browsers.Close(sessionID, operands[0]); err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "closed browser "+operands[0])
	return err
}
