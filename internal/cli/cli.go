// Package cli implements agent-manager's subcommands over internal/sessioncmd,
// the same layer the MCP server drives. An agent whose CLI carries no MCP
// client reaches the workspace from its shell instead, and both fronts speak
// the same words because the sentences come from sessioncmd.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
)

type Command func(args []string, sessionID, configDir string) error

// ErrUsageShown reports that -h already printed the usage, so the caller
// exits without an error line.
var ErrUsageShown = errors.New("usage shown")

const anyNumber = -1

type command struct {
	name  string
	usage string
	about string
	// Verbs are reached through run, never registered at the top level.
	verbs []command
	run   Command
}

type section struct {
	title    string
	commands []command
}

func sections() []section {
	return []section{
		sessionSection(),
		taskSection(),
		fileSection(),
		terminalSection(),
		reviewSection(),
	}
}

func Commands() map[string]Command {
	table := map[string]Command{}
	for _, section := range sections() {
		for _, command := range section.commands {
			table[command.name] = command.run
		}
	}
	return table
}

func Help() string {
	var help strings.Builder
	help.WriteString("agent-manager runs your session beside the user's other agents and terminals.\n\n")
	help.WriteString("usage: agent-manager <command> [flags]\n")
	help.WriteString("       agent-manager help\n\n")
	help.WriteString("Every command acts as the session it runs in, so run them from your own shell.\n")
	for _, section := range sections() {
		help.WriteString("\n" + section.title + "\n")
		help.WriteString(usageLines(section.commands))
	}
	return help.String()
}

func usageLines(commands []command) string {
	var lines strings.Builder
	for _, command := range commands {
		lines.WriteString("  agent-manager " + command.usage + "\n")
		lines.WriteString("      " + command.about + "\n")
		lines.WriteString(usageLines(command.verbs))
	}
	return lines.String()
}

func groupSection(title, name, about string, verbs []command) section {
	group := command{
		name:  name,
		usage: name + " <" + strings.Join(verbNames(verbs), "|") + ">",
		about: about,
		verbs: verbs,
		run: func(args []string, sessionID, configDir string) error {
			return dispatch(os.Stdout, name, verbs, args, sessionID, configDir)
		},
	}
	return section{title: title, commands: []command{group}}
}

func verbNames(verbs []command) []string {
	names := make([]string, 0, len(verbs))
	for _, verb := range verbs {
		names = append(names, verb.name)
	}
	return names
}

func dispatch(out io.Writer, group string, verbs []command, args []string, sessionID, configDir string) error {
	names := verbNames(verbs)
	if len(args) == 0 {
		return fmt.Errorf("usage: agent-manager %s <%s>", group, strings.Join(names, "|"))
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(out, usageLines(verbs))
		return ErrUsageShown
	}
	for _, verb := range verbs {
		if verb.name == args[0] {
			return verb.run(args[1:], sessionID, configDir)
		}
	}
	return fmt.Errorf("%s has no %q command; it takes %s", group, args[0], strings.Join(names, ", "))
}

// bind hands a subcommand the layer it drives, which tests replace with a
// fake so no handler reaches the manager's live tmux socket.
func bind[Layer any](open func(configDir string) Layer, run func(io.Writer, Layer, []string, string) error) Command {
	return func(args []string, sessionID, configDir string) error {
		return run(os.Stdout, open(configDir), args, sessionID)
	}
}

func configCommand(run func(out io.Writer, args []string, sessionID, configDir string) error) Command {
	return func(args []string, sessionID, configDir string) error {
		return run(os.Stdout, args, sessionID, configDir)
	}
}

// The set's name carries its usage line, so -h, an unknown flag and a
// miscounted operand all print the same words.
func newFlagSet(usage string) *flag.FlagSet {
	set := flag.NewFlagSet(usage, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

func jsonFlag(set *flag.FlagSet) *bool {
	return set.Bool("json", false, "print the raw result as JSON instead of a sentence")
}

func usageError(usage string) error {
	return fmt.Errorf("usage: agent-manager %s", usage)
}

func parseCommand(out io.Writer, set *flag.FlagSet, args []string, min, max int) ([]string, error) {
	operands, err := parseInterspersed(set, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(out, "usage: agent-manager "+set.Name())
		set.SetOutput(out)
		set.PrintDefaults()
		return nil, ErrUsageShown
	}
	if err != nil {
		return nil, fmt.Errorf("%w; usage: agent-manager %s", err, set.Name())
	}
	if len(operands) < min || (max != anyNumber && len(operands) > max) {
		return nil, usageError(set.Name())
	}
	return operands, nil
}

// parseInterspersed reads flags wherever they sit, since these commands lead
// with their operands and flag.Parse stops at the first one. Anything the
// set has no flag for is an operand, so a message or a title may start with
// a dash the way agent prose often does.
func parseInterspersed(set *flag.FlagSet, args []string) ([]string, error) {
	var literal []string
	if separator := slices.Index(args, "--"); separator >= 0 {
		literal = args[separator+1:]
		args = args[:separator]
	}
	operands := make([]string, 0, len(args)+len(literal))
	for len(args) > 0 {
		if !namesFlag(set, args[0]) {
			operands = append(operands, args[0])
			args = args[1:]
			continue
		}
		if err := set.Parse(args); err != nil {
			return nil, err
		}
		args = set.Args()
	}
	return append(operands, literal...), nil
}

// -h counts wherever it appears: the flag package answers it itself rather
// than declaring it.
func namesFlag(set *flag.FlagSet, token string) bool {
	if token == "-h" || token == "--help" {
		return true
	}
	name, dashed := strings.CutPrefix(token, "-")
	if !dashed {
		return false
	}
	name = strings.TrimPrefix(name, "-")
	name, _, _ = strings.Cut(name, "=")
	return set.Lookup(name) != nil
}

type stringList []string

func (list *stringList) String() string {
	return strings.Join(*list, ",")
}

func (list *stringList) Set(value string) error {
	*list = append(*list, strings.Split(value, ",")...)
	return nil
}

func emit(out io.Writer, asJSON bool, value any, human string) error {
	if asJSON {
		return writeJSON(out, value)
	}
	_, err := fmt.Fprintln(out, human)
	return err
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
